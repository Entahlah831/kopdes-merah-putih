package middleware

import (
	"net/http"
)

// BodyLimit membatasi ukuran payload body permintaan HTTP untuk mencegah Denial of Service (DoS).
// Mengembalikan 413 Payload Too Large jika Content-Length melebihi batas dan membungkus r.Body
// dengan http.MaxBytesReader untuk permintaan chunked/streaming.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if maxBytes > 0 && r.ContentLength > maxBytes {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				_, _ = w.Write([]byte(`{"success":false,"error":{"code":"PAYLOAD_TOO_LARGE","message":"Ukuran payload permintaan melebihi batas maksimal yang diizinkan"}}`))
				return
			}
			if r.Body != nil && maxBytes > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
