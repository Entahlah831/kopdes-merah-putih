package db

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/security"
)

// ValidateTestDatabaseDSN memastikan database target pengujian WAJIB bernama *_test.
// Ini mencegah pengujian menimpa atau mengotori database non-test atau produksi.
func ValidateTestDatabaseDSN(dsn string) error {
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("DSN database pengujian kosong")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("DSN database pengujian tidak valid (%s): %w", security.RedactDSN(dsn), err)
	}

	dbName := strings.TrimSpace(cfg.ConnConfig.Database)
	if dbName == "" || !strings.HasSuffix(dbName, "_test") {
		return fmt.Errorf("KEAMANAN TES: pengujian ditolak karena nama database '%s' tidak berakhiran '_test' (DSN: %s)", dbName, security.RedactDSN(dsn))
	}
	return nil
}

// findRepoRoot mencari direktori akar repositori yang memiliki go.mod.
func findRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// loadEnvMapFromFile membaca berkas env sintetis (.env.test / .env.test.alt) tanpa pernah membaca .env asli.
func loadTestEnvMap() map[string]string {
	envMap := make(map[string]string)
	envFile := os.Getenv("TEST_ENV_FILE")
	if envFile == "" {
		envFile = ".env.test"
	}
	base := filepath.Base(envFile)
	// Guard keras: dilarang membaca file asli pemilik (.env, .env.local, .env.production, .env.live)
	if base == ".env" || base == ".env.local" || base == ".env.production" || base == ".env.live" {
		return envMap
	}

	candidatePath := envFile
	if !filepath.IsAbs(candidatePath) {
		candidatePath = filepath.Join(findRepoRoot(), filepath.Base(envFile))
	}
	candidatePath = filepath.Clean(candidatePath)
	// #nosec G304,G703 -- helper tes membaca file profil env lokal repositori setelah pembersihan path
	data, err := os.ReadFile(candidatePath)
	if err != nil {
		return envMap
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			v = strings.Trim(v, `"'`)
			envMap[k] = v
		}
	}
	return envMap
}

func getTestVar(key string, fileMap map[string]string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fileMap[key]
}

// GetTestEnv mengambil variabel lingkungan pengujian dari OS env atau file .env.test (bukan .env asli).
func GetTestEnv(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return loadTestEnvMap()[key]
}

// ResolveTestDatabaseDSN menyusun DSN pengujian dari variabel lingkungan (atau .env.test bila belum diexport)
// dan mewajibkan nama database berakhiran '_test'.
func ResolveTestDatabaseDSN(overrideUser, overridePassword string) (string, error) {
	fileMap := loadTestEnvMap()

	if overrideUser == "" && overridePassword == "" {
		if dsn := getTestVar("TEST_DATABASE_URL", fileMap); dsn != "" {
			if err := ValidateTestDatabaseDSN(dsn); err != nil {
				return "", err
			}
			return dsn, nil
		}
		if dsn := getTestVar("DATABASE_URL", fileMap); dsn != "" {
			if err := ValidateTestDatabaseDSN(dsn); err != nil {
				return "", err
			}
			return dsn, nil
		}
	}

	host := getTestVar("DB_HOST", fileMap)
	if host == "" || host == "postgres" {
		// Saat tes Go dijalankan langsung di host (di luar container compose), pakai loopback 127.0.0.1
		host = "127.0.0.1"
	}

	port := getTestVar("DB_HOST_PORT", fileMap)
	if port == "" {
		port = getTestVar("DB_PORT", fileMap)
	}
	if port == "" {
		return "", fmt.Errorf("DB_HOST_PORT atau DB_PORT wajib disetel di environment atau .env.test")
	}

	dbname := getTestVar("DB_NAME", fileMap)
	if dbname == "" {
		return "", fmt.Errorf("DB_NAME wajib disetel (dan harus berakhiran _test) di environment atau .env.test")
	}

	user := overrideUser
	if user == "" {
		user = getTestVar("DB_USER", fileMap)
	}
	pass := overridePassword
	if pass == "" {
		pass = getTestVar("DB_PASSWORD", fileMap)
	}
	sslmode := getTestVar("DB_SSLMODE", fileMap)
	if sslmode == "" {
		sslmode = "disable"
	}

	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, pass),
		Host:     fmt.Sprintf("%s:%s", host, port),
		Path:     "/" + dbname,
		RawQuery: "sslmode=" + url.QueryEscape(sslmode),
	}
	dsn := u.String()
	if err := ValidateTestDatabaseDSN(dsn); err != nil {
		return "", err
	}
	return dsn, nil
}
