package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kopdes/merah-putih/internal/config"
)

func TestBuildDatabaseDSN_ConflictDetection_AndNoPasswordLeak(t *testing.T) {
	baseEnv := map[string]string{
		"APP_ENV":            "test",
		"API_PORT":           "18080",
		"API_HOST_PORT":      "18080",
		"METRICS_PORT":       "19100",
		"METRICS_HOST_PORT":  "19100",
		"DB_HOST":            "127.0.0.1",
		"DB_PORT":            "15433",
		"DB_NAME":            "kopdes_alt_test",
		"DB_USER":            "kopdes_app",
		"DB_PASSWORD":        "SuperSecretTestPass999!",
		"DB_SSLMODE":         "disable",
		"JWT_SECRET":         strings.Repeat("a1", 32),
		"BLIND_INDEX_SECRET": strings.Repeat("b2", 32),
		"AES_MASTER_KEY":     strings.Repeat("c3", 32),
		"QRIS_SECRET":        strings.Repeat("d4", 32),
		"METRICS_TOKEN":      strings.Repeat("e5", 32),
		"ALLOWED_ORIGINS":    "http://localhost:3000",
	}

	lookupFrom := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) {
			v, ok := m[k]
			return v, ok
		}
	}

	// 1. Sukses membentuk DSN dari komponen DB_*
	cfg, err := config.Load(lookupFrom(baseEnv))
	if err != nil {
		t.Fatalf("Expected valid config load, got error: %v", err)
	}
	dsn, err := cfg.BuildDatabaseDSN()
	if err != nil {
		t.Fatalf("BuildDatabaseDSN failed: %v", err)
	}
	if !strings.Contains(dsn, "@127.0.0.1:15433/kopdes_alt_test") {
		t.Fatalf("DSN mismatch, got: %s", dsn)
	}

	// 2. DATABASE_URL bertentangan port/host/db -> WAJIB gagal dan DILARANG mencetak password
	conflicts := []string{
		"postgres://kopdes_app:SuperSecretTestPass999!@127.0.0.1:5432/kopdes_alt_test?sslmode=disable",
		"postgres://kopdes_app:SuperSecretTestPass999!@otherhost:15433/kopdes_alt_test?sslmode=disable",
		"postgres://kopdes_app:SuperSecretTestPass999!@127.0.0.1:15433/other_db?sslmode=disable",
		"postgres://otheruser:SuperSecretTestPass999!@127.0.0.1:15433/kopdes_alt_test?sslmode=disable",
		"postgres://kopdes_app:WrongPassword!@127.0.0.1:15433/kopdes_alt_test?sslmode=disable",
	}
	for _, badURL := range conflicts {
		m := make(map[string]string, len(baseEnv)+1)
		for k, v := range baseEnv {
			m[k] = v
		}
		m["DATABASE_URL"] = badURL
		c, err := config.Load(lookupFrom(m))
		if err == nil {
			_, err = c.BuildDatabaseDSN()
		}
		if err == nil {
			t.Errorf("Expected error for conflicting DATABASE_URL %q, got nil", badURL)
			continue
		}
		if strings.Contains(err.Error(), "SuperSecretTestPass999!") || strings.Contains(err.Error(), "WrongPassword!") {
			t.Errorf("SECURITY VIOLATION: error message leaked password: %v", err)
		}
	}

	// 3. DATABASE_URL identik dengan komponen -> HARUS sukses
	mSame := make(map[string]string, len(baseEnv)+1)
	for k, v := range baseEnv {
		mSame[k] = v
	}
	mSame["DATABASE_URL"] = "postgres://kopdes_app:SuperSecretTestPass999!@127.0.0.1:15433/kopdes_alt_test?sslmode=disable"
	cSame, err := config.Load(lookupFrom(mSame))
	if err != nil {
		t.Fatalf("Expected matching DATABASE_URL to succeed, got: %v", err)
	}
	if _, err := cSame.BuildDatabaseDSN(); err != nil {
		t.Fatalf("Expected BuildDatabaseDSN to succeed for matching DATABASE_URL, got: %v", err)
	}
}

