package api

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/auth"
	"github.com/kopdes/merah-putih/internal/security"
)

type Config struct {
	Port                int
	JWTSecret           string
	BlindIndexSecret    string
	QRISSecret          string
	AESMasterKey        string
	AllowedOrigins      []string
	RateLimitRPS        float64
	RateLimitBurst      int
	TrustedProxies      []string
	MaxRateLimitClients int
}

func decodeSecretBytes(secret string) []byte {
	if s, err := hex.DecodeString(secret); err == nil && len(s) >= 32 {
		return s
	}
	if s, err := base64.StdEncoding.DecodeString(secret); err == nil && len(s) >= 32 {
		return s
	}
	return []byte(secret)
}

// Validate memastikan tidak ada secret default/kosong dan panjang memenuhi standar 32-byte
func (c *Config) Validate() error {
	if err := security.ValidateSecretKey("JWT_SECRET", decodeSecretBytes(c.JWTSecret)); err != nil {
		return err
	}
	if err := security.ValidateSecretKey("BLIND_INDEX_SECRET", decodeSecretBytes(c.BlindIndexSecret)); err != nil {
		return err
	}
	if err := security.ValidateSecretKey("QRIS_SECRET", decodeSecretBytes(c.QRISSecret)); err != nil {
		return err
	}
	if err := security.ValidateSecretKey("AES_MASTER_KEY", decodeSecretBytes(c.AESMasterKey)); err != nil {
		return err
	}
	return nil
}

func DefaultConfig() Config {
	return Config{
		Port:                8080,
		JWTSecret:           "",
		BlindIndexSecret:    "",
		QRISSecret:          "",
		AESMasterKey:        "",
		AllowedOrigins:      []string{"*"},
		RateLimitRPS:        20.0,
		RateLimitBurst:      40,
		TrustedProxies:      nil,
		MaxRateLimitClients: 10000,
	}
}

type Server struct {
	cfg         Config
	pool        *pgxpool.Pool
	keyProvider security.KeyProvider
	httpServer  *http.Server
	router      *http.ServeMux
}

func NewServer(pool *pgxpool.Pool, cfg Config) *Server {
	tm := auth.NewTokenManager(cfg.JWTSecret, "kopdes-merah-putih-api")
	authMw := middleware.NewAuthMiddleware(tm)
	rateLimiter := middleware.NewIPRateLimiterWithConfig(
		cfg.RateLimitRPS,
		cfg.RateLimitBurst,
		cfg.TrustedProxies,
		cfg.MaxRateLimitClients,
	)
	trustedMatcher := middleware.NewTrustedProxyMatcher(cfg.TrustedProxies)
	loginLimiter := middleware.NewLoginRateLimiter(5, 15*time.Minute, 10000)

	var keyProvider security.KeyProvider
	aesBytes := decodeSecretBytes(cfg.AESMasterKey)
	if len(aesBytes) >= 32 {
		kp, err := security.NewMasterKeyProvider("v1", "k1", aesBytes[:32])
		if err == nil {
			keyProvider = kp
		}
	}

	// Inisialisasi Handlers
	authH := handlers.NewAuthHandler(pool, tm, []byte(cfg.BlindIndexSecret), loginLimiter, trustedMatcher)
	qrisH := handlers.NewQRISHandler(pool, []byte(cfg.QRISSecret))
	iuranH := handlers.NewIuranHandler(pool)
	katalogH := handlers.NewKatalogHandler(pool)
	healthH := handlers.NewHealthHandler(pool)

	mux := http.NewServeMux()

	// --- 1. Public Endpoints ---
	mux.HandleFunc("GET /healthz", healthH.HandleHealthz)
	mux.HandleFunc("GET /readyz", healthH.HandleReadyz)
	mux.HandleFunc("POST /api/v1/auth/login", authH.HandleLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", authH.HandleRefreshToken)
	mux.HandleFunc("POST /api/v1/auth/revoke", authH.HandleRevokeToken)

	// --- 2. Authenticated Endpoints (Semua Anggota/User) ---
	mux.Handle("GET /api/v1/auth/me", authMw.RequireAuth(http.HandlerFunc(authH.HandleGetProfile)))
	mux.Handle("POST /api/v1/pembayaran/qris", authMw.RequireAuth(http.HandlerFunc(qrisH.HandleBuatQRIS)))
	mux.Handle("GET /api/v1/pembayaran/{id}", authMw.RequireAuth(http.HandlerFunc(qrisH.HandleGetPembayaranStatus)))
	mux.Handle("POST /api/v1/pembayaran/{id}/bukti", authMw.RequireAuth(http.HandlerFunc(qrisH.HandleKlaimBuktiPembayaran)))
	mux.Handle("GET /api/v1/katalog/stok", authMw.RequireAuth(http.HandlerFunc(katalogH.HandleListStokGudang)))
	mux.Handle("GET /api/v1/katalog/harga", authMw.RequireAuth(http.HandlerFunc(katalogH.HandleGetHargaAnggota)))

	// --- 3. Operator / Pengurus Endpoints (RBAC Guard) ---
	// Pemisahan izin: pengawas & operator hanya read-only antrean/tunggakan, verifikasi & generate dibatasi
	readOnlyAdminRoles := []string{"admin", "pengurus", "operator", "pengawas", "bendahara", "ketua"}
	pembayaranVerifikasiRoles := []string{"admin", "pengurus", "bendahara", "ketua"}
	iuranGenerateRoles := []string{"admin", "pengurus", "bendahara"}

	mux.Handle("GET /api/v1/pembayaran/antrean", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(qrisH.HandleListAntreanVerifikasi)),
	))
	mux.Handle("POST /api/v1/pembayaran/{id}/verifikasi", authMw.RequireAuth(
		middleware.RequireRoles(pembayaranVerifikasiRoles...)(http.HandlerFunc(qrisH.HandleVerifikasiPembayaran)),
	))
	mux.Handle("POST /api/v1/iuran/generate", authMw.RequireAuth(
		middleware.RequireRoles(iuranGenerateRoles...)(http.HandlerFunc(iuranH.HandleGenerateTagihan)),
	))
	mux.Handle("GET /api/v1/iuran/menunggak", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(iuranH.HandleListMenunggak)),
	))

	// --- 4. Admin Koperasi QRIS Management (PERLU VERIFIKASI) ---
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
		cfg:         cfg,
		pool:        pool,
		keyProvider: keyProvider,
		httpServer:  httpSrv,
		router:      mux,
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
