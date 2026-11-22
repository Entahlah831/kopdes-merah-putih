package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kopdes/merah-putih/internal/config"
)

func backendRootDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

func TestStructural_LeanBudgetsAndLimits(t *testing.T) {
	bRoot := backendRootDir(t)

	// 1. Maksimal 45 variabel env di AppConfig Catalog
	if n := len(config.Catalog()); n > 45 {
		t.Fatalf("Catalog memiliki %d variabel env, melebihi batas 45", n)
	}

	// 2. Maksimal 22 tabel PostgreSQL lintas migrasi *.up.sql
	migPaths, err := filepath.Glob(filepath.Join(bRoot, "db", "migrations", "*_*.up.sql"))
	if err != nil || len(migPaths) == 0 {
		t.Fatalf("glob migrasi: err=%v count=%d", err, len(migPaths))
	}
	tableCount := 0
	tblRe := regexp.MustCompile(`(?mi)^\s*CREATE\s+TABLE\b`)
	for _, mp := range migPaths {
		migBytes, rErr := os.ReadFile(mp)
		if rErr != nil {
			t.Fatalf("baca migrasi %s: %v", mp, rErr)
		}
		tableCount += len(tblRe.FindAllIndex(migBytes, -1))
	}
	if tableCount == 0 || tableCount > 22 {
		t.Fatalf("Jumlah tabel=%d (harus 1..22)", tableCount)
	}

	// 3. Maksimal 38 endpoint HTTP (37 aktif saat ini)
	routesBytes, err := os.ReadFile(filepath.Join(bRoot, "internal", "api", "routes.go"))
	if err != nil {
		t.Fatalf("baca routes.go: %v", err)
	}
	routeMatches := regexp.MustCompile(`mux\.Handle(?:Func)?\(`).FindAllIndex(routesBytes, -1)
	if len(routeMatches) == 0 || len(routeMatches) > 38 {
		t.Fatalf("Jumlah endpoint terdaftar=%d, melebihi batas 38", len(routeMatches))
	}
}

func TestStructural_FileAndFunctionSizes_NoRawSQL(t *testing.T) {
	bRoot := backendRootDir(t)
	internalDir := filepath.Join(bRoot, "internal")
	fset := token.NewFileSet()
	rawSQLPattern := regexp.MustCompile(`(?i)(pool\.(Exec|Query|QueryRow)|dbtx\.(Exec|Query|QueryRow)|"\s*(SELECT\s+.+\s+FROM|INSERT\s+INTO|UPDATE\s+\w+\s+SET|DELETE\s+FROM)\b)`)

	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		rel, _ := filepath.Rel(internalDir, path)
		relSlash := filepath.ToSlash(rel)

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)

		// Handler file <= 400 baris
		if strings.Contains(filepath.Base(path), "handler") {
			lines := strings.Count(text, "\n") + 1
			if lines > 400 {
				t.Errorf("%s memiliki %d baris (> 400 baris batas handler)", relSlash, lines)
			}
		}

		// Zero raw SQL di luar internal/db/
		if !strings.HasPrefix(relSlash, "db/") && rawSQLPattern.MatchString(text) {
			t.Errorf("%s mengandung pola SQL mentah; semua SQL wajib lewat sqlc", relSlash)
		}

		// Setiap fungsi di luar internal/db/ <= 60 baris
		if !strings.HasPrefix(relSlash, "db/") {
			astFile, pErr := parser.ParseFile(fset, path, raw, 0)
			if pErr != nil {
				return pErr
			}
			for _, decl := range astFile.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				start := fset.Position(fn.Pos()).Line
				end := fset.Position(fn.End()).Line
				if end-start+1 > 60 {
					t.Errorf("%s fungsi %s memiliki %d baris (> 60)", relSlash, fn.Name.Name, end-start+1)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestServer_Health_PublicConfig_AndReadOnlyGuard(t *testing.T) {
	cfg := DefaultConfig()
	srv := NewServer(nil, cfg)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz status=%d", rec.Code)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("header keamanan X-Content-Type-Options tidak disetel")
	}

	roCfg := DefaultConfig()
	roCfg.ReadOnlyMode = true
	roSrv := NewServer(nil, roCfg)
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/public/pendaftaran", strings.NewReader(`{}`))
	postRec := httptest.NewRecorder()
	roSrv.Router().ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("read-only POST status=%d, ingin 503", postRec.Code)
	}
}
