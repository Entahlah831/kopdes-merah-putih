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

func repoRootDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func TestStructural_LeanBudgetsAndLimits(t *testing.T) {
	root := repoRootDir(t)

	// 1. Maksimal 40 variabel env di AppConfig Catalog
	if n := len(config.Catalog()); n > 40 {
		t.Fatalf("Catalog memiliki %d variabel env, melebihi batas 40", n)
	}

	// 2. Maksimal 20 tabel PostgreSQL
	migBytes, err := os.ReadFile(filepath.Join(root, "db", "migrations", "000001_init_kopdes_schema.up.sql"))
	if err != nil {
		t.Fatalf("baca migrasi awal: %v", err)
	}
	tableCount := regexp.MustCompile(`(?mi)^\s*CREATE\s+TABLE\b`).FindAllIndex(migBytes, -1)
	if len(tableCount) == 0 || len(tableCount) > 20 {
		t.Fatalf("Jumlah tabel=%d (harus 1..20)", len(tableCount))
	}

	// 3. Maksimal 35 endpoint HTTP
	routesBytes, err := os.ReadFile(filepath.Join(root, "internal", "api", "routes.go"))
	if err != nil {
		t.Fatalf("baca routes.go: %v", err)
	}
	routeMatches := regexp.MustCompile(`mux\.Handle(?:Func)?\(`).FindAllIndex(routesBytes, -1)
	if len(routeMatches) == 0 || len(routeMatches) > 35 {
		t.Fatalf("Jumlah endpoint terdaftar=%d, melebihi batas 35", len(routeMatches))
	}
}

func TestStructural_FileAndFunctionSizes_NoRawSQL(t *testing.T) {
	root := repoRootDir(t)
	internalDir := filepath.Join(root, "internal")
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
