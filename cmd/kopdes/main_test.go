package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/testseed"
)

func TestDispatchCommand_UnknownAndValidation(t *testing.T) {
	ctx := context.Background()
	cfg := config.AppConfig{}

	if err := dispatchCommand(ctx, nil, cfg, []string{"unknown", "cmd"}); err == nil {
		t.Fatal("expected error for unknown command")
	}
	err := dispatchCommand(ctx, nil, cfg, []string{"admin", "create", "--email", "a@b.c", "--password", "short"})
	if err == nil || !strings.Contains(err.Error(), "10") {
		t.Fatalf("expected weak password validation error, got: %v", err)
	}
	err = dispatchCommand(ctx, nil, cfg, []string{"iuran", "generate", "--periode", "invalid"})
	if err == nil {
		t.Fatal("expected invalid periode error")
	}
}

func TestValidRoleID(t *testing.T) {
	if !validRoleID.MatchString(config.DefaultDBAppRole) {
		t.Fatalf("default DB role %q invalid", config.DefaultDBAppRole)
	}
	for _, bad := range []string{"", "1bad", "kopdes-app; DROP TABLE", "a b"} {
		if validRoleID.MatchString(bad) {
			t.Fatalf("expected %q to fail role regex", bad)
		}
	}
}

func TestRelocateLegacySubdirs_Idempotent(t *testing.T) {
	kycDir := filepath.Join(t.TempDir(), "kyc")
	buktiDir := filepath.Join(t.TempDir(), "bukti")
	legacyKYC := filepath.Join(kycDir, "kyc")
	legacyBukti := filepath.Join(kycDir, "bukti")
	if err := os.MkdirAll(legacyKYC, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacyBukti, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(buktiDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyKYC, "doc-1.enc"), []byte("enc-kyc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyBukti, "bukti_p1.enc"), []byte("enc-bukti"), 0o600); err != nil {
		t.Fatal(err)
	}

	movedK1, movedB1, err := relocateLegacySubdirs(kycDir, buktiDir)
	if err != nil || movedK1 != 1 || movedB1 != 1 {
		t.Fatalf("first relocate failed: movedK=%d movedB=%d err=%v", movedK1, movedB1, err)
	}
	if _, err := os.Stat(filepath.Join(kycDir, "doc-1.enc")); err != nil {
		t.Fatalf("expected migrated kyc file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(buktiDir, "bukti_p1.enc")); err != nil {
		t.Fatalf("expected migrated bukti file: %v", err)
	}

	// Panggilan kedua wajib idempoten (0 dipindahkan, tidak menimpa)
	movedK2, movedB2, err := relocateLegacySubdirs(kycDir, buktiDir)
	if err != nil || movedK2 != 0 || movedB2 != 0 {
		t.Fatalf("second relocate must be idempotent: movedK=%d movedB=%d err=%v", movedK2, movedB2, err)
	}
}

func TestAdminRecoveryCLI_ResetMFAAndResetPassword(t *testing.T) {
	if testing.Short() {
		t.Skip("melewati uji CLI DB pada mode -short")
	}
	envPath := filepath.Clean(filepath.Join("..", "..", ".env.test"))
	cfg, err := config.LoadFromEnvAndFile(envPath)
	if err != nil {
		t.Fatalf("LoadFromEnvAndFile: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		t.Fatalf("openPool: %v", err)
	}
	defer pool.Close()

	// Seed admin dulu agar tersedia akun target, lalu pakai akun admin sementara untuk uji reset
	if _, err := testseed.SeedAll(ctx, pool, cfg, ""); err != nil {
		t.Fatalf("SeedAll: %v", err)
	}
	recEmail := "recovery-test-" + time.Now().Format("150405.000") + "@kopdes-test.local"
	recHP := "0899" + time.Now().Format("04050000")[:8]
	if err := runAdminCreate(ctx, pool, cfg, []string{
		"--email", recEmail, "--nama", "Admin Recovery", "--hp", recHP, "--password", "SandiAwal1234!",
	}); err != nil {
		t.Fatalf("runAdminCreate: %v", err)
	}
	if err := dispatchCommand(ctx, pool, cfg, []string{"admin", "reset-mfa", "--email", recEmail}); err != nil {
		t.Fatalf("admin reset-mfa gagal: %v", err)
	}
	if err := dispatchCommand(ctx, pool, cfg, []string{"admin", "reset-password", "--email", recEmail, "--password", "SandiBaruKuat123!"}); err != nil {
		t.Fatalf("admin reset-password gagal: %v", err)
	}
	blindKey := security.DecodeSecretMaterial(cfg.BlindIndexSecret)
	emailHash := security.HashBlindIndex(recEmail, blindKey)
	u, err := db.New(pool).GetUserByEmailHash(ctx, response.ToPGText(emailHash))
	if err != nil {
		t.Fatalf("GetUserByEmailHash: %v", err)
	}
	if u.MfaEnabled || !security.CheckPassword("SandiBaruKuat123!", u.PasswordHash) {
		t.Fatalf("expected MFA disabled and password updated after CLI recovery, mfa=%v", u.MfaEnabled)
	}
	var auditCnt int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_log WHERE entitas_id = $1 AND aksi IN ('admin.cli_reset_mfa','admin.cli_reset_password')", response.FromPGUUID(u.ID).String()).Scan(&auditCnt); err != nil || auditCnt < 2 {
		t.Fatalf("expected >= 2 CLI audit entries, got %d err=%v", auditCnt, err)
	}
}
