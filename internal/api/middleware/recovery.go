package middleware

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/kopdes/merah-putih/internal/api/response"
)

// Recovery menangani panic yang tidak terduga dan mengembalikan respons HTTP 500 terstruktur
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[PANIC RECOVERED] %v\nStack trace:\n%s", rec, string(debug.Stack()))
				response.InternalServerError(w, "Terjadi kesalahan internal pada server.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
