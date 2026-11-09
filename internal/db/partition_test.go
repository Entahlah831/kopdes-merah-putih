package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	internaldb "github.com/kopdes/merah-putih/internal/db"
)

func TestK2_InsertOutsidePartition_SucceedsAfterMigration(t *testing.T) {
	ctx := context.Background()
	pool := getDBPool(t, "kopdes_app", "kopdes_dev_password")
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	defer conn.Release()

	koperasiID := uuid.New()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	_, _ = tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true);", koperasiID.String())
	_, _ = tx.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'true', true);")

	// 1. Uji tanggal 2027 (masuk ke partisi spesifik y2027m02)
	time2027, _ := time.Parse(time.RFC3339, "2027-02-15T10:00:00Z")
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (
			koperasi_id, nama_tabel, record_id, aksi, created_at
		) VALUES (
			$1, 'users', $2, 'INSERT', $3
		);
	`, koperasiID, uuid.New(), time2027)
	if err != nil {
		t.Fatalf("Gagal insert ke partisi 2027: %v", err)
	}

	// 2. Uji tanggal masa depan jauh 2029 (masuk ke partisi DEFAULT tanpa error)
	time2029, _ := time.Parse(time.RFC3339, "2029-08-20T12:00:00Z")
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (
			koperasi_id, nama_tabel, record_id, aksi, created_at
		) VALUES (
			$1, 'users', $2, 'INSERT', $3
		);
	`, koperasiID, uuid.New(), time2029)
	if err != nil {
		t.Fatalf("Gagal insert ke partisi DEFAULT 2029: %v", err)
	}

	t.Logf("Sukses: Data 2027 dan 2029 berhasil masuk ke partisi masing-masing tanpa error!")
}

func TestK2_EnsurePartitionsAhead_Idempotent(t *testing.T) {
	ctx := context.Background()
	pool := getDBPool(t, "postgres", "kopdes_dev_password")
	defer pool.Close()

	// Pastikan partisi 3 bulan ke depan selalu ada
	err := internaldb.EnsurePartitionsAhead(ctx, pool, 3)
	if err != nil {
		t.Fatalf("EnsurePartitionsAhead gagal: %v", err)
	}

	// Jalankan sekali lagi untuk menguji idempotensi (tidak boleh error jika sudah ada)
	err = internaldb.EnsurePartitionsAhead(ctx, pool, 3)
	if err != nil {
		t.Fatalf("EnsurePartitionsAhead pemanggilan kedua gagal: %v", err)
	}
}
