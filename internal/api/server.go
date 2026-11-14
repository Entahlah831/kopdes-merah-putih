package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/auth"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
)

type Config struct {
	AppEnv                       string
	AppTimezone                  string
	Port                         int
	MetricsPort                  int
	JWTSecret                    string
	JWTPreviousSecret            string
	JWTAlgorithm                 string
	JWTIssuer                    string
	JWTAccessTTL                 time.Duration
	JWTRefreshTTL                time.Duration
	BlindIndexSecret             string
	QRISSecret                   string
	AESMasterKey                 string
	AESKeyVersion                string
	AESKeyID                     string
	AESPreviousKeys              string
	AllowedOrigins               []string
	RateLimitRPS                 float64
	RateLimitBurst               int
	TrustedProxies               []string
	MaxRateLimitClients          int
	MaxBodyBytes                 int64
	HTTPReadHeaderTimeout        time.Duration
	HTTPReadTimeout              time.Duration
	HTTPWriteTimeout             time.Duration
	HTTPIdleTimeout              time.Duration
	BcryptCost                   int
	MFAIssuer                    string
	MFAMaxAttempts               int
	MFASessionTTL                time.Duration
	MFASetupTTL                  time.Duration
	LoginMaxAttempts             int
	LoginLockoutDuration         time.Duration
	QRISDynamicEnabled           bool
	AllowPersonalPaymentPilot    bool
	PilotDefaultMaxPerTx         int64
	PilotDefaultMaxPerDay        int64
	DualApprovalDefaultThreshold int64
	StorageKYCDir                string
	MaxKYCUploadBytes            int64
	MetricsToken                 string
	MetricsPreviousToken         string
}

func decodeSecretBytes(secret string) []byte {
	return security.DecodeSecretMaterial(secret)
}

