package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckDatabaseRole memeriksa hak akses role basis data saat startup.
// Menolak jika role adalah superuser atau memiliki rolbypassrls,
// kecuali allowSuperuser bernilai true (hanya untuk testing dev dengan flag khusus).
func CheckDatabaseRole(ctx context.Context, pool *pgxpool.Pool, allowSuperuser bool) error {
	var (
		rolname      string
		rolsuper     bool
		rolbypassrls bool
	)

	err := pool.QueryRow(ctx, `
		SELECT rolname, rolsuper, rolbypassrls
		FROM pg_roles
		WHERE rolname = current_user;
	`).Scan(&rolname, &rolsuper, &rolbypassrls)
	if err != nil {
		return fmt.Errorf("gagal memeriksa hak akses role basis data: %w", err)
	}

	if (rolsuper || rolbypassrls) && !allowSuperuser {
		return fmt.Errorf("PELANGGARAN KEAMANAN KRITIS (K1): Aplikasi dilarang berjalan sebagai role superuser/bypassrls ('%s', rolsuper=%t, rolbypassrls=%t) karena RLS akan di-bypass", rolname, rolsuper, rolbypassrls)
	}

	return nil
}
