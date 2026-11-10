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

func TestTask2_SQLFunction_EnsureFuturePartitions(t *testing.T) {
	ctx := context.Background()
	// Wajib dapat dijalankan oleh role kopdes_app non-superuser
	pool := getDBPool(t, "kopdes_app", "kopdes_dev_password")
	defer pool.Close()

	// 1. Eksekusi fungsi SQL idempoten fn_ensure_future_partitions(3)
	var resultJSON string
	err := pool.QueryRow(ctx, "SELECT fn_ensure_future_partitions(3)::text;").Scan(&resultJSON)
	if err != nil {
		t.Fatalf("Gagal memanggil fn_ensure_future_partitions sebagai kopdes_app: %v", err)
	}
	t.Logf("Hasil pemanggilan fn_ensure_future_partitions(3): %s", resultJSON)

	// 2. Eksekusi ulang untuk verifikasi idempotensi
	err = pool.QueryRow(ctx, "SELECT fn_ensure_future_partitions(3)::text;").Scan(&resultJSON)
	if err != nil {
		t.Fatalf("Pemanggilan kedua fn_ensure_future_partitions gagal (tidak idempoten): %v", err)
	}

	// 3. Pastikan partisi untuk 4 tabel tercover: audit_log, kas_mutasi, stok_mutasi, notifikasi
	tables := []string{"audit_log", "kas_mutasi", "stok_mutasi", "notifikasi"}
	for _, tbl := range tables {
		var partCount int
		err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM pg_inherits
			WHERE inhparent = $1::regclass;
		`, tbl).Scan(&partCount)
		if err != nil {
			t.Fatalf("Gagal memeriksa partisi untuk tabel %s: %v", tbl, err)
		}
		if partCount < 3 {
			t.Errorf("Tabel %s hanya memiliki %d partisi, minimal 3", tbl, partCount)
		}
	}
}

func TestTask2_PartitionMetrics_And_Alert(t *testing.T) {
	ctx := context.Background()
	pool := getDBPool(t, "kopdes_app", "kopdes_dev_password")
	defer pool.Close()

	// Verifikasi helper metrik kalkulasi partisi
	metrics, err := internaldb.GetPartitionMetrics(ctx, pool)
	if err != nil {
		t.Fatalf("GetPartitionMetrics gagal: %v", err)
	}

	if metrics.AuditLogDefaultRows != 0 || metrics.KasMutasiDefaultRows != 0 ||
		metrics.StokMutasiDefaultRows != 0 || metrics.NotifikasiDefaultRows != 0 {
		t.Errorf("Partisi default harus memiliki 0 baris, didapat: %+v", metrics)
	}

	if metrics.MinMonthsRemaining < 3 {
		t.Errorf("Bulan tersisa partisi minimal harus >= 3, didapat %d", metrics.MinMonthsRemaining)
	}

	if metrics.AlertActive {
		t.Errorf("Peringatan partisi tidak boleh aktif dalam kondisi normal")
	}
}

