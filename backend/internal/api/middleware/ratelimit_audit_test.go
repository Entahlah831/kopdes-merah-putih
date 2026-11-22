package middleware

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/config"
)

func openMiddlewareTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("melewati uji rate_limits DB pada mode -short")
	}
	cfg, err := config.LoadFromEnvAndFile(filepath.Clean(filepath.Join("..", "..", "..", "..", ".env.test")))
	if err != nil {
		t.Fatalf("LoadFromEnvAndFile: %v", err)
	}
	dsn, err := cfg.BuildDatabaseDSN()
	if err != nil {
		t.Fatalf("BuildDatabaseDSN: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestDecisionAudit_D6_NoGoroutineLeakAndBoundedLRU(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 8; i++ {
		lim := NewIPRateLimiterWithConfig(10, 20, nil, 64)
		lim.Allow(fmt.Sprintf("198.51.100.%d", i))
	}
	time.Sleep(15 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+1 {
		t.Fatalf("goroutine leak detected after creating 8 IPRateLimiters: before=%d after=%d", before, after)
	}
}

func TestDecisionAudit_D6_Hybrid500FakeIPsZeroDBAmplification(t *testing.T) {
	pool := openMiddlewareTestPool(t)
	ctx := context.Background()
	runID := uuid.NewString()[:8]
	pureCat := "d6pure-" + runID
	hybCat := "d6hyb-" + runID
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM rate_limits WHERE kategori IN ($1, $2)", pureCat, hybCat)
	}()

	// 1. Ukur DBRateLimiter murni saat menerima 500 IP palsu acak (1 req per IP)
	pure := NewDBRateLimiter(pool, nil)
	pureStart := time.Now()
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("%s:198.51.%d.%d", pureCat, i/256, i%256)
		allowed, _, err := pure.Allow(ctx, key, pureCat, 20, 3600)
		if err != nil || !allowed {
			t.Fatalf("pure DBRateLimiter i=%d allowed=%v err=%v", i, allowed, err)
		}
	}
	pureDur := time.Since(pureStart)

	var pureRows int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM rate_limits WHERE kategori = $1", pureCat).Scan(&pureRows); err != nil {
		t.Fatal(err)
	}

	// 2. Ukur HybridRateLimiter pada skenario sama: 500 IP palsu -> 0 tulis DB!
	hybrid := NewHybridRateLimiter(pool, nil, 4096)
	hybStart := time.Now()
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("%s:198.51.%d.%d", hybCat, i/256, i%256)
		allowed, _, err := hybrid.Allow(ctx, key, hybCat, 20, 3600)
		if err != nil || !allowed {
			t.Fatalf("hybrid Allow i=%d allowed=%v err=%v", i, allowed, err)
		}
	}
	hybDur := time.Since(hybStart)

	var hybRows int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM rate_limits WHERE kategori = $1", hybCat).Scan(&hybRows); err != nil {
		t.Fatal(err)
	}

	t.Logf("D6 benchmark 500 fake IPs: pure_db_rows=%d (%v) vs hybrid_db_rows=%d (%v)", pureRows, pureDur, hybRows, hybDur)
	if pureRows != 500 {
		t.Fatalf("expected 500 rows for pure DBRateLimiter baseline, got %d", pureRows)
	}
	if hybRows != 0 {
		t.Fatalf("expected 0 DB writes for 500 one-off fake IPs under HybridRateLimiter, got %d", hybRows)
	}
}

func TestDecisionAudit_D6_PersistOnThresholdAndCGNATIsolation(t *testing.T) {
	pool := openMiddlewareTestPool(t)
	ctx := context.Background()
	cat := "d6blk-" + uuid.NewString()[:8]
	badKey := cat + ":203.0.113.55"
	goodKey := cat + ":203.0.113.56"
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM rate_limits WHERE kategori = $1", cat)
	}()

	h1 := NewHybridRateLimiter(pool, nil, 1024)
	for i := 1; i <= 3; i++ {
		ok, _, err := h1.Allow(ctx, badKey, cat, 3, 600)
		if err != nil || !ok {
			t.Fatalf("hit %d expected allowed=true, ok=%v err=%v", i, ok, err)
		}
	}
	// Hit ke-4 menembus batas -> ditolak & dipersist ke DB rate_limits
	ok4, _, err := h1.Allow(ctx, badKey, cat, 3, 600)
	if err != nil || ok4 {
		t.Fatalf("hit 4 expected blocked (allowed=false), got ok=%v err=%v", ok4, err)
	}

	// Instance baru (simulasi restart proses dengan RAM kosong) memuat blokir aktif dari DB
	h2 := NewHybridRateLimiter(pool, nil, 1024)
	okAfterRestart, _, _ := h2.Allow(ctx, badKey, cat, 3, 600)
	if okAfterRestart {
		t.Fatal("blocked key must persist across HybridRateLimiter restart via rate_limits")
	}
	okNeighbor, _, _ := h2.Allow(ctx, goodKey, cat, 3, 600)
	if !okNeighbor {
		t.Fatal("unrelated neighbor IP must stay allowed")
	}

	// Pastikan LoginRateLimiter tetap ramah CGNAT (banyak warga di IP sama)
	loginLim := NewLoginRateLimiter(3, time.Minute, 256)
	cgnatIP := "100.64.12.34"
	for i := 0; i < 3; i++ {
		loginLim.RecordFailure(cgnatIP, "acct-penyerang")
	}
	if okBad, _ := loginLim.CheckAllowed(cgnatIP, "acct-penyerang"); okBad {
		t.Fatal("expected penyerang account locked")
	}
	if okWarga, _ := loginLim.CheckAllowed(cgnatIP, "acct-warga-jujur"); !okWarga {
		t.Fatal("CGNAT violation: warga jujur ikut terkunci pada IP yang sama")
	}
}
