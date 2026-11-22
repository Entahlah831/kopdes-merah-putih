package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/anggota"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/auth"
	authapi "github.com/kopdes/merah-putih/internal/auth/api"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/filestore"
	"github.com/kopdes/merah-putih/internal/iuran"
	"github.com/kopdes/merah-putih/internal/katalog"
	"github.com/kopdes/merah-putih/internal/pembayaran"
	"github.com/kopdes/merah-putih/internal/pengaturan"
	"github.com/kopdes/merah-putih/internal/pesanan"
	"github.com/kopdes/merah-putih/internal/security"
)

const (
	lockKeyPesananExpire     int64 = 810001
	lockKeyIuranGenerate     int64 = 810002
	lockKeyPDPPurge          int64 = 810003
	lockKeyBuktiPurge        int64 = 810004
	lockKeyRateLimitsCleanup int64 = 810005
	lockKeySessionCleanup    int64 = 810006
)

type jobSpec struct {
	name    string
	lockKey int64
	fn      func(context.Context) (string, error)
}

// Worker menjalankan tugas berkala di dalam kontainer dengan advisory lock PostgreSQL.
type Worker struct {
	pool        *pgxpool.Pool
	cfg         config.AppConfig
	authCleaner authapi.MaintenanceCleaner
	angSvc      *anggota.Service
	pesSvc      *pesanan.Service
	paySvc      *pembayaran.Service
	iurSvc      *iuran.Service
	jobs        []jobSpec
}

// New menginisialisasi worker terjadwal beserta layanan domain terkait secara modular.
func New(pool *pgxpool.Pool, cfg config.AppConfig) (*Worker, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool database wajib tersedia untuk worker")
	}
	kpSet, err := security.NewPurposeKeySet(cfg.AESMasterKey, cfg.AESPreviousKeys)
	if err != nil {
		return nil, fmt.Errorf("inisialisasi kunci worker: %w", err)
	}
	fstore, err := buildWorkerFileStore(cfg, kpSet)
	if err != nil {
		return nil, err
	}
	return assembleWorker(pool, cfg, kpSet, fstore)
}

func buildWorkerFileStore(cfg config.AppConfig, kpSet *security.PurposeKeySet) (filestore.FileStore, error) {
	kycDir := strings.TrimSpace(cfg.StorageKYCDir)
	if kycDir == "" {
		kycDir = "./storage/kyc"
	}
	buktiDir := strings.TrimSpace(cfg.StorageBuktiDir)
	if buktiDir == "" {
		buktiDir = "./storage/bukti"
	}
	return filestore.NewLocalFileStore(map[string]filestore.NamespaceConfig{
		filestore.NamespaceKYC: {Dir: kycDir, KeyProvider: kpSet.KYC, LegacyKeyProvider: kpSet.Master},
		filestore.NamespaceBukti: {
			Dir: buktiDir, FallbackDirs: []string{filepath.Join(kycDir, "bukti")},
			KeyProvider: kpSet.Bukti, LegacyKeyProvider: kpSet.Master,
		},
	})
}

