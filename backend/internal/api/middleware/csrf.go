package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/kopdes/merah-putih/internal/api/response"
)

// CSRFGuard melindungi permintaan berbasis cookie yang mengubah state (POST/PUT/PATCH/DELETE).
func CSRFGuard(allowedOrigins []string, trustedProxies []string) func(http.Handler) http.Handler {
	allowedSet := buildAllowedOriginSet(allowedOrigins)
	trusted := NewTrustedProxyMatcher(trustedProxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) || !HasSessionCookie(r) {
				next.ServeHTTP(w, r)
				return
			}
			origin := extractCSRFOrigin(r)
			if origin != "" && !isAllowedCSRFOrigin(origin, allowedSet, r, trusted) {
				response.Error(w, http.StatusForbidden, "CSRF_ORIGIN_REJECTED", "Origin permintaan tidak diizinkan untuk sesi cookie")
				return
			}
			if strings.TrimSpace(r.Header.Get("X-Requested-With")) == "" {
				response.Error(w, http.StatusForbidden, "CSRF_HEADER_REQUIRED", "Header X-Requested-With wajib diisi untuk sesi cookie")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func buildAllowedOriginSet(origins []string) map[string]bool {
	out := make(map[string]bool, len(origins))
	for _, o := range origins {
		norm := strings.TrimRight(strings.TrimSpace(o), "/")
		if norm != "" && norm != "*" {
			out[norm] = true
		}
	}
	return out
}

func extractCSRFOrigin(r *http.Request) string {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return strings.TrimRight(origin, "/")
	}
	if ref := strings.TrimSpace(r.Header.Get("Referer")); ref != "" {
		if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	return ""
}

func isAllowedCSRFOrigin(origin string, allowedSet map[string]bool, r *http.Request, trusted *TrustedProxyMatcher) bool {
	if origin == "null" {
		return false
	}
	if allowedSet[origin] {
		return true
	}
	return sameAsServerOrigin(origin, r, trusted)
}

func sameAsServerOrigin(origin string, r *http.Request, trusted *TrustedProxyMatcher) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || r.Host == "" {
		return false
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return false
	}
	return strings.EqualFold(u.Scheme, RequestScheme(r, trusted))
}

// RequestScheme menentukan http/https dengan hanya mempercayai X-Forwarded-Proto dari TrustedProxies.
func RequestScheme(r *http.Request, trusted *TrustedProxyMatcher) string {
	if r.TLS != nil {
		return "https"
	}
	if trusted != nil && IsRemoteTrustedProxy(r, trusted) {
		if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
			parts := strings.Split(proto, ",")
			if p := strings.ToLower(strings.TrimSpace(parts[0])); p == "https" || p == "http" {
				return p
			}
		}
	}
	return "http"
}

// IsRemoteTrustedProxy memeriksa apakah RemoteAddr koneksi TCP langsung berasal dari proxy terpercaya.
func IsRemoteTrustedProxy(r *http.Request, trusted *TrustedProxyMatcher) bool {
	if trusted == nil {
		return false
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		// Tangani IPv4 host:port atau [::1]:port
		if !strings.Contains(host[idx+1:], "]") {
			host = host[:idx]
		}
	}
	host = strings.Trim(host, "[]")
	return trusted.IsTrusted(host)
}
