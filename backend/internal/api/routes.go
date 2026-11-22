package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/anggota"
	angapi "github.com/kopdes/merah-putih/internal/anggota/api"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/auth"
	"github.com/kopdes/merah-putih/internal/filestore"
	"github.com/kopdes/merah-putih/internal/iuran"
	"github.com/kopdes/merah-putih/internal/katalog"
	"github.com/kopdes/merah-putih/internal/laporan"
	"github.com/kopdes/merah-putih/internal/pembayaran"
	"github.com/kopdes/merah-putih/internal/pengaturan"
	"github.com/kopdes/merah-putih/internal/pesanan"
	"github.com/kopdes/merah-putih/internal/security"
)

type domainHandlers struct {
	health        *handlers.HealthHandler
	publicConfig  *handlers.PublicConfigHandler
	pengaturan    *pengaturan.Handler
	auth          *auth.Handler
	anggota       *anggota.Handler
	iuran         *iuran.Handler
	katalog       *katalog.Handler
	pesanan       *pesanan.Handler
	pembayaran    *pembayaran.Handler
	laporan       *laporan.Handler
	hybridLimiter *middleware.HybridRateLimiter
	memberLookup  func(ctx context.Context, userID uuid.UUID) (uuid.UUID, string, bool, error)
}

func resolveStorageDirs(cfg Config) (string, string) {
	kycDir := strings.TrimSpace(cfg.StorageKYCDir)
	if kycDir == "" {
		kycDir = "./storage/kyc"
	}
	buktiDir := strings.TrimSpace(cfg.StorageBuktiDir)
	if buktiDir == "" {
		buktiDir = "./storage/bukti"
	}
	return kycDir, buktiDir
}

func extractPurposeProviders(ks *security.PurposeKeySet) (dbKP, kycKP, buktiKP, masterKP security.KeyProvider) {
	if ks == nil {
		return nil, nil, nil, nil
	}
	return ks.DB, ks.KYC, ks.Bukti, ks.Master
}

func initAppFileStore(cfg Config, kycDir, buktiDir string, kycKP, buktiKP, masterKP security.KeyProvider) filestore.FileStore {
	if cfg.FileStore != nil {
		return cfg.FileStore
	}
	store, err := filestore.NewLocalFileStore(map[string]filestore.NamespaceConfig{
		filestore.NamespaceKYC: {Dir: kycDir, KeyProvider: kycKP, LegacyKeyProvider: masterKP},
		filestore.NamespaceBukti: {
			Dir: buktiDir, FallbackDirs: []string{filepath.Join(kycDir, "bukti")},
			KeyProvider: buktiKP, LegacyKeyProvider: masterKP,
		},
	})
	if err != nil {
		return nil
	}
	return store
}

func buildCoreServices(pool *pgxpool.Pool, cfg Config, ks *security.PurposeKeySet, store filestore.FileStore) (*pengaturan.Service, *auth.Service, *anggota.Service) {
	dbKP, _, _, _ := extractPurposeProviders(ks)
	blindKey := decodeSecretBytes(cfg.BlindIndexSecret)
	pengSvc := pengaturan.NewService(pool, cfg.TrustedProxies)
	loginLim := middleware.NewLoginRateLimiter(cfg.LoginMaxAttempts, cfg.LoginLockoutDuration, cfg.MaxRateLimitClients)
	authSvc := auth.NewService(pool, auth.Config{
		AppEnv: cfg.AppEnv, BlindIndexSecret: blindKey, KeyProvider: dbKP, Audit: pengSvc,
		BcryptCost: cfg.BcryptCost, SessionIdleTTL: cfg.SessionIdleTTL, SessionAbsTTL: cfg.SessionAbsTTL,
		CookieSecure: cfg.CookieSecure, AllowBearerAuth: cfg.AllowBearerAuth,
		MFARequiredRoles: cfg.MFARequiredRoles, MFAIssuer: cfg.MFAIssuer, MFAMaxAttempts: cfg.MFAMaxAttempts,
		LoginMaxAttempts: cfg.LoginMaxAttempts, LoginLockoutDuration: cfg.LoginLockoutDuration,
		TrustedProxies: cfg.TrustedProxies, CaptchaEnabled: cfg.CaptchaEnabled, CaptchaSecretKey: cfg.CaptchaSecretKey,
	}, loginLim)
	angSvc := anggota.NewService(pool, anggota.Config{
		KeyProvider: dbKP, BlindIndexSecret: blindKey, TrustedProxies: cfg.TrustedProxies,
		RegistrationEnabled: !cfg.RegistrationDisabled, CaptchaEnabled: cfg.CaptchaEnabled,
		CaptchaSecretKey: cfg.CaptchaSecretKey, RetentionDays: cfg.PDPRejectedRetentionDays,
		MaxKYCUploadBytes: cfg.MaxKYCUploadBytes, FileStore: store, UserManager: authSvc, Audit: pengSvc,
	})
	authSvc.SetMembers(angSvc)
	return pengSvc, authSvc, angSvc
}

