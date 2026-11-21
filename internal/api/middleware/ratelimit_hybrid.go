package middleware

import (
	"container/list"
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/db"
)

type hybridBucket struct {
	key          string
	count        int32
	windowReset  time.Time
	blockedUntil time.Time
}

// HybridRateLimiter menggabungkan penghitung LRU di memori (L1: 0 tulis DB untuk trafik normal/banjir IP palsu)
// dan persistensi ke tabel rate_limits (L2) hanya ketika pelanggar melewati ambang batas.
type HybridRateLimiter struct {
	mu         sync.Mutex
	pool       *pgxpool.Pool
	trusted    *TrustedProxyMatcher
	buckets    map[string]*list.Element
	lruList    *list.List
	maxEntries int
}

// NewHybridRateLimiter membentuk limiter hybrid sekaligus memuat daftar blokir aktif dari DB bila tersedia.
func NewHybridRateLimiter(pool *pgxpool.Pool, trustedProxies []string, maxEntries int) *HybridRateLimiter {
	if maxEntries <= 0 {
		maxEntries = 4096
	}
	h := &HybridRateLimiter{
		pool:       pool,
		trusted:    NewTrustedProxyMatcher(trustedProxies),
		buckets:    make(map[string]*list.Element),
		lruList:    list.New(),
		maxEntries: maxEntries,
	}
	h.warmUpActiveBlocks()
	return h
}

func (h *HybridRateLimiter) ClientIP(r *http.Request) string {
	return GetClientIP(r, h.trusted)
}

func (h *HybridRateLimiter) warmUpActiveBlocks() {
	if h.pool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := db.New(h.pool).ListActiveRateLimitBlocks(ctx)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range rows {
		if !r.BlockedUntil.Valid {
			continue
		}
		entry := h.getOrCreateEntryLocked(r.Key, time.Now())
		entry.blockedUntil = r.BlockedUntil.Time
	}
}

func (h *HybridRateLimiter) getOrCreateEntryLocked(key string, now time.Time) *hybridBucket {
	if elem, ok := h.buckets[key]; ok {
		h.lruList.MoveToFront(elem)
		return elem.Value.(*hybridBucket)
	}
	if h.lruList.Len() >= h.maxEntries {
		if oldest := h.lruList.Back(); oldest != nil {
			old := oldest.Value.(*hybridBucket)
			delete(h.buckets, old.key)
			h.lruList.Remove(oldest)
		}
	}
	entry := &hybridBucket{key: key, windowReset: now}
	h.buckets[key] = h.lruList.PushFront(entry)
	return entry
}

// Allow memeriksa batas laju di memori dan hanya menulis ke PostgreSQL jika melewati ambang batas.
func (h *HybridRateLimiter) Allow(ctx context.Context, key, kategori string, maxHits, windowSecs int32) (bool, int32, error) {
	if h == nil {
		return true, maxHits, nil
	}
	if windowSecs <= 0 {
		windowSecs = 60
	}
	now := time.Now()
	allowed, remaining, shouldPersist := h.evaluateInMemory(key, maxHits, windowSecs, now)
	if !allowed {
		if shouldPersist {
			h.persistBlock(ctx, key, kategori, windowSecs)
		}
		return false, 0, nil
	}
	return true, remaining, nil
}

func (h *HybridRateLimiter) evaluateInMemory(key string, maxHits, windowSecs int32, now time.Time) (bool, int32, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := h.getOrCreateEntryLocked(key, now)
	if now.Before(entry.blockedUntil) {
		return false, 0, false
	}
	if now.After(entry.windowReset) || entry.windowReset.Equal(now) {
		entry.count = 0
		entry.windowReset = now.Add(time.Duration(windowSecs) * time.Second)
	}
	entry.count++
	if entry.count > maxHits {
		entry.blockedUntil = now.Add(time.Duration(windowSecs) * time.Second)
		return false, 0, true
	}
	return true, maxHits - entry.count, false
}

func (h *HybridRateLimiter) persistBlock(ctx context.Context, key, kategori string, blockSecs int32) {
	if h.pool == nil {
		return
	}
	_ = db.New(h.pool).SetRateLimitBlocked(ctx, db.SetRateLimitBlockedParams{
		Key:       key,
		Kategori:  kategori,
		BlockSecs: blockSecs,
	})
}
