package middleware

import (
	"container/list"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kopdes/merah-putih/internal/api/response"
)

// TrustedProxyMatcher memvalidasi apakah alamat IP asal (RemoteAddr) termasuk dalam proxy terpercaya
type TrustedProxyMatcher struct {
	exactIPs map[string]bool
	cidrs    []*net.IPNet
}

func NewTrustedProxyMatcher(proxies []string) *TrustedProxyMatcher {
	m := &TrustedProxyMatcher{
		exactIPs: make(map[string]bool),
	}
	for _, p := range proxies {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "/") {
			_, ipNet, err := net.ParseCIDR(p)
			if err == nil {
				m.cidrs = append(m.cidrs, ipNet)
			}
		} else {
			if ip := net.ParseIP(p); ip != nil {
				m.exactIPs[ip.String()] = true
			}
		}
	}
	return m
}

func (m *TrustedProxyMatcher) IsTrusted(ipStr string) bool {
	if m == nil {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	if m.exactIPs[ip.String()] {
		return true
	}
	for _, cidr := range m.cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// GetClientIP mengekstrak IP klien dengan validasi ketat anti-spoofing
// Header X-Forwarded-For dan X-Real-IP HANYA dipercayai jika RemoteAddr berasal dari TrustedProxies
func GetClientIP(r *http.Request, trusted *TrustedProxyMatcher) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	host = strings.TrimSpace(host)

	if trusted != nil && trusted.IsTrusted(host) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			client := strings.TrimSpace(parts[0])
			if net.ParseIP(client) != nil {
				return client
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			client := strings.TrimSpace(xri)
			if net.ParseIP(client) != nil {
				return client
			}
		}
	}

	return host
}

type clientEntry struct {
	key        string
	tokens     float64
	lastRefill time.Time
}

// IPRateLimiter menerapkan Token Bucket dengan Bounded Memory LRU Cache
type IPRateLimiter struct {
	mu             sync.Mutex
	clients        map[string]*list.Element
	lruList        *list.List
	rate           float64 // tokens per detik
	capacity       float64 // max burst
	maxClients     int     // batas maksimal memori map LRU
	trustedProxies *TrustedProxyMatcher
}

func NewIPRateLimiterWithConfig(rps float64, burst int, trustedProxies []string, maxClients int) *IPRateLimiter {
	if maxClients <= 0 {
		maxClients = 10000
	}
	limiter := &IPRateLimiter{
		clients:        make(map[string]*list.Element),
		lruList:        list.New(),
		rate:           rps,
		capacity:       float64(burst),
		maxClients:     maxClients,
		trustedProxies: NewTrustedProxyMatcher(trustedProxies),
	}

	// Rutin pembersihan IP tidak aktif setiap 10 menit
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		for range ticker.C {
			limiter.mu.Lock()
			now := time.Now()
			for elem := limiter.lruList.Back(); elem != nil; {
				prev := elem.Prev()
				entry := elem.Value.(*clientEntry)
				if now.Sub(entry.lastRefill) > 10*time.Minute {
					delete(limiter.clients, entry.key)
					limiter.lruList.Remove(elem)
				}
				elem = prev
			}
			limiter.mu.Unlock()
		}
	}()

	return limiter
}

func NewIPRateLimiter(rps float64, burst int) *IPRateLimiter {
	return NewIPRateLimiterWithConfig(rps, burst, nil, 10000)
}