func buildDomainHandlers(pool *pgxpool.Pool, cfg Config, ks *security.PurposeKeySet) *domainHandlers {
	dbKP, kycKP, buktiKP, masterKP := extractPurposeProviders(ks)
	kycDir, buktiDir := resolveStorageDirs(cfg)
	fstore := initAppFileStore(cfg, kycDir, buktiDir, kycKP, buktiKP, masterKP)
	pengSvc, authSvc, angSvc := buildCoreServices(pool, cfg, ks, fstore)

	iurSvc := iuran.NewService(pool, cfg.TrustedProxies, angSvc, pengSvc)
	katSvc := katalog.NewService(pool, cfg.TrustedProxies, angSvc, pengSvc)
	pesSvc := pesanan.NewService(pool, dbKP, pesanan.Config{
		OrderExpiryMinutes: maxInt(15, int(cfg.PaymentExpiryDuration.Minutes())),
		StockHoldMinutes:   cfg.StockHoldMinutes, Timezone: cfg.AppTimezone, TrustedProxies: cfg.TrustedProxies,
		Settings: pengSvc, Audit: pengSvc, Members: angSvc, Stock: katSvc,
	})
	bayarSvc, _ := pembayaran.NewService(pool, pembayaran.Config{
		PaymentsEnabled: !cfg.PaymentsDisabled, AllowPersonalPilot: cfg.AllowPersonalPaymentPilot,
		PilotMaxPerTx: cfg.PilotMaxPerTx, PilotMaxPerDay: cfg.PilotMaxPerDay,
		PaymentExpiry: cfg.PaymentExpiryDuration, MaxPendingPerUser: cfg.PendingLimitPerMember,
		MaxPendingGlobal: cfg.PendingLimitGlobal, DualControlBreakglassEnabled: cfg.DualControlBreakglassEnabled,
		Timezone: cfg.AppTimezone, UploadMaxBytes: cfg.MaxKYCUploadBytes, ProofStorageDir: buktiDir,
		LegacyKYCStorageDir: filepath.Join(kycDir, "bukti"), BuktiRetentionDays: cfg.BuktiRetentionDays,
		KeyProvider: buktiKP, DBKeyProvider: dbKP, LegacyKeyProvider: masterKP, FileStore: fstore,
		Settings: pengSvc, Audit: pengSvc, Members: angSvc, Bills: iurSvc, Orders: pesSvc, BreakglassTOTP: authSvc,
		TrustedProxies: cfg.TrustedProxies,
	})
	pesSvc.SetActivePaymentChecker(bayarSvc)
	lapSvc := laporan.NewService(angSvc, iurSvc, pesSvc, bayarSvc, cfg.AppTimezone)
	return assembleDomainHandlers(pool, cfg, pengSvc, authSvc, angSvc, iurSvc, katSvc, pesSvc, bayarSvc, lapSvc)
}

