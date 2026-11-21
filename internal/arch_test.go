package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const moduleImportPrefix = "github.com/kopdes/merah-putih/internal/"

// tableManifest mendefinisikan kepemilikan eksklusif 21 tabel PostgreSQL per modul domain.
var tableManifest = map[string][]string{
	"auth":       {"users", "sesi", "kode_aktivasi", "rate_limits"},
	"pengaturan": {"pengaturan", "audit_log"},
	"anggota":    {"anggota", "pendaftaran", "dokumen_kyc"},
	"iuran":      {"jenis_iuran", "tagihan_iuran"},
	"katalog":    {"kategori", "produk", "promo"},
	"pesanan":    {"pesanan", "pesanan_item"},
	"pembayaran": {"rekening_qris", "pembayaran", "pembayaran_alokasi", "pengembalian_dana"},
	"laporan":    {},
	"worker":     {"worker_runs"},
}

var domainModules = []string{
	"auth", "pengaturan", "anggota", "iuran", "katalog", "pesanan", "pembayaran", "laporan",
}

var sharedInfraImports = map[string]bool{
	"db":             true,
	"config":         true,
	"security":       true,
	"filestore":      true,
	"api/response":   true,
	"api/middleware": true,
}

func internalDirPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(wd)
}

func buildTableOwnerMap() map[string]string {
	out := make(map[string]string)
	for mod, tables := range tableManifest {
		for _, tbl := range tables {
			out[tbl] = mod
		}
	}
	return out
}