func (limiter *IPRateLimiter) Allow(ip string) (bool, int) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	now := time.Now()
	elem, exists := limiter.clients[ip]
	if !exists {
		// Evict LRU jika sudah mencapai batas kapasitas
		if limiter.lruList.Len() >= limiter.maxClients {
			oldest := limiter.lruList.Back()
			if oldest != nil {
				oldEntry := oldest.Value.(*clientEntry)
				delete(limiter.clients, oldEntry.key)
				limiter.lruList.Remove(oldest)
			}
		}

		entry := &clientEntry{
			key:        ip,
			tokens:     limiter.capacity - 1,
			lastRefill: now,
		}
		limiter.clients[ip] = limiter.lruList.PushFront(entry)
		return true, int(entry.tokens)
	}

	limiter.lruList.MoveToFront(elem)
	entry := elem.Value.(*clientEntry)
	elapsed := now.Sub(entry.lastRefill).Seconds()
	entry.tokens += elapsed * limiter.rate
	if entry.tokens > limiter.capacity {
		entry.tokens = limiter.capacity
	}
	entry.lastRefill = now

	if entry.tokens >= 1 {
		entry.tokens -= 1
		return true, int(entry.tokens)
	}

	return false, 0
}

func (limiter *IPRateLimiter) Len() int {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return limiter.lruList.Len()
}

func RateLimit(limiter *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := GetClientIP(r, limiter.trustedProxies)
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

// ------------------------------------------------------------------------
// Login Rate Limiter (Per Akun & Per IP dengan Backoff / Lockout)
// ------------------------------------------------------------------------

type loginAttemptEntry struct {
	key         string
	failures    int
	lockedUntil time.Time
}

type LoginRateLimiter struct {
	mu              sync.Mutex
	attempts        map[string]*list.Element
	lruList         *list.List
	maxCapacity     int
	maxFailures     int
	lockoutDuration time.Duration
}

func NewLoginRateLimiter(maxFailures int, lockoutDuration time.Duration, maxCapacity int) *LoginRateLimiter {
	if maxCapacity <= 0 {
		maxCapacity = 10000
	}
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if lockoutDuration <= 0 {
		lockoutDuration = 15 * time.Minute
	}
	return &LoginRateLimiter{
		attempts:        make(map[string]*list.Element),
		lruList:         list.New(),
		maxCapacity:     maxCapacity,
		maxFailures:     maxFailures,
		lockoutDuration: lockoutDuration,
	}
}

func (l *LoginRateLimiter) getEntry(key string) *loginAttemptEntry {
	if elem, ok := l.attempts[key]; ok {
		l.lruList.MoveToFront(elem)
		return elem.Value.(*loginAttemptEntry)
	}
	if l.lruList.Len() >= l.maxCapacity {
		oldest := l.lruList.Back()
		if oldest != nil {
			delete(l.attempts, oldest.Value.(*loginAttemptEntry).key)
			l.lruList.Remove(oldest)
		}
	}
	entry := &loginAttemptEntry{key: key}
	l.attempts[key] = l.lruList.PushFront(entry)
	return entry
}

// CheckAllowed memeriksa apakah IP atau akun sedang terkunci karena gagal login berulang kali
func (l *LoginRateLimiter) CheckAllowed(ip, accountKey string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for _, k := range []string{"ip:" + ip, "acc:" + accountKey} {
		if elem, ok := l.attempts[k]; ok {
			entry := elem.Value.(*loginAttemptEntry)
			if now.Before(entry.lockedUntil) {
				return false, entry.lockedUntil.Sub(now)
			}
		}
	}
	return true, 0
}

// RecordFailure mencatat percobaan login gagal dan mengunci akun/IP jika melampaui batas
func (l *LoginRateLimiter) RecordFailure(ip, accountKey string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for _, k := range []string{"ip:" + ip, "acc:" + accountKey} {
		entry := l.getEntry(k)
		entry.failures++
		if entry.failures >= l.maxFailures {
			entry.lockedUntil = now.Add(l.lockoutDuration)
		}
	}
}

// RecordSuccess mereset penghitung kegagalan akun setelah berhasil login
func (l *LoginRateLimiter) RecordSuccess(accountKey string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	k := "acc:" + accountKey
	if elem, ok := l.attempts[k]; ok {
		delete(l.attempts, k)
		l.lruList.Remove(elem)
	}
}