func assembleDomainHandlers(
	pool *pgxpool.Pool, cfg Config,
	pengSvc *pengaturan.Service, authSvc *auth.Service, angSvc *anggota.Service,
	iurSvc *iuran.Service, katSvc *katalog.Service, pesSvc *pesanan.Service,
	bayarSvc *pembayaran.Service, lapSvc *laporan.Service,
) *domainHandlers {
	pubOpts := buildPublicConfigOpts(pengSvc, bayarSvc, cfg)
	return &domainHandlers{
		health: handlers.NewHealthHandler(pool), publicConfig: handlers.NewPublicConfigHandler(pubOpts),
		pengaturan: pengaturan.NewHandler(pengSvc), auth: auth.NewHandler(authSvc),
		anggota: anggota.NewHandler(angSvc), iuran: iuran.NewHandler(iurSvc),
		katalog: katalog.NewHandler(katSvc), pesanan: pesanan.NewHandler(pesSvc),
		pembayaran: pembayaran.NewHandler(bayarSvc), laporan: laporan.NewHandler(lapSvc),
		hybridLimiter: middleware.NewHybridRateLimiter(pool, cfg.TrustedProxies, cfg.MaxRateLimitClients),
		memberLookup: func(ctx context.Context, userID uuid.UUID) (uuid.UUID, string, bool, error) {
			info, err := angSvc.GetByUserID(ctx, userID)
			if err != nil {
				if errors.Is(err, angapi.ErrNotFound) {
					return uuid.Nil, "", false, nil
				}
				return uuid.Nil, "", false, err
			}
			return info.ID, info.Status, true, nil
		},
	}
}

func buildPublicConfigOpts(pengSvc *pengaturan.Service, bayarSvc *pembayaran.Service, cfg Config) handlers.PublicConfigOptions {
	return handlers.PublicConfigOptions{
		Settings: pengSvc, QRIS: bayarSvc, APIVersion: cfg.APIVersion, Timezone: cfg.AppTimezone,
		MaxBodyBytes: cfg.MaxBodyBytes, MaxKYCUploadBytes: cfg.MaxKYCUploadBytes,
		PaymentExpirySeconds: int64(cfg.PaymentExpiryDuration.Seconds()),
		PollingIntervalMS: cfg.PollingIntervalMS, AccessTokenTTLSeconds: int64(cfg.SessionIdleTTL.Seconds()),
		CookieSecure: cfg.CookieSecure, AllowBearerAuth: cfg.AllowBearerAuth, StockHoldMinutes: cfg.StockHoldMinutes,
		PaymentsEnabled: !cfg.PaymentsDisabled, RegistrationEnabled: !cfg.RegistrationDisabled,
		LoginEnabled: true, ReadOnlyMode: cfg.ReadOnlyMode,
		PersonalPaymentPilotEnabled: cfg.AllowPersonalPaymentPilot, CaptchaEnabled: cfg.CaptchaEnabled,
		CaptchaProvider: cfg.CaptchaProvider, CaptchaSiteKey: cfg.CaptchaSiteKey,
		MFAMandatoryRoles: cfg.MFARequiredRoles, PaginationDefaultLimit: cfg.PaginationDefaultLimit,
		PaginationMaxLimit: cfg.PaginationMaxLimit,
	}
}

func registerAllRoutes(mux *http.ServeMux, pool *pgxpool.Pool, cfg Config, dh *domainHandlers) {
	registerPublicAuthRoutes(mux, pool, cfg, dh)
	registerMemberAdminRoutes(mux, pool, cfg, dh)
	registerCommerceFinanceRoutes(mux, pool, cfg, dh)
}

func sessionOptsFromConfig(cfg Config, dh *domainHandlers) middleware.SessionAuthOptions {
	return middleware.SessionAuthOptions{
		IdleTTL: cfg.SessionIdleTTL, AllowBearer: cfg.AllowBearerAuth, MemberLookup: dh.memberLookup,
	}
}