func assembleWorker(pool *pgxpool.Pool, cfg config.AppConfig, kpSet *security.PurposeKeySet, fstore filestore.FileStore) (*Worker, error) {
	pengSvc := pengaturan.NewService(pool, cfg.TrustedProxies)
	blindKey := security.DecodeSecretMaterial(cfg.BlindIndexSecret)
	authSvc := auth.NewService(pool, auth.Config{
		AppEnv: cfg.AppEnv, KeyProvider: kpSet.DB, BlindIndexSecret: blindKey, Audit: pengSvc,
	}, nil)
	angSvc := anggota.NewService(pool, anggota.Config{
		KeyProvider: kpSet.DB, BlindIndexSecret: blindKey,
		RetentionDays: cfg.PDPRejectedRetentionDays, FileStore: fstore, UserManager: authSvc, Audit: pengSvc,
	})
	authSvc.SetMembers(angSvc)
	katSvc := katalog.NewService(pool, cfg.TrustedProxies, angSvc, pengSvc)
	iurSvc := iuran.NewService(pool, cfg.TrustedProxies, angSvc, pengSvc)
	pesSvc := pesanan.NewService(pool, kpSet.DB, pesanan.Config{
		StockHoldMinutes: cfg.StockHoldMinutes, Timezone: cfg.AppTimezone,
		Settings: pengSvc, Audit: pengSvc, Members: angSvc, Stock: katSvc,
	})
	paySvc, err := pembayaran.NewService(pool, pembayaran.Config{
		PaymentExpiry: cfg.PaymentExpiryDuration, ProofStorageDir: cfg.StorageBuktiDir,
		BuktiRetentionDays: cfg.BuktiRetentionDays, KeyProvider: kpSet.Bukti,
		DBKeyProvider: kpSet.DB, LegacyKeyProvider: kpSet.Master, FileStore: fstore,
		Settings: pengSvc, Audit: pengSvc, Members: angSvc, Bills: iurSvc, Orders: pesSvc, BreakglassTOTP: authSvc,
		Timezone: cfg.AppTimezone,
	})
	if err != nil {
		return nil, fmt.Errorf("inisialisasi layanan pembayaran worker: %w", err)
	}
	pesSvc.SetActivePaymentChecker(paySvc)
	w := &Worker{pool: pool, cfg: cfg, authCleaner: authSvc, angSvc: angSvc, pesSvc: pesSvc, paySvc: paySvc, iurSvc: iurSvc}
	w.jobs = []jobSpec{
		{name: "pesanan_expire", lockKey: lockKeyPesananExpire, fn: w.runPesananExpire},
		{name: "iuran_generate", lockKey: lockKeyIuranGenerate, fn: w.runIuranGenerate},
		{name: "pdp_purge", lockKey: lockKeyPDPPurge, fn: w.runPDPPurge},
		{name: "bukti_purge", lockKey: lockKeyBuktiPurge, fn: w.runBuktiPurge},
		{name: "rate_limits_cleanup", lockKey: lockKeyRateLimitsCleanup, fn: w.runRateLimitsCleanup},
		{name: "session_cleanup", lockKey: lockKeySessionCleanup, fn: w.runSessionCleanup},
	}
	return w, nil
}

// Run menjalankan loop penjadwalan worker hingga context dibatalkan secara anggun.
func (w *Worker) Run(ctx context.Context, tickInterval time.Duration) {
	if tickInterval <= 0 {
		tickInterval = 60 * time.Second
	}
	var wg sync.WaitGroup
	executeTick := func() {
		wg.Add(1)
		defer wg.Done()
		if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[worker] kesalahan tick: %v", err)
		}
	}
	executeTick()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
			executeTick()
		}
	}
}