// ConfigFromAppConfig mengonversi config.AppConfig menjadi api.Config secara deterministik.
func ConfigFromAppConfig(c config.AppConfig) Config {
	return Config{
		AppEnv:                       c.AppEnv,
		AppTimezone:                  c.AppTimezone,
		Port:                         c.APIPort,
		MetricsPort:                  c.MetricsPort,
		JWTSecret:                    c.JWTSecret,
		JWTPreviousSecret:            c.JWTPreviousSecret,
		JWTAlgorithm:                 c.JWTAlgorithm,
		JWTIssuer:                    c.JWTIssuer,
		JWTAccessTTL:                 c.JWTAccessTTL,
		JWTRefreshTTL:                c.JWTRefreshTTL,
		BlindIndexSecret:             c.BlindIndexSecret,
		QRISSecret:                   c.QRISSecret,
		AESMasterKey:                 c.AESMasterKey,
		AESKeyVersion:                c.AESKeyVersion,
		AESKeyID:                     c.AESKeyID,
		AESPreviousKeys:              c.AESPreviousKeys,
		AllowedOrigins:               c.AllowedOrigins,
		RateLimitRPS:                 c.RateLimitRPS,
		RateLimitBurst:               c.RateLimitBurst,
		TrustedProxies:               c.TrustedProxies,
		MaxRateLimitClients:          c.RateLimitMaxClients,
		MaxBodyBytes:                 c.MaxBodyBytes,
		HTTPReadHeaderTimeout:        c.HTTPReadHeaderTimeout,
		HTTPReadTimeout:              c.HTTPReadTimeout,
		HTTPWriteTimeout:             c.HTTPWriteTimeout,
		HTTPIdleTimeout:              c.HTTPIdleTimeout,
		BcryptCost:                   c.BcryptCost,
		MFAIssuer:                    c.MFAIssuer,
		MFAMaxAttempts:               c.MFAMaxAttempts,
		MFASessionTTL:                c.MFASessionTTL,
		MFASetupTTL:                  c.MFASetupTTL,
		LoginMaxAttempts:             c.LoginMaxAttempts,
		LoginLockoutDuration:         c.LoginLockoutDuration,
		QRISDynamicEnabled:           c.QRISDynamicEnabled,
		AllowPersonalPaymentPilot:    c.AllowPersonalPaymentPilot,
		PilotDefaultMaxPerTx:         c.PilotDefaultMaxPerTx,
		PilotDefaultMaxPerDay:        c.PilotDefaultMaxPerDay,
		DualApprovalDefaultThreshold: c.DualApprovalDefaultThreshold,
		StorageKYCDir:                c.StorageKYCDir,
		MaxKYCUploadBytes:            c.MaxKYCUploadBytes,
		MetricsToken:                 c.MetricsToken,
		MetricsPreviousToken:         c.MetricsPreviousToken,
	}
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
	if strings.EqualFold(strings.TrimSpace(c.AppEnv), "production") {
		for _, o := range c.AllowedOrigins {
			if strings.TrimSpace(o) == "*" {
				return errors.New("ALLOWED_ORIGINS tidak boleh berisi '*' pada mode production")
			}
		}
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
	// #nosec G101 -- literal pembanding untuk menolak password dev bawaan di profil produksi
	if pass == "kopdes_dev_password" {
		return errors.New("pada profil produksi, dilarang menggunakan password default dev (kopdes_dev_password)")
	}
	return nil
}

func DefaultConfig() Config {
	return Config{
		AppEnv:                       "development",
		AppTimezone:                  "Asia/Jakarta",
		Port:                         8080,
		MetricsPort:                  9100,
		JWTSecret:                    "",
		JWTPreviousSecret:            "",
		JWTAlgorithm:                 "HS256",
		JWTIssuer:                    "kopdes-merah-putih",
		JWTAccessTTL:                 15 * time.Minute,
		JWTRefreshTTL:                7 * 24 * time.Hour,
		BlindIndexSecret:             "",
		QRISSecret:                   "",
		AESMasterKey:                 "",
		AESKeyVersion:                "v1",
		AESKeyID:                     "k1",
		AllowedOrigins:               []string{"*"},
		RateLimitRPS:                 20.0,
		RateLimitBurst:               40,
		TrustedProxies:               nil,
		MaxRateLimitClients:          10000,
		MaxBodyBytes:                 10 * 1024 * 1024,
		HTTPReadHeaderTimeout:        5 * time.Second,
		HTTPReadTimeout:              10 * time.Second,
		HTTPWriteTimeout:             15 * time.Second,
		HTTPIdleTimeout:              60 * time.Second,
		BcryptCost:                   12,
		MFAIssuer:                    "Kopdes Merah Putih",
		MFAMaxAttempts:               3,
		MFASessionTTL:                5 * time.Minute,
		MFASetupTTL:                  15 * time.Minute,
		LoginMaxAttempts:             5,
		LoginLockoutDuration:         15 * time.Minute,
		QRISDynamicEnabled:           false,
		AllowPersonalPaymentPilot:    false,
		PilotDefaultMaxPerTx:         1000000,
		PilotDefaultMaxPerDay:        5000000,
		DualApprovalDefaultThreshold: 5000000,
		StorageKYCDir:                "./storage/kyc",
		MaxKYCUploadBytes:            5 * 1024 * 1024,
	}
}

type Server struct {
	cfg           Config
	pool          *pgxpool.Pool
	keyProvider   security.KeyProvider
	httpServer    *http.Server
	metricsServer *http.Server
	router        *http.ServeMux
	metricsRouter http.Handler
}

func NewServer(pool *pgxpool.Pool, cfg Config) *Server {
	def := DefaultConfig()
	if cfg.Port <= 0 {
		cfg.Port = def.Port
	}
	if cfg.MaxRateLimitClients <= 0 {
		cfg.MaxRateLimitClients = def.MaxRateLimitClients
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = def.MaxBodyBytes
	}
	if cfg.HTTPReadHeaderTimeout <= 0 {
		cfg.HTTPReadHeaderTimeout = def.HTTPReadHeaderTimeout
	}
	if cfg.HTTPReadTimeout <= 0 {
		cfg.HTTPReadTimeout = def.HTTPReadTimeout
	}
	if cfg.HTTPWriteTimeout <= 0 {
		cfg.HTTPWriteTimeout = def.HTTPWriteTimeout
	}
	if cfg.HTTPIdleTimeout <= 0 {
		cfg.HTTPIdleTimeout = def.HTTPIdleTimeout
	}
	if cfg.LoginMaxAttempts <= 0 {
		cfg.LoginMaxAttempts = def.LoginMaxAttempts
	}
	if cfg.LoginLockoutDuration <= 0 {
		cfg.LoginLockoutDuration = def.LoginLockoutDuration
	}
	if cfg.MFAMaxAttempts <= 0 {
		cfg.MFAMaxAttempts = def.MFAMaxAttempts
	}
	if cfg.MFASessionTTL <= 0 {
		cfg.MFASessionTTL = def.MFASessionTTL
	}
	if cfg.MFASetupTTL <= 0 {
		cfg.MFASetupTTL = def.MFASetupTTL
	}
	if strings.TrimSpace(cfg.MFAIssuer) == "" {
		cfg.MFAIssuer = def.MFAIssuer
	}
	if cfg.JWTAccessTTL <= 0 {
		cfg.JWTAccessTTL = def.JWTAccessTTL
	}
	if cfg.JWTRefreshTTL <= 0 {
		cfg.JWTRefreshTTL = def.JWTRefreshTTL
	}
	if strings.TrimSpace(cfg.JWTAlgorithm) == "" {
		cfg.JWTAlgorithm = def.JWTAlgorithm
	}
	if strings.TrimSpace(cfg.JWTIssuer) == "" {
		cfg.JWTIssuer = def.JWTIssuer
	}
	if strings.TrimSpace(cfg.AESKeyVersion) == "" {
		cfg.AESKeyVersion = def.AESKeyVersion
	}
	if strings.TrimSpace(cfg.AESKeyID) == "" {
		cfg.AESKeyID = def.AESKeyID
	}

	tm, err := auth.NewTokenManagerWithConfig(cfg.JWTSecret, cfg.JWTPreviousSecret, cfg.JWTAlgorithm, cfg.JWTIssuer)
	if err != nil {
		// Fallback hanya bila tests langsung memanggil NewServer tanpa NewTokenManagerWithConfig valid; Start() tetap gagal bila config invalid
		tm = auth.NewTokenManager(cfg.JWTSecret, cfg.JWTIssuer)
	}
	authMw := middleware.NewAuthMiddleware(tm)
	rateLimiter := middleware.NewIPRateLimiterWithConfig(
		cfg.RateLimitRPS,
		cfg.RateLimitBurst,
		cfg.TrustedProxies,
		cfg.MaxRateLimitClients,
	)
	trustedMatcher := middleware.NewTrustedProxyMatcher(cfg.TrustedProxies)
	loginLimiter := middleware.NewLoginRateLimiter(cfg.LoginMaxAttempts, cfg.LoginLockoutDuration, cfg.MaxRateLimitClients)

	var keyProvider security.KeyProvider
	if strings.TrimSpace(cfg.AESMasterKey) != "" {
		if mkp, err := security.NewMultiKeyProviderFromConfig(cfg.AESKeyVersion, cfg.AESKeyID, cfg.AESMasterKey, cfg.AESPreviousKeys); err == nil {
			keyProvider = mkp
		}
	}

	// Inisialisasi Handlers
	authH := handlers.NewAuthHandlerWithConfig(pool, tm, []byte(cfg.BlindIndexSecret), loginLimiter, trustedMatcher, keyProvider, handlers.AuthConfig{
		AccessTTL:            cfg.JWTAccessTTL,
		RefreshTTL:           cfg.JWTRefreshTTL,
		MFASessionTTL:        cfg.MFASessionTTL,
		MFASetupTTL:          cfg.MFASetupTTL,
		MFAIssuer:            cfg.MFAIssuer,
		MFAMaxAttempts:       cfg.MFAMaxAttempts,
		LoginMaxAttempts:     cfg.LoginMaxAttempts,
		LoginLockoutDuration: cfg.LoginLockoutDuration,
	})
	qrisH := handlers.NewQRISHandlerFull(pool, []byte(cfg.QRISSecret), handlers.QRISConfig{
		DynamicQRISEnabled:           cfg.QRISDynamicEnabled,
		AllowPersonalPaymentPilot:    cfg.AllowPersonalPaymentPilot,
		PilotDefaultMaxPerTx:         cfg.PilotDefaultMaxPerTx,
		PilotDefaultMaxPerDay:        cfg.PilotDefaultMaxPerDay,
		DualApprovalDefaultThreshold: cfg.DualApprovalDefaultThreshold,
		Timezone:                     cfg.AppTimezone,
	})
	iuranH := handlers.NewIuranHandler(pool)
	katalogH := handlers.NewKatalogHandler(pool)
	healthH := handlers.NewHealthHandler(pool)
	metricsH := handlers.NewMetricsHandlerWithRotation(pool, cfg.MetricsToken, cfg.MetricsPreviousToken, trustedMatcher)
	pendaftaranH := handlers.NewPendaftaranHandlerWithConfig(pool, keyProvider, []byte(cfg.BlindIndexSecret), cfg.StorageKYCDir, cfg.MaxKYCUploadBytes)
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
	mux.Handle("POST /api/v1/toko/pesanan/{id}/expire", authMw.RequireAuth(
		middleware.RequireRoles("admin", "pengurus", "bendahara", "ketua", "operator")(http.HandlerFunc(tokoH.HandleExpirePesanan)),
	))

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
	handler = middleware.BodyLimit(cfg.MaxBodyBytes)(handler)
	handler = middleware.RateLimit(rateLimiter)(handler)
	handler = middleware.CORS(corsCfg)(handler)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Recovery(handler)

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout, // Anti-Slowloris attack (S3)
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	metricsMux := http.NewServeMux()
	metricsMux.HandleFunc("GET /metrics", metricsH.HandleMetrics)
	var metricsSrv *http.Server
	if cfg.MetricsPort > 0 && cfg.MetricsPort != cfg.Port {
		metricsSrv = &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.MetricsPort),
			Handler:           middleware.Recovery(middleware.SecurityHeaders(metricsMux)),
			ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
			ReadTimeout:       cfg.HTTPReadTimeout,
			WriteTimeout:      cfg.HTTPWriteTimeout,
			IdleTimeout:       cfg.HTTPIdleTimeout,
		}
	}

	return &Server{
		cfg:           cfg,
		pool:          pool,
		keyProvider:   keyProvider,
		httpServer:    httpSrv,
		metricsServer: metricsSrv,
		router:        mux,
		metricsRouter: metricsMux,
	}
}