func registerPublicAuthRoutes(mux *http.ServeMux, pool *pgxpool.Pool, cfg Config, dh *domainHandlers) {
	sesOpts := sessionOptsFromConfig(cfg, dh)
	limSes := middleware.RequireSessionWithOptions(pool, sesOpts, true)
	optSes := middleware.OptionalSessionWithOptions(pool, sesOpts)
	fullSes := middleware.RequireSessionWithOptions(pool, sesOpts, false)
	staffRoles := middleware.RequireRoles("admin", "bendahara")
	adminOnly := middleware.RequireRoles("admin")

	mux.HandleFunc("GET /healthz", dh.health.HandleHealthz)
	mux.HandleFunc("GET /readyz", dh.health.HandleReadyz)
	mux.HandleFunc("GET /api/v1/public/config", dh.publicConfig.HandleGetPublicConfig)
	mux.Handle("POST /api/v1/public/pendaftaran", hybridLimit(dh.hybridLimiter, "registration", 20, 3600)(http.HandlerFunc(dh.anggota.HandlePublicRegister)))
	mux.Handle("POST /api/v1/public/pendaftaran/{id}/dokumen", hybridLimit(dh.hybridLimiter, "kyc_upload", 40, 3600)(http.HandlerFunc(dh.anggota.HandleUploadDokumen)))
	mux.HandleFunc("POST /api/v1/auth/login", dh.auth.HandleLogin)
	mux.Handle("POST /api/v1/auth/mfa/setup", limSes(http.HandlerFunc(dh.auth.HandleMFASetup)))
	mux.Handle("POST /api/v1/auth/mfa/verify", limSes(http.HandlerFunc(dh.auth.HandleMFAVerify)))
	mux.Handle("POST /api/v1/auth/logout", limSes(http.HandlerFunc(dh.auth.HandleLogout)))
	mux.Handle("POST /api/v1/auth/password", optSes(http.HandlerFunc(dh.auth.HandlePassword)))
	mux.Handle("GET /api/v1/auth/me", limSes(http.HandlerFunc(dh.auth.HandleMe)))
	mux.Handle("PUT /api/v1/pengaturan", fullSes(staffRoles(http.HandlerFunc(dh.pengaturan.HandleUpdate))))
	mux.Handle("GET /api/v1/audit-log", fullSes(adminOnly(http.HandlerFunc(dh.pengaturan.HandleListAuditLog))))
	mux.Handle("GET /api/v1/users", fullSes(adminOnly(http.HandlerFunc(dh.auth.HandleListUsers))))
	mux.Handle("POST /api/v1/users", fullSes(adminOnly(http.HandlerFunc(dh.auth.HandleCreateUser))))
	mux.Handle("PATCH /api/v1/users/{id}", fullSes(adminOnly(http.HandlerFunc(dh.auth.HandleUpdateUser))))
}

func registerMemberAdminRoutes(mux *http.ServeMux, pool *pgxpool.Pool, cfg Config, dh *domainHandlers) {
	sesOpts := sessionOptsFromConfig(cfg, dh)
	fullSes := middleware.RequireSessionWithOptions(pool, sesOpts, false)
	adminOnly := middleware.RequireRoles("admin")
	staffMember := middleware.RequireRoles("admin", "bendahara", "petugas_toko")
	iuranRoles := middleware.RequireRoles("admin", "bendahara")

	mux.Handle("GET /api/v1/pendaftaran", fullSes(adminOnly(http.HandlerFunc(dh.anggota.HandleListPendaftaran))))
	mux.Handle("POST /api/v1/pendaftaran/{id}/verifikasi", fullSes(adminOnly(http.HandlerFunc(dh.anggota.HandleVerifyDecision))))
	mux.Handle("GET /api/v1/pendaftaran/{id}/dokumen/{tipe}", fullSes(adminOnly(http.HandlerFunc(dh.anggota.HandleGetDokumen))))
	mux.Handle("GET /api/v1/anggota", fullSes(staffMember(http.HandlerFunc(dh.anggota.HandleList))))
	mux.Handle("GET /api/v1/anggota/{id}", fullSes(http.HandlerFunc(dh.anggota.HandleGetByID)))
	mux.Handle("PATCH /api/v1/anggota/{id}/status", fullSes(adminOnly(http.HandlerFunc(dh.anggota.HandleUpdateStatus))))
	mux.Handle("POST /api/v1/anggota/impor", fullSes(adminOnly(http.HandlerFunc(dh.anggota.HandleImportCSV))))
	mux.Handle("POST /api/v1/iuran/jenis", fullSes(iuranRoles(http.HandlerFunc(dh.iuran.HandleCreateJenis))))
	mux.Handle("GET /api/v1/iuran/tagihan", fullSes(http.HandlerFunc(dh.iuran.HandleListTagihan)))
	mux.Handle("PATCH /api/v1/iuran/tagihan/{id}/keringanan", fullSes(iuranRoles(http.HandlerFunc(dh.iuran.HandleKeringanan))))
}

