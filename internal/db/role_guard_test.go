package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	internaldb "github.com/kopdes/merah-putih/internal/db"
)

func getDBPool(t *testing.T, user, password string) *pgxpool.Pool {
	dsn := "postgres://" + user + ":" + password + "@127.0.0.1:5433/kopdes_db?sslmode=disable"
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("Parse config error: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("Ping error: %v", err)
	}
	return pool
}

func TestK1_SuperuserRejectedOnStartup(t *testing.T) {
	ctx := context.Background()

	// 1. Koneksi sebagai superuser 'postgres'
	poolSuper := getDBPool(t, "postgres", "kopdes_dev_password")
	defer poolSuper.Close()

	// Saat allowSuperuser = false, fungsi wajib MENOLAK koneksi superuser
	err := internaldb.CheckDatabaseRole(ctx, poolSuper, false)
	if err == nil {
		t.Fatalf("Harusnya GAGAL: Aplikasi tidak boleh mengizinkan superuser berjalan saat allowSuperuser = false")
	}
	t.Logf("Sukses ditolak: %v", err)

	// Saat allowSuperuser = true (flag dev), diizinkan
	if err := internaldb.CheckDatabaseRole(ctx, poolSuper, true); err != nil {
		t.Fatalf("Gagal: allowSuperuser=true harusnya diizinkan untuk dev, dapat: %v", err)
	}
}

func TestK1_AppRole_NativeRLS_WithoutSetRole(t *testing.T) {
	ctx := context.Background()

	// 2. Koneksi sebagai non-superuser 'kopdes_app' APA ADANYA (tanpa SET ROLE)
	poolApp := getDBPool(t, "kopdes_app", "kopdes_dev_password")
	defer poolApp.Close()

	// Periksa role
	if err := internaldb.CheckDatabaseRole(ctx, poolApp, false); err != nil {
		t.Fatalf("kopdes_app bukan superuser, harusnya lolos: %v", err)
	}

	// 3. Tes isolasi tenant:
	koperasiA := uuid.New()
	koperasiB := uuid.New()

	conn, err := poolApp.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	// Tanpa app.current_koperasi_id disetel, query ke tabel tenant WAJIB mengembalikan 0 baris!
	tx, err := poolApp.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	// Pastikan sesi bersih tanpa config
	_, _ = tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', '', true);")
	_, _ = tx.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', true);")

	var rowCount int
	err = tx.QueryRow(ctx, "SELECT count(*) FROM rekening_qris_koperasi;").Scan(&rowCount)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}

	if rowCount != 0 {
		t.Errorf("PELANGGARAN RLS: Tanpa app.current_koperasi_id, query tenant harusnya 0 baris, tetapi dapat %d baris!", rowCount)
	}

	_ = koperasiA
	_ = koperasiB
}
