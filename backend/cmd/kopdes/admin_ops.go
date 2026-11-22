package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

func runAdminResetMFA(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, args []string) error {
	fs := flag.NewFlagSet("admin reset-mfa", flag.ContinueOnError)
	email := fs.String("email", "", "Email pengguna yang MFA-nya direset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	em := strings.ToLower(strings.TrimSpace(*email))
	if em == "" {
		return fmt.Errorf("--email wajib diisi")
	}
	if pool == nil {
		return fmt.Errorf("koneksi database belum tersedia")
	}
	q := db.New(pool)
	blindKey := security.DecodeSecretMaterial(cfg.BlindIndexSecret)
	u, err := q.GetUserByEmailHash(ctx, response.ToPGText(security.HashBlindIndex(em, blindKey)))
	if err != nil {
		return fmt.Errorf("pengguna tidak ditemukan: %w", err)
	}
	if err := q.ResetUserMFAByID(ctx, u.ID); err != nil {
		return fmt.Errorf("reset MFA gagal: %w", err)
	}
	if err := q.RevokeAllUserSessions(ctx, u.ID); err != nil {
		return fmt.Errorf("pencabutan sesi gagal: %w", err)
	}
	_ = q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: pgtype.UUID{},
		ActorPeran:  "owner_cli",
		Aksi:        "admin.cli_reset_mfa",
		Entitas:     "users",
		EntitasID:   response.FromPGUUID(u.ID).String(),
		Ringkasan:   fmt.Sprintf("CLI owner mereset MFA pengguna peran=%s; wajib setup MFA ulang saat login", u.Peran),
		IpAddress:   "local-cli",
	})
	fmt.Printf("MFA berhasil direset untuk id=%s (peran=%s); semua sesi aktif dicabut.\n", response.FromPGUUID(u.ID), u.Peran)
	return nil
}

func runAdminResetPassword(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, args []string) error {
	fs := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
	email := fs.String("email", "", "Email pengguna")
	password := fs.String("password", "", "Kata sandi baru (>=12 karakter)")
	fromStdin := fs.Bool("password-stdin", false, "Baca kata sandi baru dari stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	em := strings.ToLower(strings.TrimSpace(*email))
	newPwd, err := resolveCLIResetPassword(*password, *fromStdin)
	if err != nil {
		return err
	}
	if em == "" || len(newPwd) < 12 {
		return fmt.Errorf("--email dan kata sandi baru (>=12 karakter) wajib diisi")
	}
	if pool == nil {
		return fmt.Errorf("koneksi database belum tersedia")
	}
	q := db.New(pool)
	blindKey := security.DecodeSecretMaterial(cfg.BlindIndexSecret)
	u, err := q.GetUserByEmailHash(ctx, response.ToPGText(security.HashBlindIndex(em, blindKey)))
	if err != nil {
		return fmt.Errorf("pengguna tidak ditemukan: %w", err)
	}
	hash, err := security.HashPasswordWithCost(newPwd, 12)
	if err != nil {
		return fmt.Errorf("hash sandi: %w", err)
	}
	if err := q.UpdateUserSandiHash(ctx, db.UpdateUserSandiHashParams{ID: u.ID, PasswordHash: hash}); err != nil {
		return fmt.Errorf("perbarui sandi gagal: %w", err)
	}
	if err := q.RevokeAllUserSessions(ctx, u.ID); err != nil {
		return fmt.Errorf("pencabutan sesi gagal: %w", err)
	}
	_ = q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: pgtype.UUID{},
		ActorPeran:  "owner_cli",
		Aksi:        "admin.cli_reset_password",
		Entitas:     "users",
		EntitasID:   response.FromPGUUID(u.ID).String(),
		Ringkasan:   fmt.Sprintf("CLI owner mereset kata sandi pengguna peran=%s", u.Peran),
		IpAddress:   "local-cli",
	})
	fmt.Printf("Kata sandi direset untuk id=%s (peran=%s); semua sesi aktif dicabut.\n", response.FromPGUUID(u.ID), u.Peran)
	return nil
}

func resolveCLIResetPassword(flagVal string, fromStdin bool) (string, error) {
	if fromStdin {
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		if err != nil {
			return "", fmt.Errorf("baca stdin: %w", err)
		}
		return strings.TrimRight(string(raw), "\r\n"), nil
	}
	return flagVal, nil
}