func registerCommerceFinanceRoutes(mux *http.ServeMux, pool *pgxpool.Pool, cfg Config, dh *domainHandlers) {
	sesOpts := sessionOptsFromConfig(cfg, dh)
	optSes := middleware.OptionalSessionWithOptions(pool, sesOpts)
	fullSes := middleware.RequireSessionWithOptions(pool, sesOpts, false)
	tokoRoles := middleware.RequireRoles("admin", "petugas_toko")
	bayarRoles := middleware.RequireRoles("admin", "bendahara")
	laporanRoles := middleware.RequireRoles("admin", "bendahara", "petugas_toko")

	mux.Handle("GET /api/v1/katalog/produk", optSes(http.HandlerFunc(dh.katalog.HandleListProduk)))
	mux.Handle("POST /api/v1/katalog/produk", fullSes(tokoRoles(http.HandlerFunc(dh.katalog.HandleWriteProduk))))
	mux.Handle("POST /api/v1/toko/checkout", fullSes(http.HandlerFunc(dh.pesanan.HandleCheckout)))
	mux.Handle("GET /api/v1/pesanan", fullSes(http.HandlerFunc(dh.pesanan.HandleList)))
	mux.Handle("PATCH /api/v1/pesanan/{id}/status", fullSes(http.HandlerFunc(dh.pesanan.HandleUpdateStatus)))
	mux.Handle("GET /api/v1/pesanan/daftar-antar", fullSes(tokoRoles(http.HandlerFunc(dh.pesanan.HandleDaftarAntar))))
	mux.Handle("GET /api/v1/pembayaran", fullSes(http.HandlerFunc(dh.pembayaran.HandleList)))
	mux.Handle("POST /api/v1/pembayaran", fullSes(http.HandlerFunc(dh.pembayaran.HandlePost)))
	mux.Handle("POST /api/v1/pembayaran/{id}/verifikasi", fullSes(bayarRoles(http.HandlerFunc(dh.pembayaran.HandleVerify))))
	mux.Handle("POST /api/v1/pembayaran/{id}/refund", fullSes(bayarRoles(http.HandlerFunc(dh.pembayaran.HandleRefund))))
	mux.Handle("GET /api/v1/laporan", fullSes(laporanRoles(http.HandlerFunc(dh.laporan.HandleGet))))
}

func hybridLimit(lim *middleware.HybridRateLimiter, cat string, maxHits, windowSecs int32) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if lim != nil {
				key := cat + ":" + lim.ClientIP(r)
				allowed, _, err := lim.Allow(r.Context(), key, cat, maxHits, windowSecs)
				if err == nil && !allowed {
					response.Error(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "Terlalu banyak permintaan dari IP ini")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func readOnlyGuard(readOnly bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if readOnly && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
				if r.URL.Path != "/api/v1/auth/login" && r.URL.Path != "/api/v1/auth/logout" {
					response.Error(w, http.StatusServiceUnavailable, "READ_ONLY_MODE", "Sistem sedang dalam mode baca-saja")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
