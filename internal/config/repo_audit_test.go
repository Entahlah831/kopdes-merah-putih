package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kopdes/merah-putih/internal/config"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd gagal: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod tidak ditemukan")
		}
		dir = parent
	}
}

func TestRepoAudit_EnvCatalogConsistency_AndEnvReferenceFresh(t *testing.T) {
	root := repoRoot(t)
	specs := config.Catalog()
	if len(specs) != 43 {
		t.Fatalf("Jumlah variabel env harus tepat 43 (<= 45), ditemukan %d", len(specs))
	}
	specMap := make(map[string]config.EnvVarSpec, len(specs))
	for _, s := range specs {
		specMap[s.Name] = s
	}

	// 1. Cek .env.example, .env.test, .env.test.alt 100% sinkron dengan Catalog()
	envFiles := []string{".env.example", ".env.test", ".env.test.alt"}
	parsed := make(map[string]map[string]string, len(envFiles))
	for _, rel := range envFiles {
		m, err := config.ParseEnvFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("Gagal parse %s: %v", rel, err)
		}
		parsed[rel] = m

		for name, spec := range specMap {
			val, ok := m[name]
			if !ok {
				t.Errorf("%s kehilangan variabel %s yang ada di AppConfig", rel, name)
				continue
			}
			if rel == ".env.example" && spec.Secret && val != "" {
				t.Errorf(".env.example TIDAK BOLEH berisi nilai rahasia untuk %s", name)
			}
		}
		for k := range m {
			if _, ok := specMap[k]; !ok {
				t.Errorf("%s memiliki variabel %s yang tidak dikenal oleh AppConfig (unused/mismatch)", rel, k)
			}
		}
	}

	// 2. Pastikan Set A (.env.test) dan Set B (.env.test.alt) memakai port, DB, dan secret berbeda
	setA := parsed[".env.test"]
	setB := parsed[".env.test.alt"]
	if !strings.HasSuffix(setA["DB_NAME"], "_test") || !strings.HasSuffix(setB["DB_NAME"], "_test") {
		t.Errorf("Set A dan Set B DB_NAME wajib berakhiran _test: A=%q B=%q", setA["DB_NAME"], setB["DB_NAME"])
	}
	mustDiffer := []string{
		"DB_NAME", "API_PORT", "API_HOST_PORT", "UI_PORT", "WORKER_PORT", "DB_PORT", "DB_HOST_PORT",
		"DB_PASSWORD", "POSTGRES_PASSWORD", "AES_MASTER_KEY", "BLIND_INDEX_SECRET",
		"BACKUP_AGE_RECIPIENT", "STORAGE_BUKTI_DIR", "STOCK_HOLD_MINUTES",
		"ALLOW_BEARER_AUTH", "BUKTI_RETENTION_DAYS", "SESSION_IDLE_TTL", "QRIS_UNIQUE_DIGITS", "MFA_REQUIRED_ROLES",
	}
	for _, k := range mustDiffer {
		if setA[k] == "" || setB[k] == "" {
			t.Errorf("Variabel uji %s wajib terisi di .env.test dan .env.test.alt", k)
		} else if setA[k] == setB[k] {
			t.Errorf("Set A (.env.test) dan Set B (.env.test.alt) harus berbeda untuk %s (sama-sama %q)", k, setA[k])
		}
	}

	// 3. Cek seluruh panggilan os.Getenv / os.LookupEnv di kode Go produksi terdaftar (selain 4 selector OS resmi)
	processSelectors := map[string]bool{
		"ENV_FILE": true, "TEST_ENV_FILE": true, "TEST_DATABASE_URL": true, "DATABASE_URL": true,
	}
	reEnvCall := regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\(\s*"([A-Z0-9_]+)"\s*\)`)
	for _, sub := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, sub), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range reEnvCall.FindAllStringSubmatch(string(data), -1) {
				envName := m[1]
				if _, ok := specMap[envName]; !ok && !processSelectors[envName] {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s membaca env %s yang TIDAK terdaftar di AppConfig / .env.example", rel, envName)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir %s gagal: %v", sub, err)
		}
	}

	// 4. Cek docs/ENV_REFERENCE.md sinkron 100% dengan RenderEnvReferenceMarkdown()
	docPath := filepath.Join(root, "docs", "ENV_REFERENCE.md")
	docBytes, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("docs/ENV_REFERENCE.md belum ada: %v", err)
	}
	expectedDoc := strings.ReplaceAll(config.RenderEnvReferenceMarkdown(), "\r\n", "\n")
	actualDoc := strings.ReplaceAll(string(docBytes), "\r\n", "\n")
	if actualDoc != expectedDoc {
		t.Errorf("docs/ENV_REFERENCE.md tidak sinkron dengan struct AppConfig; jalankan envcheck --gen-doc docs/ENV_REFERENCE.md")
	}
}

func TestRepoAudit_NoHardcodedPortsOrDevPasswords_AndInfraFilesExist(t *testing.T) {
	root := repoRoot(t)

	requiredFiles := []string{
		".dockerignore",
		"docker-compose.yml",
		"docker-compose.test.yml",
		"cmd/envcheck/main.go",
		"scripts/promote_env.ps1",
		"scripts/promote_env.sh",
		"scripts/port_check.ps1",
		"scripts/port_check.sh",
		"docs/TUGAS_MANUAL_PEMILIK.md",
	}
	for _, rel := range requiredFiles {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("Berkas prasyarat konfigurasi tidak ditemukan: %s", rel)
		}
	}

	hardcodedTargets := []string{
		"Dockerfile",
		"docker-compose.yml",
		"docker-compose.test.yml",
		"scripts/backup.sh",
		"scripts/restore_drill.sh",
		"scripts/restore_test.sh",
		"scripts/seed_test_accounts.ps1",
		"scripts/seed_test_accounts.sh",
		"cmd/server/main.go",
		"cmd/uitester/main.go",
		"cmd/seed_test_accounts/main.go",
		"frontend-tester/index.html",
		"frontend-tester/app.js",
	}
	portRe := regexp.MustCompile(`\b(?:8080|8081|8082|18080|18081|18082|5432|5433|9090|9100)\b`)
	for _, rel := range hardcodedTargets {
		p := filepath.Join(root, rel)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("Gagal membaca %s: %v", rel, err)
			continue
		}
		for idx, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if loc := portRe.FindString(line); loc != "" {
				t.Errorf("%s:%d mengandung port hardcoded %q — wajib membaca dari variabel lingkungan!", rel, idx+1, loc)
			}
		}
	}

	rolesBytes, err := os.ReadFile(filepath.Join(root, "db", "docker-init", "01_roles.sql"))
	if err != nil {
		t.Fatalf("Gagal membaca 01_roles.sql: %v", err)
	}
	if strings.Contains(string(rolesBytes), "v_pass := 'kopdes_dev_password'") {
		t.Errorf("db/docker-init/01_roles.sql masih mengandung fallback password dev!")
	}
}