// Router mengembalikan instance mux untuk kebutuhan testing langsung via httptest
func (s *Server) Router() http.Handler {
	return s.httpServer.Handler
}

// MetricsRouter mengembalikan router khusus endpoint /metrics
func (s *Server) MetricsRouter() http.Handler {
	return s.metricsRouter
}

// MetricsAddr mengembalikan alamat listen port metrics terpisah (kosong bila berbagi port API)
func (s *Server) MetricsAddr() string {
	if s.metricsServer == nil {
		return ""
	}
	return s.metricsServer.Addr
}

// KeyProvider mengembalikan active encryption key provider
func (s *Server) KeyProvider() security.KeyProvider {
	return s.keyProvider
}

// Start menjalankan HTTP server (serta listener terpisah METRICS_PORT bila diset berbeda dari API_PORT)
func (s *Server) Start() error {
	if err := s.cfg.Validate(); err != nil {
		return fmt.Errorf("gagal memulai server: konfigurasi keamanan tidak valid: %w", err)
	}
	if s.metricsServer != nil {
		go func() {
			if err := s.metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("METRICS SERVER ERROR (%s): %v", s.metricsServer.Addr, err)
			}
		}()
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown mematikan server secara graceful
func (s *Server) Shutdown(ctx context.Context) error {
	var errMetrics error
	if s.metricsServer != nil {
		errMetrics = s.metricsServer.Shutdown(ctx)
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	return errMetrics
}
