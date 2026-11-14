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
				t.Errorf(".env.example TIDAK BOLEH berisi nilai rahasia untuk %s (dapat non-kosong)", name)
			}
		}
		for k := range m {
			if _, ok := specMap[k]; !ok {
				t.Errorf("%s memiliki variabel %s yang tidak dikenal oleh AppConfig (unused/mismatch)", rel, k)
			}
		}
	}

	// 2. Pastikan Set A (.env.test) dan Set B (.env.test.alt) benar-benar memakai port, DB, dan secret berbeda
	setA := parsed[".env.test"]
	setB := parsed[".env.test.alt"]
	if !strings.HasSuffix(setA["DB_NAME"], "_test") || !strings.HasSuffix(setB["DB_NAME"], "_test") {
		t.Errorf("Set A dan Set B DB_NAME wajib berakhiran _test: A=%q B=%q", setA["DB_NAME"], setB["DB_NAME"])
	}
	mustDiffer := []string{
		"DB_NAME", "API_PORT", "API_HOST_PORT", "UI_PORT", "API_PUBLIC_URL", "DB_PORT", "DB_HOST_PORT",
		"DB_CONTAINER_PORT", "METRICS_PORT", "METRICS_HOST_PORT", "PROMETHEUS_HOST_PORT",
		"DB_PASSWORD", "KOPDES_APP_PASSWORD", "POSTGRES_PASSWORD", "TEST_SEED_PASSWORD",
		"JWT_SECRET", "AES_MASTER_KEY", "BLIND_INDEX_SECRET", "QRIS_SECRET", "METRICS_TOKEN",
		"JWT_ACCESS_TTL", "JWT_ALGORITHM", "BCRYPT_COST", "MFA_ISSUER",
	}
	for _, k := range mustDiffer {
		if setA[k] == "" || setB[k] == "" {
			t.Errorf("Variabel uji %s wajib terisi di .env.test dan .env.test.alt", k)
		} else if setA[k] == setB[k] {
			t.Errorf("Set A (.env.test) dan Set B (.env.test.alt) harus berbeda untuk %s (sama-sama %q)", k, setA[k])
		}
	}

	// 3. Cek seluruh panggilan os.Getenv / os.LookupEnv di kode Go produksi (cmd/ & internal/) ada di Catalog()
	reEnvCall := regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\(\s*"([A-Z0-9_]+)"\s*\)`)
	for _, sub := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, sub), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			matches := reEnvCall.FindAllStringSubmatch(string(data), -1)
			for _, m := range matches {
				envName := m[1]
				if _, ok := specMap[envName]; !ok {
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
		t.Errorf("docs/ENV_REFERENCE.md tidak sinkron dengan struct AppConfig di internal/config/config.go; jalankan envcheck --gen-doc docs/ENV_REFERENCE.md")
	}
}

func TestRepoAudit_NoHardcodedPortsOrDevPasswords_AndInfraFilesExist(t *testing.T) {
	root := repoRoot(t)

	// 1. Cek berkas prasyarat Tugas A4, A5, A6, A7, B4
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

	// 2. Uji statis larangan port hardcoded (8080, 5432, 5433, 9090, 9100) di berkas konfigurasi & skrip eksekusi
	// Kecuali daftar allowlist yang terdokumentasi (.env*, docs/*.md, test files, default fallback di config.go/server.go)
	hardcodedTargets := []string{
		"Dockerfile",
		"docker-compose.yml",
		"docker-compose.test.yml",
		"docs/prometheus.example.yml",
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
	portRe := regexp.MustCompile(`\b(?:8080|8081|18080|18081|5432|5433|9090|9100)\b`)
	for _, rel := range hardcodedTargets {
		p := filepath.Join(root, rel)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("Gagal membaca %s: %v", rel, err)
			continue
		}
		lines := strings.Split(string(data), "\n")
		for idx, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if loc := portRe.FindString(line); loc != "" {
				t.Errorf("%s:%d mengandung port hardcoded %q — wajib membaca dari variabel lingkungan!", rel, idx+1, loc)
			}
		}
	}

	// 3. Pastikan db/docker-init/01_roles.sql tidak memiliki fallback kopdes_dev_password
	rolesBytes, err := os.ReadFile(filepath.Join(root, "db", "docker-init", "01_roles.sql"))
	if err != nil {
		t.Fatalf("Gagal membaca 01_roles.sql: %v", err)
	}
	if strings.Contains(string(rolesBytes), "v_pass := 'kopdes_dev_password'") {
		t.Errorf("db/docker-init/01_roles.sql masih mengandung fallback password dev!")
	}
}
