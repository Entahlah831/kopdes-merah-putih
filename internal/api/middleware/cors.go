package middleware

import (
	"net/http"
	"strings"
)

type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	AllowCredentials bool
	MaxAgeSeconds    int
}

func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Requested-With", "X-Request-ID", "Idempotency-Key"},
		AllowCredentials: true,
		MaxAgeSeconds:    86400,
	}
}

// CORS menangani preflight dan izin lintas origin untuk aplikasi Front-End
func CORS(cfg CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Validasi origin yang diizinkan
			allowed := false
			isWildcard := false
			for _, o := range cfg.AllowedOrigins {
				if o == "*" {
					allowed = true
					isWildcard = true
					break
				}
				if o == origin {
					allowed = true
					break
				}
			}

			if allowed {
				if isWildcard {
					w.Header().Set("Access-Control-Allow-Origin", "*")
					// W3C CORS Spec: Allow-Credentials dilarang bersamaan dengan wildcard '*'
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					if cfg.AllowCredentials {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
				}
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.AllowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ", "))
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Retry-After, X-RateLimit-Limit, X-RateLimit-Remaining, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			// Tangani request HTTP OPTIONS preflight
			if r.Method == http.MethodOptions {
				if !allowed {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
