package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/iuran"
	"github.com/kopdes/merah-putih/internal/pembayaran"
	"github.com/kopdes/merah-putih/internal/pendaftaran"
	"github.com/kopdes/merah-putih/internal/pesanan"
	"github.com/kopdes/merah-putih/internal/security"
)

const (
	lockKeyPesananExpire     int64 = 810001
	lockKeyIuranGenerate     int64 = 810002
	lockKeyPDPPurge          int64 = 810003
	lockKeyBuktiPurge        int64 = 810004
	lockKeyRateLimitsCleanup int64 = 810005
)

type jobSpec struct {
	name    string
	lockKey int64
	fn      func(context.Context) (string, error)
}

// Worker menjalankan tugas berkala di dalam kontainer dengan advisory lock PostgreSQL.
type Worker struct {
	pool    *pgxpool.Pool
	cfg     config.AppConfig
	pesSvc  *pesanan.Service
	paySvc  *pembayaran.Service
	iurSvc  *iuran.Service
	pendSvc *pendaftaran.Service
	jobs    []jobSpec
}

// New menginisialisasi worker terjadwal beserta layanan domain terkait.
func New(pool *pgxpool.Pool, cfg config.AppConfig) (*Worker, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool database wajib tersedia untuk worker")
	}
	kpSet, err := security.NewPurposeKeySet(cfg.AESMasterKey, cfg.AESPreviousKeys)
	if err != nil {
		return nil, fmt.Errorf("inisialisasi kunci worker: %w", err)
	}
	kycStore, err := pendaftaran.NewKYCFileStore(cfg.StorageKYCDir, kpSet.KYC, 5<<20)
	if err != nil {
		return nil, fmt.Errorf("inisialisasi storage KYC worker: %w", err)
	}
	paySvc, err := pembayaran.NewService(pool, pembayaran.Config{
		PaymentExpiry:      cfg.PaymentExpiryDuration,
		ProofStorageDir:    cfg.StorageBuktiDir,
		BuktiRetentionDays: cfg.BuktiRetentionDays,
		KeyProvider:        kpSet.Bukti,
		DBKeyProvider:      kpSet.DB,
		LegacyKeyProvider:  kpSet.Master,
		Timezone:           cfg.AppTimezone,
	})
	if err != nil {
		return nil, fmt.Errorf("inisialisasi layanan pembayaran worker: %w", err)
	}
	w := &Worker{
		pool:    pool,
		cfg:     cfg,
		pesSvc:  pesanan.NewService(pool, kpSet.DB, pesanan.Config{StockHoldMinutes: cfg.StockHoldMinutes, Timezone: cfg.AppTimezone}),
		paySvc:  paySvc,
		iurSvc:  iuran.NewService(pool, cfg.TrustedProxies),
		pendSvc: pendaftaran.NewService(pool, kycStore, pendaftaran.Config{KeyProvider: kpSet.DB, RetentionDays: cfg.PDPRejectedRetentionDays}),
	}
	w.jobs = []jobSpec{
		{name: "pesanan_expire", lockKey: lockKeyPesananExpire, fn: w.runPesananExpire},
		{name: "iuran_generate", lockKey: lockKeyIuranGenerate, fn: w.runIuranGenerate},
		{name: "pdp_purge", lockKey: lockKeyPDPPurge, fn: w.runPDPPurge},
		{name: "bukti_purge", lockKey: lockKeyBuktiPurge, fn: w.runBuktiPurge},
		{name: "rate_limits_cleanup", lockKey: lockKeyRateLimitsCleanup, fn: w.runRateLimitsCleanup},
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
		JobName:     jobName,
		LastStatus:  status,
		LastSummary: truncateMsg(summary, 300),
		LastError:   errMsg,
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
	deleted, err := w.pendSvc.PurgeExpiredRejected(ctx, w.cfg.PDPRejectedRetentionDays)
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
	deleted, err := db.New(w.pool).CleanupExpiredRateLimits(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("rate_limits_dibersihkan=%d", deleted), nil
}

func truncateMsg(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
