package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/testseed"
)

func setupWorkerTestPool(t *testing.T) (config.AppConfig, *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("melewati uji worker DB pada mode -short")
	}
	cfg, err := config.LoadFromEnvAndFile(filepath.Clean(filepath.Join("..", "..", ".env.test")))
	if err != nil {
		t.Fatalf("LoadFromEnvAndFile: %v", err)
	}
	cfg.StorageKYCDir = t.TempDir()
	cfg.StorageBuktiDir = t.TempDir()
	dsn, err := cfg.BuildDatabaseDSN()
	if err != nil {
		t.Fatalf("BuildDatabaseDSN: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := testseed.SeedAll(ctx, pool, cfg, ""); err != nil {
		t.Fatalf("SeedAll: %v", err)
	}
	return cfg, pool
}

func TestDecisionAudit_D8_TwoConcurrentWorkersNoDuplicateBills(t *testing.T) {
	cfg, pool := setupWorkerTestPool(t)
	ctx := context.Background()

	w1, err := New(pool, cfg)
	if err != nil {
		t.Fatalf("worker.New #1: %v", err)
	}
	w2, err := New(pool, cfg)
	if err != nil {
		t.Fatalf("worker.New #2: %v", err)
	}

	startGate := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for idx, w := range []*Worker{w1, w2} {
		wg.Add(1)
		go func(i int, wk *Worker) {
			defer wg.Done()
			<-startGate
			errs[i] = wk.RunOnce(ctx)
		}(idx, w)
	}
	close(startGate)
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("worker #%d RunOnce error: %v", i+1, e)
		}
	}

	now := time.Now().UTC()
	var totalRows, distinctPairs int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT (anggota_id::text || ':' || jenis_iuran_id::text))
		FROM tagihan_iuran
		WHERE periode_tahun = $1 AND periode_bulan = $2`,
		now.Year(), int(now.Month())).Scan(&totalRows, &distinctPairs)
	if err != nil {
		t.Fatalf("cek duplikasi tagihan: %v", err)
	}
	if totalRows != distinctPairs {
		t.Fatalf("ditemukan tagihan ganda saat 2 worker konkuren berjalan: total=%d distinct=%d", totalRows, distinctPairs)
	}
}

func TestDecisionAudit_D8_CrashRestartCatchUpAndReadyz(t *testing.T) {
	cfg, pool := setupWorkerTestPool(t)
	ctx := context.Background()

	// Sisipkan pesanan dengan hold stok kedaluwarsa & produk tertahan=2
	prodID := uuid.New()
	ordID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO produk (id, sku, nama, satuan, harga_umum, harga_anggota, stok, stok_tertahan, status)
		VALUES ($1, $2, 'Gula Worker Uji', 'kg', 16000, 15000, 10, 2, 'aktif')`,
		prodID, "WRK-"+uuid.NewString()[:8]); err != nil {
		t.Fatalf("insert produk: %v", err)
	}
	res := testseed.LookupResources(ctx, pool)
	var memberUserID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT user_id FROM anggota WHERE id = $1", res.AnggotaID).Scan(&memberUserID); err != nil {
		t.Fatalf("lookup user_id anggota: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO pesanan (
			id, nomor_pesanan, user_id, anggota_id, metode_pengiriman, metode_pembayaran,
			status, stok_status, stok_hold_expires_at,
			subtotal, diskon, ongkir, total_bayar,
			nama_penerima, hp_penerima_enc, hp_penerima_masked, alamat_pengantaran_enc, catatan, expires_at
		) VALUES (
			$1, $2, $3, $4, 'ambil_sendiri', 'qris',
			'menunggu_pembayaran', 'reserved', NOW() - INTERVAL '5 minutes',
			30000, 0, 0, 30000,
			'Warga Worker', $5, '0811****0004', $5, 'uji worker', NOW() - INTERVAL '5 minutes'
		)`, ordID, "ORD-WRK-"+uuid.NewString()[:6], memberUserID, res.AnggotaID, []byte("enc")); err != nil {
		t.Fatalf("insert pesanan: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO pesanan_item (pesanan_id, produk_id, nama_produk, kuantitas, harga_satuan, subtotal)
		VALUES ($1, $2, 'Gula Worker Uji', 2, 15000, 30000)`, ordID, prodID); err != nil {
		t.Fatalf("insert pesanan_item: %v", err)
	}

	// Worker #1 berjalan singkat lalu dihentikan paksa via cancel (simulasi crash/stop)
	w1, err := New(pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cCtx, cCancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	go func() {
		w1.Run(cCtx, 5*time.Minute)
		close(doneCh)
	}()
	time.Sleep(120 * time.Millisecond)
	cCancel()
	<-doneCh

	// Worker #2 hidup kembali dan catch up semua tugas
	w2, err := New(pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := w2.RunOnce(ctx); err != nil {
		t.Fatalf("worker #2 catch-up RunOnce failed: %v", err)
	}

	// Hold stok harus sudah dilepas kembali (stok_tertahan=0, stok_status='released')
	var tertahan int
	var stokStatus string
	if err := pool.QueryRow(ctx, "SELECT stok_tertahan FROM produk WHERE id = $1", prodID).Scan(&tertahan); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT stok_status FROM pesanan WHERE id = $1", ordID).Scan(&stokStatus); err != nil {
		t.Fatal(err)
	}
	if tertahan != 0 || stokStatus != "released" {
		t.Fatalf("expected hold released after catch-up, got stok_tertahan=%d stok_status=%s", tertahan, stokStatus)
	}

	// Pastikan /readyz memuat worker_jobs dan worker_degraded=false
	hh := handlers.NewHealthHandler(pool)
	rec := httptest.NewRecorder()
	hh.HandleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data := env["data"].(map[string]interface{})
	jobs := data["worker_jobs"].([]interface{})
	if len(jobs) < 5 || data["worker_degraded"] != false {
		t.Fatalf("expected >=5 healthy worker_jobs & worker_degraded=false on /readyz, got %v", data)
	}

	// Simulasi salah satu job berstatus error -> /readyz menandai worker_degraded=true
	_ = db.New(pool).UpsertWorkerRunFinish(ctx, db.UpsertWorkerRunFinishParams{
		JobName: "bukti_purge", LastStatus: "error", LastSummary: "", LastError: "simulasi gagal",
	})
	recDeg := httptest.NewRecorder()
	hh.HandleReadyz(recDeg, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var envDeg map[string]interface{}
	_ = json.Unmarshal(recDeg.Body.Bytes(), &envDeg)
	if envDeg["data"].(map[string]interface{})["worker_degraded"] != true {
		t.Fatalf("expected worker_degraded=true when job in error status, got %v", envDeg)
	}
	// Pulihkan kembali status ok
	_ = db.New(pool).UpsertWorkerRunFinish(ctx, db.UpsertWorkerRunFinishParams{
		JobName: "bukti_purge", LastStatus: "ok", LastSummary: "pulih", LastError: "",
	})
	_ = response.ToPGText("")
}

func TestWorker_ConcurrentTwoInstancesNoDuplicate(t *testing.T) {
	cfg, pool := setupWorkerTestPool(t)
	ctx := context.Background()

	// Sisipkan 1 sesi sudah kedaluwarsa untuk menguji tugas ke-6 session_cleanup
	var anyUserID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM users LIMIT 1").Scan(&anyUserID); err != nil {
		t.Fatalf("ambil user: %v", err)
	}
	expiredTok := "expired-worker-" + uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO sesi (user_id, token_hash, mfa_verified, scope, ip_address, user_agent, idle_expires_at, abs_expires_at)
		VALUES ($1, $2, TRUE, 'full', '127.0.0.1', 'worker-test', NOW() - INTERVAL '2 hours', NOW() - INTERVAL '1 hour')`,
		anyUserID, expiredTok); err != nil {
		t.Fatalf("sisip sesi kedaluwarsa: %v", err)
	}

	w1, err := New(pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	w2, err := New(pool, cfg)
	if err != nil {
		t.Fatal(err)
	}

	gate := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, w := range []*Worker{w1, w2} {
		wg.Add(1)
		go func(idx int, wk *Worker) {
			defer wg.Done()
			<-gate
			errs[idx] = wk.RunOnce(ctx)
		}(i, w)
	}
	close(gate)
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("worker #%d gagal: %v", i+1, e)
		}
	}

	var left int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM sesi WHERE token_hash = $1", expiredTok).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("expected expired session cleaned up by session_cleanup, remaining=%d", left)
	}

	// Pastikan endpoint health worker /healthz & /readyz merespons 200 dengan 6 job tercatat
	hh := w1.HealthHandler()
	recH := httptest.NewRecorder()
	hh.ServeHTTP(recH, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recH.Code != http.StatusOK {
		t.Fatalf("worker /healthz=%d body=%s", recH.Code, recH.Body.String())
	}
	recR := httptest.NewRecorder()
	hh.ServeHTTP(recR, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var readyEnv map[string]interface{}
	_ = json.Unmarshal(recR.Body.Bytes(), &readyEnv)
	if recR.Code != http.StatusOK || int(readyEnv["data"].(map[string]interface{})["jobs_recorded"].(float64)) < 6 {
		t.Fatalf("worker /readyz=%d body=%s", recR.Code, recR.Body.String())
	}
}
