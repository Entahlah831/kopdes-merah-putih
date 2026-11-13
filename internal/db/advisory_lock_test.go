package db_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	internaldb "github.com/kopdes/merah-putih/internal/db"
)

func TestAdvisoryLock_CanonicalSorting(t *testing.T) {
	input := []string{"stok_mutasi", "audit_log_y2026m11", "kas_mutasi", "audit_log", "pembayaran_verifikasi_log"}
	expected := []string{"audit_log", "kas_mutasi", "pembayaran_verifikasi_log", "stok_mutasi"}

	// Panggil AcquireOrderedAdvisoryLocks dalam dry-run / koneksi DB
	pool := getDBPool(t, "kopdes_app", "")
	defer pool.Close()

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx failed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tenantID := uuid.New()
	acquired, err := internaldb.AcquireOrderedAdvisoryLocks(ctx, tx, tenantID, input...)
	if err != nil {
		t.Fatalf("AcquireOrderedAdvisoryLocks failed: %v", err)
	}

	if !reflect.DeepEqual(acquired, expected) {
		t.Fatalf("Urutan kanonikal tidak sesuai! Expected %v, got %v", expected, acquired)
	}
	t.Logf("Sukses: Normalisasi partisi dan pengurutan leksikografis kanonikal valid: %v", acquired)
}

func TestAdvisoryLock_Ordered_PreventsDeadlock_UnderParallelLoad(t *testing.T) {
	pool := getDBPool(t, "kopdes_app", "")
	defer pool.Close()

	ctx := context.Background()
	tenantID := uuid.New()

	const iterations = 20
	var wg sync.WaitGroup
	errCh := make(chan error, iterations*2)

	// Dua kelompok worker menjalankan transaksi dengan parameter acak/terbalik,
	// namun keduanya melalui helper AcquireOrderedAdvisoryLocks sehingga bebas deadlock.
	for i := 0; i < iterations; i++ {
		wg.Add(2)

		// Worker 1: Masukan terbalik ["stok_mutasi", "kas_mutasi", "audit_log"]
		go func(workerID int) {
			defer wg.Done()
			tx, err := pool.Begin(ctx)
			if err != nil {
				errCh <- err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()

			_, err = internaldb.AcquireOrderedAdvisoryLocks(ctx, tx, tenantID, "stok_mutasi", "kas_mutasi", "audit_log")
			if err != nil {
				errCh <- err
				return
			}
			time.Sleep(10 * time.Millisecond)
			_ = tx.Commit(ctx)
		}(i)

		// Worker 2: Masukan berbeda ["audit_log", "kas_mutasi"]
		go func(workerID int) {
			defer wg.Done()
			tx, err := pool.Begin(ctx)
			if err != nil {
				errCh <- err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()

			_, err = internaldb.AcquireOrderedAdvisoryLocks(ctx, tx, tenantID, "audit_log", "kas_mutasi")
			if err != nil {
				errCh <- err
				return
			}
			time.Sleep(10 * time.Millisecond)
			_ = tx.Commit(ctx)
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("Terjadi deadlock atau error saat beban paralel dengan helper: %v", err)
		}
	}

	t.Logf("Sukses: %d worker paralel dengan urutan masukan terbalik berhasil dieksekusi tanpa satu pun deadlock!", iterations*2)
}

func TestAdvisoryLock_ReverseOrder_CausesDeadlockOrTimeout(t *testing.T) {
	pool := getDBPool(t, "kopdes_app", "")
	defer pool.Close()

	ctx := context.Background()
	tenantID := uuid.New()

	// Buktikan bahwa jika transaksi sengaja mengambil kunci advisory secara manual dengan urutan TERBALIK:
	// Tx 1: lock(audit_log) -> lock(kas_mutasi)
	// Tx 2: lock(kas_mutasi) -> lock(audit_log)
	// Maka PostgreSQL mendeteksi deadlock (40P01) atau lock timeout (55P03).

	lockQuery := internaldb.TableAdvisoryLockQuery()

	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx1 error: %v", err)
	}
	defer func() { _ = tx1.Rollback(ctx) }()

	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx2 error: %v", err)
	}
	defer func() { _ = tx2.Rollback(ctx) }()

	// Pasang lock_timeout pendek pada tx1 dan tx2 agar pengujian cepat dan deterministik
	if _, err := tx1.Exec(ctx, "SET LOCAL lock_timeout = '500ms';"); err != nil {
		t.Fatalf("Set lock_timeout tx1 error: %v", err)
	}
	if _, err := tx2.Exec(ctx, "SET LOCAL lock_timeout = '500ms';"); err != nil {
		t.Fatalf("Set lock_timeout tx2 error: %v", err)
	}

	// 1. Tx1 mengambil kunci 'audit_log'
	if _, err := tx1.Exec(ctx, lockQuery, "audit_log", tenantID.String()); err != nil {
		t.Fatalf("Tx1 lock audit_log error: %v", err)
	}

	// 2. Tx2 mengambil kunci 'kas_mutasi' (terbalik)
	if _, err := tx2.Exec(ctx, lockQuery, "kas_mutasi", tenantID.String()); err != nil {
		t.Fatalf("Tx2 lock kas_mutasi error: %v", err)
	}

	// 3. Kini kedua transaksi serentak meminta kunci milik transaksi lawan
	var wg sync.WaitGroup
	wg.Add(2)

	var errTx1, errTx2 error
	go func() {
		defer wg.Done()
		// Tx1 meminta 'kas_mutasi' (yang sedang dipegang Tx2)
		_, errTx1 = tx1.Exec(ctx, lockQuery, "kas_mutasi", tenantID.String())
	}()

	go func() {
		defer wg.Done()
		// Tx2 meminta 'audit_log' (yang sedang dipegang Tx1)
		_, errTx2 = tx2.Exec(ctx, lockQuery, "audit_log", tenantID.String())
	}()

	wg.Wait()

	// Salah satu transaksi wajib gagal karena deadlock (SQLSTATE 40P01) atau lock timeout (55P03)
	hasDeadlockOrTimeout := false
	for _, e := range []error{errTx1, errTx2} {
		if e != nil {
			errStr := strings.ToLower(e.Error())
			if strings.Contains(errStr, "deadlock") || strings.Contains(errStr, "lock timeout") || strings.Contains(errStr, "40p01") || strings.Contains(errStr, "55p03") {
				hasDeadlockOrTimeout = true
			}
		}
	}

	if !hasDeadlockOrTimeout {
		t.Fatalf("Uji urutan terbalik HARUS memicu deadlock (40P01) atau lock timeout (55P03), tetapi didapat tx1Err=%v, tx2Err=%v", errTx1, errTx2)
	}

	t.Logf("Sukses: Penguncian manual dengan urutan terbalik terbukti memicu kegagalan deadlock/timeout (tx1Err=%v, tx2Err=%v), membuktikan bahwa AcquireOrderedAdvisoryLocks mutlak diperlukan!", errTx1, errTx2)
}
