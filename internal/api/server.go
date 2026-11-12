package api

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/auth"
	"github.com/kopdes/merah-putih/internal/security"
)

type Config struct {
	Port                      int
	JWTSecret                 string
	BlindIndexSecret          string
	QRISSecret                string
	AESMasterKey              string
	AllowedOrigins            []string
	RateLimitRPS              float64
	RateLimitBurst            int
	TrustedProxies            []string
	MaxRateLimitClients       int
	QRISDynamicEnabled        bool
	AllowPersonalPaymentPilot bool
	MetricsToken              string
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
	if err := security.ValidateSecretKey("METRICS_TOKEN", decodeSecretBytes(c.MetricsToken)); err != nil {
		return err
	}
	return nil
}

// ValidateProductionDatabasePassword memvalidasi kepatuhan kata sandi database pada profil produksi (Tugas 8)
func ValidateProductionDatabasePassword(appEnv, dbPassword, kopdesAppPassword string) error {
	if strings.TrimSpace(appEnv) != "production" {
		return nil
	}
	pass := strings.TrimSpace(kopdesAppPassword)
	if pass == "" {
		pass = strings.TrimSpace(dbPassword)
	}
	if pass == "" {
		return errors.New("pada profil produksi, variabel lingkungan KOPDES_APP_PASSWORD atau DB_PASSWORD wajib disetel")
	}
	if pass == "kopdes_dev_password" {
		return errors.New("pada profil produksi, dilarang menggunakan password default dev (kopdes_dev_password)")
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
		TrustedProxies:            nil,
		MaxRateLimitClients:       10000,
		QRISDynamicEnabled:        false,
		AllowPersonalPaymentPilot: false,
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
	authH := handlers.NewAuthHandler(pool, tm, []byte(cfg.BlindIndexSecret), loginLimiter, trustedMatcher, keyProvider)
	qrisH := handlers.NewQRISHandlerWithConfig(pool, []byte(cfg.QRISSecret), cfg.QRISDynamicEnabled, cfg.AllowPersonalPaymentPilot)
	iuranH := handlers.NewIuranHandler(pool)
	katalogH := handlers.NewKatalogHandler(pool)
	healthH := handlers.NewHealthHandler(pool)
	metricsH := handlers.NewMetricsHandler(pool, cfg.MetricsToken, trustedMatcher)
	pendaftaranH := handlers.NewPendaftaranHandler(pool, keyProvider, []byte(cfg.BlindIndexSecret))
	kasH := handlers.NewKasHandler(pool)
	anggotaH := handlers.NewAnggotaHandler(pool)
	tokoH := handlers.NewTokoHandler(pool)
	laporanH := handlers.NewLaporanHandler(pool)
	userMgmtH := handlers.NewUserManagementHandler(pool)

	mux := http.NewServeMux()

	// --- 1. Public Endpoints ---
	mux.HandleFunc("GET /healthz", healthH.HandleHealthz)
	mux.HandleFunc("GET /readyz", healthH.HandleReadyz)
	mux.HandleFunc("GET /metrics", metricsH.HandleMetrics)
	mux.HandleFunc("POST /api/v1/auth/login", authH.HandleLogin)
	mux.HandleFunc("POST /api/v1/auth/mfa/verify", authH.HandleMFAVerify)
	mux.HandleFunc("POST /api/v1/auth/refresh", authH.HandleRefreshToken)
	mux.HandleFunc("POST /api/v1/auth/revoke", authH.HandleRevokeToken)

	// --- 2. Authenticated Endpoints (Semua Anggota/User) ---
	mux.Handle("GET /api/v1/auth/me", authMw.RequireAuth(http.HandlerFunc(authH.HandleGetProfile)))
	mux.Handle("POST /api/v1/auth/mfa/setup", authMw.RequireAuth(http.HandlerFunc(authH.HandleMFASetup)))
	mux.Handle("POST /api/v1/auth/mfa/enable", authMw.RequireAuth(http.HandlerFunc(authH.HandleMFAEnable)))
	mux.Handle("POST /api/v1/pembayaran/qris", authMw.RequireAuth(http.HandlerFunc(qrisH.HandleBuatQRIS)))
	mux.Handle("GET /api/v1/pembayaran/{id}", authMw.RequireAuth(http.HandlerFunc(qrisH.HandleGetPembayaranStatus)))
	mux.Handle("POST /api/v1/pembayaran/{id}/bukti", authMw.RequireAuth(http.HandlerFunc(qrisH.HandleKlaimBuktiPembayaran)))
	mux.Handle("GET /api/v1/katalog/stok", authMw.RequireAuth(http.HandlerFunc(katalogH.HandleListStokGudang)))
	mux.Handle("GET /api/v1/katalog/harga", authMw.RequireAuth(http.HandlerFunc(katalogH.HandleGetHargaAnggota)))
	mux.Handle("GET /api/v1/katalog/produk", authMw.RequireAuth(http.HandlerFunc(katalogH.HandleListProduk)))
	mux.Handle("GET /api/v1/katalog/promo", authMw.RequireAuth(http.HandlerFunc(katalogH.HandleListPromo)))
	mux.Handle("GET /api/v1/iuran/jenis", authMw.RequireAuth(http.HandlerFunc(iuranH.HandleListJenisIuran)))
	mux.Handle("GET /api/v1/iuran/tagihan/saya", authMw.RequireAuth(http.HandlerFunc(iuranH.HandleGetTagihanSaya)))
	mux.Handle("GET /api/v1/toko/keranjang", authMw.RequireAuth(http.HandlerFunc(tokoH.HandleGetKeranjang)))
	mux.Handle("POST /api/v1/toko/keranjang", authMw.RequireAuth(http.HandlerFunc(tokoH.HandleAddToCart)))
	mux.Handle("DELETE /api/v1/toko/keranjang/{produk_id}", authMw.RequireAuth(http.HandlerFunc(tokoH.HandleRemoveFromCart)))
	mux.Handle("POST /api/v1/toko/checkout", authMw.RequireAuth(http.HandlerFunc(tokoH.HandleCheckout)))
	mux.Handle("GET /api/v1/toko/pesanan", authMw.RequireAuth(http.HandlerFunc(tokoH.HandleListPesanan)))
	mux.Handle("POST /api/v1/toko/pesanan/{id}/batal", authMw.RequireAuth(http.HandlerFunc(tokoH.HandleBatalPesanan)))
	mux.Handle("POST /api/v1/toko/pesanan/{id}/expire", authMw.RequireAuth(http.HandlerFunc(tokoH.HandleExpirePesanan)))

	// Pendaftaran & KYC Calon Anggota (P1-1)
	mux.Handle("POST /api/v1/pendaftaran", authMw.RequireAuth(http.HandlerFunc(pendaftaranH.HandleAjukanPendaftaran)))
	mux.Handle("GET /api/v1/pendaftaran/{id}", authMw.RequireAuth(http.HandlerFunc(pendaftaranH.HandleGetPendaftaran)))
	mux.Handle("POST /api/v1/pendaftaran/{id}/dokumen", authMw.RequireAuth(http.HandlerFunc(pendaftaranH.HandleUploadDokumenPendaftaran)))

	// --- 3. Operator / Pengurus Endpoints (RBAC Guard) ---
	readOnlyAdminRoles := []string{"admin", "pengurus", "operator", "pengawas", "bendahara", "ketua"}
	pembayaranVerifikasiRoles := []string{"admin", "pengurus", "bendahara", "ketua"}
	iuranGenerateRoles := []string{"admin", "pengurus", "bendahara"}
	pendaftaranVerifikasiRoles := []string{"admin", "pengurus", "ketua", "sekretaris"}

	// Manajemen Anggota (Task 9)
	mux.Handle("GET /api/v1/anggota", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(anggotaH.HandleListAnggota)),
	))
	mux.Handle("GET /api/v1/anggota/{id}", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(anggotaH.HandleGetAnggota)),
	))
	mux.Handle("PATCH /api/v1/anggota/{id}/status", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus", "ketua")(http.HandlerFunc(anggotaH.HandleUpdateStatusAnggota)),
	))

	// Manajemen Pengguna (Task 9)
	mux.Handle("GET /api/v1/users", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus", "ketua")(http.HandlerFunc(userMgmtH.HandleListUsers)),
	))
	mux.Handle("GET /api/v1/users/{id}", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus", "ketua")(http.HandlerFunc(userMgmtH.HandleGetUserDetail)),
	))
	mux.Handle("PATCH /api/v1/users/{id}/role", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus", "ketua")(http.HandlerFunc(userMgmtH.HandleUpdateUserRole)),
	))

	// CRUD Iuran (Task 9)
	mux.Handle("POST /api/v1/iuran/jenis", authMw.RequireAuth(
		middleware.RequireRoles(iuranGenerateRoles...)(http.HandlerFunc(iuranH.HandleCreateJenisIuran)),
	))

	// CRUD Katalog & Promo (Task 9)
	mux.Handle("POST /api/v1/katalog/produk", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(katalogH.HandleCreateProduk)),
	))
	mux.Handle("PUT /api/v1/katalog/produk/{id}", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(katalogH.HandleUpdateProduk)),
	))
	mux.Handle("DELETE /api/v1/katalog/produk/{id}", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(katalogH.HandleDeleteProduk)),
	))
	mux.Handle("POST /api/v1/katalog/promo", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(katalogH.HandleCreatePromo)),
	))

	// Laporan (Task 9)
	mux.Handle("GET /api/v1/laporan/keuangan", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(laporanH.HandleLaporanKeuangan)),
	))
	mux.Handle("GET /api/v1/laporan/anggota", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(laporanH.HandleLaporanRekapAnggota)),
	))

	mux.Handle("POST /api/v1/pendaftaran/{id}/verifikasi", authMw.RequireAuth(
		middleware.RequireRoles(pendaftaranVerifikasiRoles...)(http.HandlerFunc(pendaftaranH.HandleVerifikasiPendaftaran)),
	))
	mux.Handle("POST /api/v1/pendaftaran/purge-ditolak", authMw.RequireAuth(
		middleware.RequireRoles(pendaftaranVerifikasiRoles...)(http.HandlerFunc(pendaftaranH.HandlePurgeRejectedPendaftaran)),
	))
	mux.Handle("GET /api/v1/pembayaran/antrean", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(qrisH.HandleListAntreanVerifikasi)),
	))
	mux.Handle("POST /api/v1/pembayaran/{id}/verifikasi", authMw.RequireAuth(
		middleware.RequireRoles(pembayaranVerifikasiRoles...)(http.HandlerFunc(qrisH.HandleVerifikasiPembayaran)),
	))
	mux.Handle("POST /api/v1/pembayaran/mutasi-bank/impor", authMw.RequireAuth(
		middleware.RequireRoles(pembayaranVerifikasiRoles...)(http.HandlerFunc(qrisH.HandleImporMutasiBank)),
	))
	mux.Handle("GET /api/v1/pembayaran/rekonsiliasi/harian", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(qrisH.HandleLaporanRekonsiliasiHarian)),
	))
	mux.Handle("POST /api/v1/iuran/generate", authMw.RequireAuth(
		middleware.RequireRoles(iuranGenerateRoles...)(http.HandlerFunc(iuranH.HandleGenerateTagihan)),
	))
	mux.Handle("GET /api/v1/iuran/menunggak", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(iuranH.HandleListMenunggak)),
	))
	mux.Handle("GET /api/v1/kas/mutasi", authMw.RequireAuth(
		middleware.RequireRoles(readOnlyAdminRoles...)(http.HandlerFunc(kasH.HandleListKasMutasi)),
	))

	// --- 4. Admin Koperasi QRIS Management ---
	mux.Handle("POST /api/v1/koperasi/rekening-qris", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(qrisH.HandleAdminDaftarRekeningQRIS)),
	))
	mux.Handle("GET /api/v1/koperasi/rekening-qris", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(qrisH.HandleAdminListRekeningQRIS)),
	))
	mux.Handle("DELETE /api/v1/koperasi/rekening-qris/{id}", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus")(http.HandlerFunc(qrisH.HandleAdminDeactivateRekeningQRIS)),
	))

	// Middleware Pipeline Global (OWASP Headers -> CORS -> Recovery -> RateLimit -> BodyLimit)
	corsCfg := middleware.DefaultCORSConfig()
	if len(cfg.AllowedOrigins) > 0 {
		corsCfg.AllowedOrigins = cfg.AllowedOrigins
	}

	var handler http.Handler = mux
	handler = middleware.BodyLimit(10 * 1024 * 1024)(handler) // Batasi payload maksimal 10 MB (S3)
	handler = middleware.RateLimit(rateLimiter)(handler)
	handler = middleware.CORS(corsCfg)(handler)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Recovery(handler)

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,  // Anti-Slowloris attack (S3)
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
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
	if err := s.cfg.Validate(); err != nil {
		return fmt.Errorf("gagal memulai server: konfigurasi keamanan tidak valid: %w", err)
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown mematikan server secara graceful
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
