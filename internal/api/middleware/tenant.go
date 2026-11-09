package middleware

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrMissingTenantContext = errors.New("konteks koperasi_id tidak ditemukan pada sesi transaksi")
)

// WithTenantTx menjalankan fungsi bisnis dalam sebuah transaksi database terisolasi
// yang secara wajib mengeksekusi SELECT set_config('app.current_koperasi_id', $1, true)
// sesuai Aturan Workspace 1 (Multi-Tenant & RLS).
func WithTenantTx(ctx context.Context, pool *pgxpool.Pool, koperasiID uuid.UUID, isPlatformAdmin bool, fn func(tx pgx.Tx) error) error {
	if koperasiID == uuid.Nil && !isPlatformAdmin {
		return ErrMissingTenantContext
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("gagal memulai transaksi pgx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Pastikan peran non-superuser aktif jika diatur
	_, _ = tx.Exec(ctx, "SET ROLE kopdes_app;")

	// Suntikkan koperasi_id ke session settings PostgreSQL 16
	if koperasiID != uuid.Nil {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", koperasiID.String()); err != nil {
			return fmt.Errorf("gagal menetapkan app.current_koperasi_id: %w", err)
		}
	}

	// Jika pengguna merupakan platform admin, izinkan bypass RLS
	if isPlatformAdmin {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'true', true)"); err != nil {
			return fmt.Errorf("gagal menetapkan app.is_platform_admin: %w", err)
		}
	}

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
