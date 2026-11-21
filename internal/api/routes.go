package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/anggota"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/auth"
	"github.com/kopdes/merah-putih/internal/iuran"
	"github.com/kopdes/merah-putih/internal/katalog"
	"github.com/kopdes/merah-putih/internal/laporan"
	"github.com/kopdes/merah-putih/internal/pembayaran"
	"github.com/kopdes/merah-putih/internal/pendaftaran"
	"github.com/kopdes/merah-putih/internal/pesanan"
	"github.com/kopdes/merah-putih/internal/security"
)

type domainHandlers struct {
	health       *handlers.HealthHandler
	publicConfig *handlers.PublicConfigHandler
	auth         *auth.Handler
	pendaftaran  *pendaftaran.Handler
	anggota      *anggota.Handler
	iuran        *iuran.Handler
	katalog      *katalog.Handler
	pesanan      *pesanan.Handler
	pembayaran   *pembayaran.Handler
	laporan      *laporan.Handler
	dbLimiter    *middleware.DBRateLimiter
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

func buildDomainHandlers(pool *pgxpool.Pool, cfg Config, ks *security.PurposeKeySet) *domainHandlers {
	dbKP, kycKP, buktiKP, masterKP := extractPurposeProviders(ks)
	blindKey := decodeSecretBytes(cfg.BlindIndexSecret)
	loginLim := middleware.NewLoginRateLimiter(cfg.LoginMaxAttempts, cfg.LoginLockoutDuration, cfg.MaxRateLimitClients)
	authSvc := auth.NewService(pool, auth.Config{
		AppEnv: cfg.AppEnv, BlindIndexSecret: blindKey, KeyProvider: dbKP,
		BcryptCost: cfg.BcryptCost, SessionIdleTTL: cfg.SessionIdleTTL, SessionAbsTTL: cfg.SessionAbsTTL,
		MFARequiredRoles: cfg.MFARequiredRoles, MFAIssuer: cfg.MFAIssuer, MFAMaxAttempts: cfg.MFAMaxAttempts,
		LoginMaxAttempts: cfg.LoginMaxAttempts, LoginLockoutDuration: cfg.LoginLockoutDuration,
		TrustedProxies: cfg.TrustedProxies, CaptchaEnabled: cfg.CaptchaEnabled, CaptchaSecretKey: cfg.CaptchaSecretKey,
	}, loginLim)

	kycDir, buktiDir := resolveStorageDirs(cfg)
	kycStore, _ := pendaftaran.NewKYCFileStore(kycDir, kycKP, cfg.MaxKYCUploadBytes)
	pendSvc := pendaftaran.NewService(pool, kycStore, pendaftaran.Config{
		RegistrationEnabled: !cfg.RegistrationDisabled, CaptchaEnabled: cfg.CaptchaEnabled,
		CaptchaSecretKey: cfg.CaptchaSecretKey, KeyProvider: dbKP, BlindIndexSecret: blindKey,
		RetentionDays: cfg.PDPRejectedRetentionDays, TrustedProxies: cfg.TrustedProxies,
	})
	angSvc := anggota.NewService(pool, anggota.Config{KeyProvider: dbKP, BlindIndexSecret: blindKey, TrustedProxies: cfg.TrustedProxies})
	pesSvc := pesanan.NewService(pool, dbKP, pesanan.Config{
		OrderExpiryMinutes: maxInt(15, int(cfg.PaymentExpiryDuration.Minutes())),
		StockHoldMinutes:   cfg.StockHoldMinutes,
		Timezone:           cfg.AppTimezone,
		TrustedProxies:     cfg.TrustedProxies,
	})
	bayarSvc, _ := pembayaran.NewService(pool, pembayaran.Config{
		PaymentsEnabled: !cfg.PaymentsDisabled, AllowPersonalPilot: cfg.AllowPersonalPaymentPilot,
		PilotMaxPerTx: cfg.PilotMaxPerTx, PilotMaxPerDay: cfg.PilotMaxPerDay,
		PaymentExpiry: cfg.PaymentExpiryDuration, MaxPendingPerUser: cfg.PendingLimitPerMember,
		MaxPendingGlobal: cfg.PendingLimitGlobal, DualControlBreakglassEnabled: cfg.DualControlBreakglassEnabled,
		Timezone: cfg.AppTimezone, UploadMaxBytes: cfg.MaxKYCUploadBytes, ProofStorageDir: buktiDir,
		LegacyKYCStorageDir: filepath.Join(kycDir, "bukti"), BuktiRetentionDays: cfg.BuktiRetentionDays,
		KeyProvider: buktiKP, DBKeyProvider: dbKP, LegacyKeyProvider: masterKP, TrustedProxies: cfg.TrustedProxies,
	})
	return &domainHandlers{
		health: handlers.NewHealthHandler(pool), publicConfig: handlers.NewPublicConfigHandler(buildPublicConfigOpts(pool, cfg)),
		auth: auth.NewHandler(authSvc), pendaftaran: pendaftaran.NewHandler(pendSvc),
		anggota: anggota.NewHandler(angSvc), iuran: iuran.NewHandler(iuran.NewService(pool, cfg.TrustedProxies)),
		katalog: katalog.NewHandler(katalog.NewService(pool, cfg.TrustedProxies)), pesanan: pesanan.NewHandler(pesSvc),
		pembayaran: pembayaran.NewHandler(bayarSvc), laporan: laporan.NewHandler(laporan.NewService(pool, cfg.AppTimezone)),
		dbLimiter: middleware.NewDBRateLimiter(pool, cfg.TrustedProxies),
	}
}

func buildPublicConfigOpts(pool *pgxpool.Pool, cfg Config) handlers.PublicConfigOptions {
	return handlers.PublicConfigOptions{
		Pool: pool, APIVersion: cfg.APIVersion, Timezone: cfg.AppTimezone,
		MaxBodyBytes: cfg.MaxBodyBytes, MaxKYCUploadBytes: cfg.MaxKYCUploadBytes,
		PaymentExpirySeconds: int64(cfg.PaymentExpiryDuration.Seconds()),
		PollingIntervalMS: cfg.PollingIntervalMS, AccessTokenTTLSeconds: int64(cfg.SessionIdleTTL.Seconds()),
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

func registerPublicAuthRoutes(mux *http.ServeMux, pool *pgxpool.Pool, cfg Config, dh *domainHandlers) {
	limSes := middleware.RequireSession(pool, cfg.SessionIdleTTL, true)
	optSes := middleware.OptionalSession(pool, cfg.SessionIdleTTL)
	fullSes := middleware.RequireSession(pool, cfg.SessionIdleTTL, false)
	staffRoles := middleware.RequireRoles("admin", "bendahara")
	adminOnly := middleware.RequireRoles("admin")

	mux.HandleFunc("GET /healthz", dh.health.HandleHealthz)
	mux.HandleFunc("GET /readyz", dh.health.HandleReadyz)
	mux.HandleFunc("GET /api/v1/public/config", dh.publicConfig.HandleGetPublicConfig)
	mux.Handle("POST /api/v1/public/pendaftaran", dbLimit(dh.dbLimiter, "registration", 20, 3600)(http.HandlerFunc(dh.pendaftaran.HandlePublicRegister)))
	mux.Handle("POST /api/v1/public/pendaftaran/{id}/dokumen", dbLimit(dh.dbLimiter, "kyc_upload", 40, 3600)(http.HandlerFunc(dh.pendaftaran.HandleUploadDokumen)))
	mux.HandleFunc("POST /api/v1/auth/login", dh.auth.HandleLogin)
	mux.Handle("POST /api/v1/auth/mfa/setup", limSes(http.HandlerFunc(dh.auth.HandleMFASetup)))
	mux.Handle("POST /api/v1/auth/mfa/verify", limSes(http.HandlerFunc(dh.auth.HandleMFAVerify)))
	mux.Handle("POST /api/v1/auth/logout", limSes(http.HandlerFunc(dh.auth.HandleLogout)))
	mux.Handle("POST /api/v1/auth/password", optSes(http.HandlerFunc(dh.auth.HandlePassword)))
	mux.Handle("GET /api/v1/auth/me", limSes(http.HandlerFunc(dh.auth.HandleMe)))
	mux.Handle("PUT /api/v1/pengaturan", fullSes(staffRoles(http.HandlerFunc(dh.auth.HandleUpdatePengaturan))))
	mux.Handle("GET /api/v1/users", fullSes(adminOnly(http.HandlerFunc(dh.auth.HandleListUsers))))
	mux.Handle("POST /api/v1/users", fullSes(adminOnly(http.HandlerFunc(dh.auth.HandleCreateUser))))
	mux.Handle("PATCH /api/v1/users/{id}", fullSes(adminOnly(http.HandlerFunc(dh.auth.HandleUpdateUser))))
}

func registerMemberAdminRoutes(mux *http.ServeMux, pool *pgxpool.Pool, cfg Config, dh *domainHandlers) {
	fullSes := middleware.RequireSession(pool, cfg.SessionIdleTTL, false)
	adminOnly := middleware.RequireRoles("admin")
	staffMember := middleware.RequireRoles("admin", "bendahara", "petugas_toko")
	iuranRoles := middleware.RequireRoles("admin", "bendahara")

	mux.Handle("GET /api/v1/pendaftaran", fullSes(adminOnly(http.HandlerFunc(dh.pendaftaran.HandleListPendaftaran))))
	mux.Handle("POST /api/v1/pendaftaran/{id}/verifikasi", fullSes(adminOnly(http.HandlerFunc(dh.pendaftaran.HandleVerifyDecision))))
	mux.Handle("GET /api/v1/pendaftaran/{id}/dokumen/{tipe}", fullSes(adminOnly(http.HandlerFunc(dh.pendaftaran.HandleGetDokumen))))
	mux.Handle("GET /api/v1/anggota", fullSes(staffMember(http.HandlerFunc(dh.anggota.HandleList))))
	mux.Handle("GET /api/v1/anggota/{id}", fullSes(http.HandlerFunc(dh.anggota.HandleGetByID)))
	mux.Handle("PATCH /api/v1/anggota/{id}/status", fullSes(adminOnly(http.HandlerFunc(dh.anggota.HandleUpdateStatus))))
	mux.Handle("POST /api/v1/anggota/impor", fullSes(adminOnly(http.HandlerFunc(dh.anggota.HandleImportCSV))))
	mux.Handle("POST /api/v1/iuran/jenis", fullSes(iuranRoles(http.HandlerFunc(dh.iuran.HandleCreateJenis))))
	mux.Handle("GET /api/v1/iuran/tagihan", fullSes(http.HandlerFunc(dh.iuran.HandleListTagihan)))
	mux.Handle("PATCH /api/v1/iuran/tagihan/{id}/keringanan", fullSes(iuranRoles(http.HandlerFunc(dh.iuran.HandleKeringanan))))
}

func registerCommerceFinanceRoutes(mux *http.ServeMux, pool *pgxpool.Pool, cfg Config, dh *domainHandlers) {
	optSes := middleware.OptionalSession(pool, cfg.SessionIdleTTL)
	fullSes := middleware.RequireSession(pool, cfg.SessionIdleTTL, false)
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

func dbLimit(lim *middleware.DBRateLimiter, cat string, maxHits, windowSecs int32) func(http.Handler) http.Handler {
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
