package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestT2_SpoofedXFF_BypassesLimit_Fixed(t *testing.T) {
	// Limiter with RPS=1, Burst=2, NO trusted proxies
	limiter := NewIPRateLimiterWithConfig(1.0, 2, nil, 1000)
	handler := RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	untrustedRemote := "203.0.113.10:45678"

	// Attacker sends 5 requests from the same RemoteAddr, changing X-Forwarded-For each time
	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = untrustedRemote
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if i <= 2 {
			if rec.Code != http.StatusOK {
				t.Fatalf("Request %d expected 200 OK, got %d", i, rec.Code)
			}
		} else {
			// Request 3, 4, 5 MUST be blocked (429 Too Many Requests)
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("FAILED: Request %d should be blocked with 429, got %d", i, rec.Code)
			}
		}
	}
}

func TestT2_TrustedProxy_ValidXFF_Allowed(t *testing.T) {
	// Limiter with trusted proxy 10.0.0.1 and burst=2
	limiter := NewIPRateLimiterWithConfig(1.0, 2, []string{"10.0.0.1", "172.16.0.0/12"}, 1000)
	handler := RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	proxyRemote := "10.0.0.1:54321"

	// 2 requests for Client A via trusted proxy
	for i := 1; i <= 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = proxyRemote
		req.Header.Set("X-Forwarded-For", "198.51.100.5")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Client A request %d should be OK, got %d", i, rec.Code)
		}
	}

	// 3rd request for Client A via trusted proxy should be rate limited (429)
	reqA3 := httptest.NewRequest("GET", "/test", nil)
	reqA3.RemoteAddr = proxyRemote
	reqA3.Header.Set("X-Forwarded-For", "198.51.100.5")
	recA3 := httptest.NewRecorder()
	handler.ServeHTTP(recA3, reqA3)
	if recA3.Code != http.StatusTooManyRequests {
		t.Fatalf("Client A request 3 should be 429, got %d", recA3.Code)
	}

	// But a DIFFERENT client B behind the same trusted proxy still has tokens!
	reqB := httptest.NewRequest("GET", "/test", nil)
	reqB.RemoteAddr = proxyRemote
	reqB.Header.Set("X-Forwarded-For", "198.51.100.99")
	recB := httptest.NewRecorder()
	handler.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("Client B behind trusted proxy should get 200 OK, got %d", recB.Code)
	}
}

func TestT2_BoundedMemory_LRUEviction(t *testing.T) {
	maxCap := 50
	limiter := NewIPRateLimiterWithConfig(1.0, 5, nil, maxCap)

	// Insert 200 unique IPs
	for i := 1; i <= 200; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		limiter.Allow(ip)
	}

	// Memory capacity must not exceed maxCap
	if limiter.Len() > maxCap {
		t.Fatalf("Memory leak: expected capacity <= %d, got %d", maxCap, limiter.Len())
	}
}

func TestT2_LoginRateLimiter_AccountAndIPLockout(t *testing.T) {
	loginLimiter := NewLoginRateLimiter(3, 100*time.Millisecond, 100)

	ip := "192.168.1.100"
	account := "user-email-hash-12345"

	// Initially allowed
	allowed, _ := loginLimiter.CheckAllowed(ip, account)
	if !allowed {
		t.Fatalf("Initial check should be allowed")
	}

	// 2 failures: still allowed
	loginLimiter.RecordFailure(ip, account)
	loginLimiter.RecordFailure(ip, account)
	allowed, _ = loginLimiter.CheckAllowed(ip, account)
	if !allowed {
		t.Fatalf("Check after 2 failures should still be allowed")
	}

	// 3rd failure: reaches threshold -> locked out
	loginLimiter.RecordFailure(ip, account)
	allowed, duration := loginLimiter.CheckAllowed(ip, account)
	if allowed {
		t.Fatalf("Check after 3 failures should be locked out")
	}
	if duration <= 0 {
		t.Fatalf("Expected lockout duration > 0, got %v", duration)
	}

	// Another account from same IP is also locked out
	allowedOtherAcc, _ := loginLimiter.CheckAllowed(ip, "another-account")
	if allowedOtherAcc {
		t.Fatalf("Different account on the same abusive IP should be locked out")
	}

	// Wait for lockout duration to expire
	time.Sleep(120 * time.Millisecond)
	allowedAfterWait, _ := loginLimiter.CheckAllowed(ip, account)
	if !allowedAfterWait {
		t.Fatalf("Check after lockout expired should be allowed")
	}

	// Record success resets account failure count
	loginLimiter.RecordSuccess(account)
}
