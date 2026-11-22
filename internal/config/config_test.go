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
		"UI_PORT":            "18081",
		"DB_HOST":            "127.0.0.1",
		"DB_PORT":            "15433",
		"DB_HOST_PORT":       "15433",
		"DB_NAME":            "kopdes_alt_test",
		"DB_USER":            "kopdes_app",
		"DB_PASSWORD":        "SuperSecretTestPass999!",
		"DB_SSLMODE":         "disable",
		"BLIND_INDEX_SECRET": strings.Repeat("b2", 32),
		"AES_MASTER_KEY":     strings.Repeat("c3", 32),
		"ALLOWED_ORIGINS":    "http://127.0.0.1:18081",
	}

	lookupFrom := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) {
			v, ok := m[k]
			return v, ok
		}
	}

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

func TestLoadConfig_ReadsLeanSecurityAndOperationalParametersFromEnv(t *testing.T) {
	m := map[string]string{
		"APP_ENV":                      "test",
		"API_PORT":                     "18080",
		"API_HOST_PORT":                "18080",
		"UI_PORT":                      "18081",
		"DB_HOST":                      "127.0.0.1",
		"DB_PORT":                      "15433",
		"DB_HOST_PORT":                 "15433",
		"DB_NAME":                      "kopdes_alt_test",
		"DB_USER":                      "kopdes_app",
		"DB_PASSWORD":                  "TestPass123!",
		"BLIND_INDEX_SECRET":           strings.Repeat("b2", 32),
		"AES_MASTER_KEY":               "v2:k9=" + strings.Repeat("c3", 32),
		"SESSION_IDLE_TTL":             "20m",
		"SESSION_ABS_TTL":              "8h",
		"MFA_REQUIRED_ROLES":           "admin,bendahara,petugas_toko",
		"LOGIN_MAX_ATTEMPTS":           "3",
		"LOGIN_LOCKOUT_DURATION":       "5m",
		"QRIS_UNIQUE_DIGITS":           "4",
		"PAYMENT_EXPIRY_DURATION":      "20m",
		"PENDING_LIMIT_PER_MEMBER":     "3",
		"PENDING_LIMIT_GLOBAL":         "30",
		"PDP_REJECTED_RETENTION_DAYS":  "45",
		"PILOT_MAX_PER_TX":             "750000",
		"PILOT_MAX_PER_DAY":            "2500000",
	}

	cfg, err := config.Load(func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.SessionIdleTTL != 20*time.Minute || cfg.SessionAbsTTL != 8*time.Hour {
		t.Errorf("Session TTL mismatch: idle=%v abs=%v", cfg.SessionIdleTTL, cfg.SessionAbsTTL)
	}
	if len(cfg.MFARequiredRoles) != 3 {
		t.Errorf("Auth/MFA params mismatch: roles=%v", cfg.MFARequiredRoles)
	}
	if cfg.QRISUniqueDigits != 4 {
		t.Errorf("QRIS params mismatch: digits=%d", cfg.QRISUniqueDigits)
	}
	if cfg.PDPRejectedRetentionDays != 45 || cfg.PilotMaxPerTx != 750000 || cfg.PilotMaxPerDay != 2500000 {
		t.Errorf("Operational params mismatch")
	}
}

func TestCatalogAndEnvReferenceMarkdown(t *testing.T) {
	specs := config.Catalog()
	if len(specs) != 43 {
		t.Fatalf("Expected exact 43 lean env vars (<= 45), got %d", len(specs))
	}
	for _, s := range specs {
		if strings.TrimSpace(s.Rationale) == "" {
			t.Errorf("Variabel %s wajib memiliki alasan tertulis (tag rationale)", s.Name)
		}
	}
	md := config.RenderEnvReferenceMarkdown()
	if !strings.Contains(md, "| NAMA | TIPE | DEFAULT AMAN | WAJIB DI | RAHASIA? | DIBACA OLEH | PENJELASAN | ALASAN DIPERTAHANKAN | CONTOH NILAI SINTETIS |") {
		t.Errorf("Generated markdown missing expected header table")
	}
	if !strings.Contains(md, "pengaturan") {
		t.Errorf("Generated markdown must reference single-cooperative table pengaturan")
	}
}

func TestValidateProduction_RejectsWideTrustedProxiesCIDRAndInsecureSSL(t *testing.T) {
	baseProd := map[string]string{
		"APP_ENV":               "production",
		"DB_HOST":               "10.20.1.5",
		"DB_PORT":               "5432",
		"DB_NAME":               "kopdes_prod",
		"DB_USER":               "kopdes_app",
		"DB_PASSWORD":           "ProdStrongPass123!@#",
		"DB_SSLMODE":            "require",
		"BLIND_INDEX_SECRET":    strings.Repeat("b2", 32),
		"AES_MASTER_KEY":        strings.Repeat("c3", 32),
		"BACKUP_AGE_RECIPIENT":  "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p",
		"ALLOWED_ORIGINS":       "https://koperasi.example.id",
	}

	rejectCases := []struct {
		name  string
		proxy string
	}{
		{"DockerBridge12", "172.16.0.0/12"},
		{"ClassA8", "10.0.0.0/8"},
		{"AllIPv4", "0.0.0.0/0"},
		{"InvalidCIDR", "not-a-cidr"},
	}

	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			m := make(map[string]string, len(baseProd)+1)
			for k, v := range baseProd {
				m[k] = v
			}
			m["TRUSTED_PROXIES"] = tc.proxy
			cfg, err := config.Load(func(k string) (string, bool) {
				v, ok := m[k]
				return v, ok
			})
			if err != nil {
				t.Fatalf("Load unexpected error: %v", err)
			}
			if err := cfg.ValidateProduction(); err == nil {
				t.Fatalf("Expected ValidateProduction to reject TRUSTED_PROXIES=%q in production, got nil", tc.proxy)
			}
		})
	}

	mOK := make(map[string]string, len(baseProd)+1)
	for k, v := range baseProd {
		mOK[k] = v
	}
	mOK["TRUSTED_PROXIES"] = "127.0.0.1/32,10.20.0.0/16"
	cfgOK, err := config.Load(func(k string) (string, bool) {
		v, ok := mOK[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if err := cfgOK.ValidateProduction(); err != nil {
		t.Fatalf("Expected ValidateProduction to accept /32 and /16 TRUSTED_PROXIES, got: %v", err)
	}
}

func TestAuditEnvVarsAndProductionGuards(t *testing.T) {
	baseProd := map[string]string{
		"APP_ENV":                         "production",
		"DB_HOST":                         "10.20.1.5",
		"DB_PORT":                         "5432",
		"DB_NAME":                         "kopdes_prod",
		"DB_USER":                         "kopdes_app",
		"DB_PASSWORD":                     "ProdStrongPass123!@#",
		"DB_SSLMODE":                      "require",
		"BLIND_INDEX_SECRET":              strings.Repeat("b2", 32),
		"AES_MASTER_KEY":                  strings.Repeat("c3", 32),
		"BACKUP_AGE_RECIPIENT":            "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p",
		"ALLOWED_ORIGINS":                 "https://koperasi.example.id",
		"STOCK_HOLD_MINUTES":              "45",
		"COOKIE_SECURE":                   "true",
		"ALLOW_BEARER_AUTH":               "false",
		"STORAGE_BUKTI_DIR":               "./storage/bukti_custom",
		"BUKTI_RETENTION_DAYS":            "180",
		"DUAL_CONTROL_BREAKGLASS_ENABLED": "true",
	}
	cfg, err := config.Load(func(k string) (string, bool) {
		v, ok := baseProd[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("unexpected Load error: %v", err)
	}
	if cfg.StockHoldMinutes != 45 || cfg.CookieSecure != "true" || cfg.AllowBearerAuth ||
		cfg.StorageBuktiDir != "./storage/bukti_custom" || cfg.BuktiRetentionDays != 180 ||
		!cfg.DualControlBreakglassEnabled {
		t.Fatalf("audit config fields not bound properly: %+v", cfg)
	}
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("ValidateProduction failed: %v", err)
	}

	badCookie := make(map[string]string, len(baseProd))
	for k, v := range baseProd {
		badCookie[k] = v
	}
	badCookie["COOKIE_SECURE"] = "false"
	cBad, err := config.Load(func(k string) (string, bool) { return badCookie[k], true })
	if err != nil {
		t.Fatalf("unexpected Load error: %v", err)
	}
	if err := cBad.ValidateProduction(); err == nil {
		t.Fatal("expected ValidateProduction to reject COOKIE_SECURE=false")
	}

	badAge := make(map[string]string, len(baseProd))
	for k, v := range baseProd {
		badAge[k] = v
	}
	badAge["BACKUP_AGE_RECIPIENT"] = "not-an-age-pubkey"
	if _, err := config.Load(func(k string) (string, bool) { return badAge[k], true }); err == nil {
		t.Fatal("expected Load to reject invalid BACKUP_AGE_RECIPIENT prefix")
	}
}
