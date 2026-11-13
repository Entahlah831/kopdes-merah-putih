package db_test

import (
	"testing"

	internaldb "github.com/kopdes/merah-putih/internal/db"
)

func TestValidateTestDatabaseDSN_RejectsNonTestDB(t *testing.T) {
	invalidDSNs := []string{
		"",
		"postgres://kopdes_app:pass@127.0.0.1:5432/kopdes_db?sslmode=disable",
		"postgres://kopdes_app:pass@127.0.0.1:5432/kopdes_prod?sslmode=disable",
		"postgres://kopdes_app:pass@127.0.0.1:5432/postgres?sslmode=disable",
		"postgres://kopdes_app:pass@127.0.0.1:5432/?sslmode=disable",
	}
	for _, dsn := range invalidDSNs {
		if err := internaldb.ValidateTestDatabaseDSN(dsn); err == nil {
			t.Errorf("Expected error for non-test DSN %q, got nil", dsn)
		}
	}

	validDSNs := []string{
		"postgres://kopdes_app:pass@127.0.0.1:5432/kopdes_test?sslmode=disable",
		"postgres://kopdes_app:pass@127.0.0.1:15433/kopdes_alt_test?sslmode=disable",
		"postgres://kopdes_app:pass@postgres:5432/ci_test",
	}
	for _, dsn := range validDSNs {
		if err := internaldb.ValidateTestDatabaseDSN(dsn); err != nil {
			t.Errorf("Expected nil error for valid test DSN %q, got: %v", dsn, err)
		}
	}
}

func TestResolveTestDatabaseDSN_RefusesMasterEnvFile(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "postgres://kopdes_app:pass@127.0.0.1:5433/kopdes_db?sslmode=disable")
	if _, err := internaldb.ResolveTestDatabaseDSN("", ""); err == nil {
		t.Fatalf("Expected ResolveTestDatabaseDSN to reject non-_test database kopdes_db")
	}

	t.Setenv("TEST_DATABASE_URL", "postgres://kopdes_app:pass@127.0.0.1:5433/kopdes_test?sslmode=disable")
	dsn, err := internaldb.ResolveTestDatabaseDSN("", "")
	if err != nil {
		t.Fatalf("Expected valid *_test DSN, got error: %v", err)
	}
	if dsn == "" {
		t.Fatalf("Expected non-empty DSN")
	}
}