func TestArchitecture_TableOwnershipManifestMatchesMigrations(t *testing.T) {
	intDir := internalDirPath(t)
	root := filepath.Clean(filepath.Join(intDir, ".."))
	if _, err := os.Stat(filepath.Join(intDir, "pendaftaran")); !os.IsNotExist(err) {
		t.Errorf("PELANGGARAN ARSITEKTUR: direktori internal/pendaftaran masih ada; harus melebur ke internal/anggota")
	}

	migPaths, err := filepath.Glob(filepath.Join(root, "db", "migrations", "*_*.up.sql"))
	if err != nil || len(migPaths) == 0 {
		t.Fatalf("gagal glob migrasi: %v (jumlah=%d)", err, len(migPaths))
	}

	reCreate := regexp.MustCompile(`(?mi)^\s*CREATE\s+TABLE\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	migTables := make(map[string]bool)
	for _, mp := range migPaths {
		raw, rErr := os.ReadFile(mp)
		if rErr != nil {
			t.Fatalf("baca migrasi %s: %v", mp, rErr)
		}
		for _, m := range reCreate.FindAllStringSubmatch(string(raw), -1) {
			migTables[strings.ToLower(m[1])] = true
		}
	}

	ownerMap := buildTableOwnerMap()
	for tbl := range migTables {
		if _, ok := ownerMap[tbl]; !ok {
			t.Errorf("tabel migrasi %q belum memiliki modul pemilik di tableManifest", tbl)
		}
	}
	for tbl, mod := range ownerMap {
		if !migTables[tbl] {
			t.Errorf("tabel manifest %q (modul %s) tidak ditemukan di migrasi", tbl, mod)
		}
	}
}

func TestArchitecture_ImportBoundaries(t *testing.T) {
	intDir := internalDirPath(t)
	fset := token.NewFileSet()

	for _, mod := range domainModules {
		modDir := filepath.Join(intDir, mod)
		if _, err := os.Stat(modDir); os.IsNotExist(err) {
			t.Errorf("direktori modul internal/%s belum ada", mod)
			continue
		}
		err := filepath.WalkDir(modDir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return walkErr
			}
			rel, _ := filepath.Rel(modDir, path)
			isContractSubpkg := filepath.ToSlash(rel) == "api" || strings.HasPrefix(filepath.ToSlash(rel), "api/")

			astFile, pErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if pErr != nil {
				return pErr
			}
			for _, imp := range astFile.Imports {
				impPath := strings.Trim(imp.Path.Value, `"`)
				if !strings.HasPrefix(impPath, moduleImportPrefix) {
					continue
				}
				sub := strings.TrimPrefix(impPath, moduleImportPrefix)
				validateModuleImport(t, mod, filepath.ToSlash(rel), isContractSubpkg, sub)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func validateModuleImport(t *testing.T, ownerMod, relFile string, isContractSubpkg bool, targetSub string) {
	t.Helper()
	parts := strings.Split(targetSub, "/")
	targetRoot := parts[0]

	if isContractSubpkg {
		// Paket kontrak internal/<M>/api hanya boleh mengimpor internal/<B>/api lain (tanpa internal/db atau implementasi konkret)
		if len(parts) == 2 && parts[1] == "api" && targetRoot != ownerMod {
			return
		}
		t.Errorf("PELANGGARAN KONTRAK API: internal/%s/%s mengimpor internal/%s (subpaket api harus murni tipe/interface)", ownerMod, relFile, targetSub)
		return
	}

	if targetRoot == ownerMod {
		return
	}
	if sharedInfraImports[targetSub] {
		return
	}
	// Boleh mengimpor kontrak publik domain lain: internal/<B>/api
	if len(parts) == 2 && parts[1] == "api" && isDomainOrWorkerModule(targetRoot) {
		return
	}
	t.Errorf("PELANGGARAN BATAS IMPOR: internal/%s/%s mengimpor paket konkret lintas-modul internal/%s", ownerMod, relFile, targetSub)
}

func isDomainOrWorkerModule(name string) bool {
	if name == "worker" {
		return true
	}
	for _, m := range domainModules {
		if m == name {
			return true
		}
	}
	return false
}

func TestArchitecture_SQLTableOwnership(t *testing.T) {
	intDir := internalDirPath(t)
	ownerMap := buildTableOwnerMap()

	// internal/laporan harus pure aggregator tanpa berkas SQL
	laporanSQLs, _ := filepath.Glob(filepath.Join(intDir, "laporan", "queries", "*.sql"))
	for _, lf := range laporanSQLs {
		t.Errorf("PELANGGARAN SQL LAPORAN: %s tidak boleh ada; laporan harus memanggil modul via api/", filepath.Base(lf))
	}

	for mod := range tableManifest {
		if mod == "laporan" {
			continue
		}
		sqlFiles, err := filepath.Glob(filepath.Join(intDir, mod, "queries", "*.sql"))
		if err != nil {
			t.Fatal(err)
		}
		for _, sf := range sqlFiles {
			raw, rErr := os.ReadFile(sf)
			if rErr != nil {
				t.Fatal(rErr)
			}
			for _, tbl := range extractSQLTables(string(raw)) {
				owner, known := ownerMap[tbl]
				if !known {
					t.Errorf("SQL %s/%s merujuk tabel tak dikenal %q", mod, filepath.Base(sf), tbl)
					continue
				}
				if owner != mod {
					t.Errorf("PELANGGARAN KEPEMILIKAN TABEL SQL: modul %q (%s) menyentuh tabel %q milik modul %q", mod, filepath.Base(sf), tbl, owner)
				}
			}
		}
	}
}

func extractSQLTables(sqlText string) []string {
	// Hapus komentar baris `-- ...`
	reComment := regexp.MustCompile(`(?m)--[^\r\n]*`)
	cleaned := reComment.ReplaceAllString(sqlText, " ")

	// Catat nama CTE `WITH cte AS (` atau `, cte AS (`
	ctes := make(map[string]bool)
	reCTE := regexp.MustCompile(`(?i)(?:\bWITH(?:\s+RECURSIVE)?|,)\s*([a-zA-Z_][a-zA-Z0-9_]*)\s+AS\s*\(`)
	for _, m := range reCTE.FindAllStringSubmatch(cleaned, -1) {
		ctes[strings.ToLower(m[1])] = true
	}

	skipTokens := map[string]bool{
		"select": true, "values": true, "lateral": true, "only": true, "unnest": true,
		"pg_try_advisory_lock": true, "pg_advisory_unlock": true, "nextval": true,
	}

	var found []string
	seen := make(map[string]bool)
	record := func(tok string) {
		low := strings.ToLower(tok)
		if ctes[low] || skipTokens[low] || seen[low] {
			return
		}
		seen[low] = true
		found = append(found, low)
	}

	// FROM / JOIN / INTO / DELETE FROM
	reFromJoinInto := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	for _, m := range reFromJoinInto.FindAllStringSubmatch(cleaned, -1) {
		record(m[1])
	}

	// UPDATE <table> (abaikan FOR UPDATE / FOR UPDATE OF ... dan DO UPDATE SET ...)
	reUpdate := regexp.MustCompile(`(?i)(?:FOR\s+|DO\s+)?\bUPDATE\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	for _, m := range reUpdate.FindAllStringSubmatch(cleaned, -1) {
		full := strings.ToUpper(strings.TrimSpace(m[0]))
		if strings.HasPrefix(full, "FOR ") || strings.HasPrefix(full, "DO ") {
			continue
		}
		record(m[1])
	}

	sort.Strings(found)
	return found
}

func TestArchitecture_DBQueriesMethodOwnership(t *testing.T) {
	intDir := internalDirPath(t)
	methodOwner := parseGeneratedSQLCMethodOwners(t, filepath.Join(intDir, "db"))

	for _, mod := range append(append([]string{}, domainModules...), "worker") {
		modDir := filepath.Join(intDir, mod)
		if _, err := os.Stat(modDir); os.IsNotExist(err) {
			continue
		}
		checkModuleQueryMethodCalls(t, mod, modDir, methodOwner)
	}
}

func parseGeneratedSQLCMethodOwners(t *testing.T, dbDir string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dbDir, "*.sql.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	owners := make(map[string]string)

	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".sql.go")
		if _, validMod := tableManifest[base]; !validMod {
			t.Errorf("PELANGGARAN BERKAS SQLC: internal/db/%s tidak sesuai modul pemilik di tableManifest", filepath.Base(f))
		}
		astFile, pErr := parser.ParseFile(fset, f, nil, 0)
		if pErr != nil {
			t.Fatal(pErr)
		}
		for _, decl := range astFile.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			if recvTypeIsQueries(fn.Recv.List[0].Type) {
				owners[fn.Name.Name] = base
			}
		}
	}
	return owners
}

func recvTypeIsQueries(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "Queries"
}

func checkModuleQueryMethodCalls(t *testing.T, mod, modDir string, methodOwner map[string]string) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(modDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		astFile, pErr := parser.ParseFile(fset, path, nil, 0)
		if pErr != nil {
			return pErr
		}
		if !importsPackage(astFile, moduleImportPrefix+"db") {
			return nil
		}
		rel, _ := filepath.Rel(modDir, path)
		ast.Inspect(astFile, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			owner, isQueryMethod := methodOwner[sel.Sel.Name]
			if !isQueryMethod {
				return true
			}
			// Hindari false-positive dengan memastikan pemanggilan berasal dari variabel/ekspresi query/db
			if !looksLikeQueriesReceiver(sel.X) {
				return true
			}
			if owner != mod {
				t.Errorf("PELANGGARAN METODE SQLC: internal/%s/%s memanggil db.Queries.%s (milik modul %q)",
					mod, filepath.ToSlash(rel), sel.Sel.Name, owner)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func importsPackage(f *ast.File, target string) bool {
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) == target {
			return true
		}
	}
	return false
}

func looksLikeQueriesReceiver(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		low := strings.ToLower(e.Name)
		return low == "q" || low == "qt" || low == "qtx" || strings.Contains(low, "quer")
	case *ast.SelectorExpr:
		low := strings.ToLower(e.Sel.Name)
		return low == "q" || low == "qt" || low == "qtx" || strings.Contains(low, "quer")
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "db" && sel.Sel.Name == "New" {
				return true
			}
			if sel.Sel.Name == "WithTx" {
				return true
			}
		}
	}
	return false
}
