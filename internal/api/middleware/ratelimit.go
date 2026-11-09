package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kopdes/merah-putih/internal/api/response"
)

type clientBucket struct {
	tokens     float64
	lastRefill time.Time
}

type IPRateLimiter struct {
	mu       sync.Mutex
	clients  map[string]*clientBucket
	rate     float64 // tokens per second
	capacity float64 // max burst capacity
}

func NewIPRateLimiter(rps float64, burst int) *IPRateLimiter {
	limiter := &IPRateLimiter{
		clients:  make(map[string]*clientBucket),
		rate:     rps,
		capacity: float64(burst),
	}

	// Rutin pembersihan IP yang tidak aktif setiap 10 menit
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		for range ticker.C {
			limiter.mu.Lock()
			now := time.Now()
			for ip, bucket := range limiter.clients {
				if now.Sub(bucket.lastRefill) > 10*time.Minute {
					delete(limiter.clients, ip)
				}
			}
			limiter.mu.Unlock()
		}
	}()

	return limiter
}

func getClientIP(r *http.Request) string {
	// Jika berada di belakang proxy terpercaya
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (limiter *IPRateLimiter) Allow(ip string) (bool, int) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	now := time.Now()
	bucket, exists := limiter.clients[ip]
	if !exists {
		bucket = &clientBucket{
			tokens:     limiter.capacity - 1,
			lastRefill: now,
		}
		limiter.clients[ip] = bucket
		return true, int(bucket.tokens)
	}

	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens += elapsed * limiter.rate
	if bucket.tokens > limiter.capacity {
		bucket.tokens = limiter.capacity
	}
	bucket.lastRefill = now

	if bucket.tokens >= 1 {
		bucket.tokens -= 1
		return true, int(bucket.tokens)
	}

	return false, 0
}

func RateLimit(limiter *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			allowed, remaining := limiter.Allow(ip)

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", limiter.capacity))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

			if !allowed {
				w.Header().Set("Retry-After", "1")
				response.Error(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "Batas frekuensi permintaan terlampaui. Coba lagi beberapa saat.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