// HealthHandler menyediakan endpoint liveness & readiness HTTP internal untuk worker.
func (w *Worker) HealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(rw http.ResponseWriter, r *http.Request) {
		response.JSON(rw, http.StatusOK, map[string]interface{}{
			"status": "UP", "service": "worker", "timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("GET /readyz", func(rw http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if w == nil || w.pool == nil || w.pool.Ping(ctx) != nil {
			response.Error(rw, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "Database worker tidak tersedia")
			return
		}
		runs, _ := db.New(w.pool).ListWorkerRuns(ctx)
		response.JSON(rw, http.StatusOK, map[string]interface{}{
			"status": "READY", "jobs_recorded": len(runs), "timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})
	return mux
}

// StartHealthServer menjalankan endpoint health worker pada loopback hingga ctx selesai.
func (w *Worker) StartHealthServer(ctx context.Context, addr string) error {
	if strings.TrimSpace(addr) == "" {
		if w.cfg.WorkerPort <= 0 {
			return nil
		}
		addr = fmt.Sprintf("127.0.0.1:%d", w.cfg.WorkerPort)
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           w.HealthHandler(),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("[worker] endpoint health berjalan pada http://%s/healthz", addr) // #nosec G706 -- addr berasal dari WorkerPort tervalidasi
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		sCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		return srv.Shutdown(sCtx)
	case err := <-errCh:
		return err
	}
}

// RunOnce mengeksekusi satu siklus semua tugas dengan pg_try_advisory_lock per tugas.
func (w *Worker) RunOnce(ctx context.Context) error {
	var firstErr error
	for _, job := range w.jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := w.runJobWithLock(ctx, job); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (w *Worker) runJobWithLock(ctx context.Context, job jobSpec) error {
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("%s acquire conn: %w", job.name, err)
	}
	defer conn.Release()

	cq := db.New(conn)
	acquired, err := cq.TryAdvisoryLock(ctx, job.lockKey)
	if err != nil {
		return fmt.Errorf("%s try lock: %w", job.name, err)
	}
	if !acquired {
		return nil
	}
	defer releaseAdvisoryLock(conn, cq, job.lockKey)

	_ = cq.UpsertWorkerRunStart(ctx, job.name)
	summary, runErr := job.fn(ctx)
	w.finishJobRecord(cq, job.name, summary, runErr)
	return runErr
}

func releaseAdvisoryLock(_ interface{}, cq *db.Queries, lockKey int64) {
	uctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = cq.AdvisoryUnlock(uctx, lockKey)
}

func (w *Worker) finishJobRecord(cq *db.Queries, jobName, summary string, runErr error) {
	status := "ok"
	errMsg := ""
	if runErr != nil {
		status = "error"
		errMsg = truncateMsg(runErr.Error(), 300)
	}
	fctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = cq.UpsertWorkerRunFinish(fctx, db.UpsertWorkerRunFinishParams{
		JobName: jobName, LastStatus: status, LastSummary: truncateMsg(summary, 300), LastError: errMsg,
	})
}

func (w *Worker) runPesananExpire(ctx context.Context) (string, error) {
	ordCount, err := w.pesSvc.ExpireUnpaidOrders(ctx)
	if err != nil {
		return "", err
	}
	holdCount, err := w.pesSvc.ReleaseExpiredStockHolds(ctx)
	if err != nil {
		return "", err
	}
	payCount, err := w.paySvc.ExpirePendingPayments(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pesanan=%d tahanan_stok=%d pembayaran=%d", ordCount, holdCount, payCount), nil
}

func (w *Worker) runIuranGenerate(ctx context.Context) (string, error) {
	now := time.Now().UTC()
	if loc, err := time.LoadLocation(w.cfg.AppTimezone); err == nil {
		now = now.In(loc)
	}
	yr := now.Year()
	if yr < 2000 || yr > 9999 {
		return "", fmt.Errorf("tahun sistem di luar jangkauan: %d", yr)
	}
	bl := int(now.Month())
	res, err := w.iurSvc.GenerateMonthlyBills(ctx, int32(yr), int32(bl)) // #nosec G115 -- tahun 2000..9999 dan bulan 1..12 aman dalam int32
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("periode=%04d-%02d dibuat=%d dilewati=%d", res.PeriodeTahun, res.PeriodeBulan, res.Dibuat, res.Dilewati), nil
}

func (w *Worker) runPDPPurge(ctx context.Context) (string, error) {
	deleted, err := w.angSvc.PurgeExpiredRejected(ctx, w.cfg.PDPRejectedRetentionDays)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ditolak_dihapus=%d", deleted), nil
}

func (w *Worker) runBuktiPurge(ctx context.Context) (string, error) {
	purged, err := w.paySvc.PurgeExpiredBukti(ctx, w.cfg.BuktiRetentionDays)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("bukti_dihapus=%d", purged), nil
}

func (w *Worker) runRateLimitsCleanup(ctx context.Context) (string, error) {
	deleted, err := w.authCleaner.CleanupExpiredRateLimits(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("rate_limits_dibersihkan=%d", deleted), nil
}

func (w *Worker) runSessionCleanup(ctx context.Context) (string, error) {
	deleted, err := w.authCleaner.CleanupExpiredSessions(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sesi_kedaluwarsa_dihapus=%d", deleted), nil
}

func truncateMsg(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
