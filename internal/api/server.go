package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/auth"
)

type Config struct {
	Port             int
	JWTSecret        string
	BlindIndexSecret string
	QRISSecret       string
	AllowedOrigins   []string
	RateLimitRPS     float64
	RateLimitBurst   int
}

func DefaultConfig() Config {
	return Config{
		Port:             8080,
		JWTSecret:        "dev-super-secret-jwt-key-minimum-32bytes-long!",
		BlindIndexSecret: "dev-blind-index-hmac-sha256-secret-32b!",
		QRISSecret:       "dev-qris-linear-probing-secret-32b-key!",
		AllowedOrigins:   []string{"*"},
		RateLimitRPS:     20.0,
		RateLimitBurst:   40,
	}
}

type Server struct {
	cfg        Config
	pool       *pgxpool.Pool
	httpServer *http.Server
	router     *http.ServeMux
}

func NewServer(pool *pgxpool.Pool, cfg Config) *Server {
	tm := auth.NewTokenManager(cfg.JWTSecret, "kopdes-merah-putih-api")
	authMw := middleware.NewAuthMiddleware(tm)
	rateLimiter := middleware.NewIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)

	// Inisialisasi Handlers
	authH := handlers.NewAuthHandler(pool, tm, []byte(cfg.BlindIndexSecret))
	qrisH := handlers.NewQRISHandler(pool, []byte(cfg.QRISSecret))
	iuranH := handlers.NewIuranHandler(pool)
	katalogH := handlers.NewKatalogHandler(pool)
	healthH := handlers.NewHealthHandler(pool)

	mux := http.NewServeMux()

	// --- 1. Public Endpoints ---
	mux.HandleFunc("GET /healthz", healthH.HandleHealthz)
	mux.HandleFunc("GET /readyz", healthH.HandleReadyz)
	mux.HandleFunc("POST /api/v1/auth/login", authH.HandleLogin)

	// --- 2. Authenticated Endpoints (Semua Anggota/User) ---
	mux.Handle("GET /api/v1/auth/me", authMw.RequireAuth(http.HandlerFunc(authH.HandleGetProfile)))
	mux.Handle("POST /api/v1/pembayaran/qris", authMw.RequireAuth(http.HandlerFunc(qrisH.HandleBuatQRIS)))
	mux.Handle("GET /api/v1/pembayaran/{id}", authMw.RequireAuth(http.HandlerFunc(qrisH.HandleGetPembayaranStatus)))
	mux.Handle("POST /api/v1/pembayaran/{id}/bukti", authMw.RequireAuth(http.HandlerFunc(qrisH.HandleKlaimBuktiPembayaran)))
	mux.Handle("GET /api/v1/katalog/stok", authMw.RequireAuth(http.HandlerFunc(katalogH.HandleListStokGudang)))
	mux.Handle("GET /api/v1/katalog/harga", authMw.RequireAuth(http.HandlerFunc(katalogH.HandleGetHargaAnggota)))

	// --- 3. Operator / Pengurus Endpoints (RBAC Guard) ---
	adminRoles := []string{"admin", "pengurus", "operator", "pengawas"}
	mux.Handle("GET /api/v1/pembayaran/antrean", authMw.RequireAuth(
		middleware.RequireRoles(adminRoles...)(http.HandlerFunc(qrisH.HandleListAntreanVerifikasi)),
	))
	mux.Handle("POST /api/v1/pembayaran/{id}/verifikasi", authMw.RequireAuth(
		middleware.RequireRoles(adminRoles...)(http.HandlerFunc(qrisH.HandleVerifikasiPembayaran)),
	))
	mux.Handle("POST /api/v1/iuran/generate", authMw.RequireAuth(
		middleware.RequireRoles(adminRoles...)(http.HandlerFunc(iuranH.HandleGenerateTagihan)),
	))
	mux.Handle("GET /api/v1/iuran/menunggak", authMw.RequireAuth(
		middleware.RequireRoles(adminRoles...)(http.HandlerFunc(iuranH.HandleListMenunggak)),
	))

	// --- 4. Admin Koperasi QRIS Management (PADG BI 21/18/PADG/2019) ---
	mux.Handle("POST /api/v1/koperasi/rekening-qris", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(qrisH.HandleAdminDaftarRekeningQRIS)),
	))
	mux.Handle("GET /api/v1/koperasi/rekening-qris", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(qrisH.HandleAdminListRekeningQRIS)),
	))
	mux.Handle("DELETE /api/v1/koperasi/rekening-qris/{id}", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(qrisH.HandleAdminDeactivateRekeningQRIS)),
	))

	// Middleware Pipeline Global (OWASP Headers -> CORS -> Recovery -> RateLimit)
	corsCfg := middleware.DefaultCORSConfig()
	if len(cfg.AllowedOrigins) > 0 {
		corsCfg.AllowedOrigins = cfg.AllowedOrigins
	}

	var handler http.Handler = mux
	handler = middleware.RateLimit(rateLimiter)(handler)
	handler = middleware.CORS(corsCfg)(handler)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Recovery(handler)

	httpSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &Server{
		cfg:        cfg,
		pool:       pool,
		httpServer: httpSrv,
		router:     mux,
	}
}

// Router mengembalikan instance mux untuk kebutuhan testing langsung via httptest
func (s *Server) Router() http.Handler {
	return s.httpServer.Handler
}

// Start menjalankan HTTP server
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown mematikan server secara graceful
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