func TestLoadConfig_ReadsSecurityAndOperationalParametersFromEnv(t *testing.T) {
	m := map[string]string{
		"APP_ENV":                       "test",
		"API_PORT":                      "18080",
		"METRICS_PORT":                  "19100",
		"DB_HOST":                       "127.0.0.1",
		"DB_PORT":                       "15433",
		"DB_NAME":                       "kopdes_alt_test",
		"DB_USER":                       "kopdes_app",
		"DB_PASSWORD":                   "TestPass123!",
		"JWT_SECRET":                    strings.Repeat("a1", 32),
		"JWT_ALGORITHM":                 "HS512",
		"JWT_ACCESS_TTL":                "3m",
		"JWT_REFRESH_TTL":               "48h",
		"BLIND_INDEX_SECRET":            strings.Repeat("b2", 32),
		"AES_MASTER_KEY":                strings.Repeat("c3", 32),
		"AES_KEY_VERSION":               "v2",
		"AES_KEY_ID":                    "k9",
		"QRIS_SECRET":                   strings.Repeat("d4", 32),
		"METRICS_TOKEN":                 strings.Repeat("e5", 32),
		"BCRYPT_COST":                   "10",
		"MFA_ISSUER":                    "Kopdes Alt Test",
		"MFA_MAX_ATTEMPTS":              "4",
		"MFA_SESSION_TTL":               "2m",
		"MFA_SETUP_TTL":                 "6m",
		"LOGIN_MAX_ATTEMPTS":            "3",
		"LOGIN_LOCKOUT_DURATION":        "5m",
		"RATE_LIMIT_RPS":                "45",
		"RATE_LIMIT_BURST":              "90",
		"MAX_BODY_BYTES":                "5242880",
		"HTTP_READ_HEADER_TIMEOUT":      "3s",
		"PARTITION_MONTHS_AHEAD":        "4",
		"PDP_REJECTED_RETENTION_DAYS":   "45",
		"PILOT_DEFAULT_MAX_PER_TX":      "750000",
		"PILOT_DEFAULT_MAX_PER_DAY":     "2500000",
		"DUAL_APPROVAL_DEFAULT_THRESHOLD": "300000",
	}

	cfg, err := config.Load(func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.JWTAlgorithm != "HS512" || cfg.JWTAccessTTL != 3*time.Minute || cfg.JWTRefreshTTL != 48*time.Hour {
		t.Errorf("JWT params mismatch: algo=%s access=%v refresh=%v", cfg.JWTAlgorithm, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	}
	if cfg.BcryptCost != 10 || cfg.MFAIssuer != "Kopdes Alt Test" || cfg.MFAMaxAttempts != 4 {
		t.Errorf("Auth/MFA params mismatch: cost=%d issuer=%s attempts=%d", cfg.BcryptCost, cfg.MFAIssuer, cfg.MFAMaxAttempts)
	}
	if cfg.AESKeyVersion != "v2" || cfg.AESKeyID != "k9" {
		t.Errorf("AES metadata mismatch: version=%s id=%s", cfg.AESKeyVersion, cfg.AESKeyID)
	}
	if cfg.PartitionMonthsAhead != 4 || cfg.PDPRejectedRetentionDays != 45 || cfg.PilotDefaultMaxPerTx != 750000 {
		t.Errorf("Operational params mismatch")
	}
}

func TestCatalogAndEnvReferenceMarkdown(t *testing.T) {
	specs := config.Catalog()
	if len(specs) < 35 {
		t.Fatalf("Expected comprehensive env catalog (>= 35 vars), got %d", len(specs))
	}
	md := config.RenderEnvReferenceMarkdown()
	if !strings.Contains(md, "| NAMA | TIPE | DEFAULT AMAN | WAJIB DI | RAHASIA? | DIBACA OLEH | PENJELASAN | CONTOH NILAI SINTETIS |") {
		t.Errorf("Generated markdown missing expected header table")
	}
	if !strings.Contains(md, "konfigurasi_aturan") {
		t.Errorf("Generated markdown must distinguish env vars vs konfigurasi_aturan DB table")
	}
}
