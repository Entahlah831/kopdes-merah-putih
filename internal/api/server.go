package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
)

type Config struct {
	AppEnv                    string
	AppTimezone               string
	Port                      int
	BlindIndexSecret          string
	AESMasterKey              string
	AESPreviousKeys           string
	AllowedOrigins            []string
	TrustedProxies            []string
	RateLimitRPS              float64
	RateLimitBurst            int
	MaxRateLimitClients       int
	MaxBodyBytes              int64
	HTTPReadHeaderTimeout     time.Duration
	HTTPReadTimeout           time.Duration
	HTTPWriteTimeout          time.Duration
	HTTPIdleTimeout           time.Duration
	BcryptCost                int
	MFAIssuer                 string
	MFAMaxAttempts            int
	SessionIdleTTL            time.Duration
	SessionAbsTTL             time.Duration
	MFARequiredRoles          []string
	LoginMaxAttempts          int
	LoginLockoutDuration      time.Duration
	AllowPersonalPaymentPilot bool
	PilotMaxPerTx             int64
	PilotMaxPerDay            int64
	QRISUniqueDigits          int
	PaymentExpiryDuration     time.Duration
	PendingLimitPerMember     int
	PendingLimitGlobal        int
	PDPRejectedRetentionDays  int
	StorageKYCDir             string
	MaxKYCUploadBytes         int64
	APIVersion                string
	PaymentsDisabled          bool
	RegistrationDisabled      bool
	ReadOnlyMode              bool
	CaptchaEnabled            bool
	CaptchaProvider           string
	CaptchaSiteKey            string
	CaptchaSecretKey          string
	PollingIntervalMS         int
	PaginationDefaultLimit    int
	PaginationMaxLimit        int
}

func decodeSecretBytes(secret string) []byte {
	return security.DecodeSecretMaterial(secret)
}

func defaultBcryptCost(appEnv string) int {
	if strings.EqualFold(strings.TrimSpace(appEnv), "production") {
		return 12
	}
	return 10
}

func ConfigFromAppConfig(c config.AppConfig) Config {
	def := DefaultConfig()
	return Config{
		AppEnv:                    c.AppEnv,
		AppTimezone:               c.AppTimezone,
		Port:                      c.APIPort,
		BlindIndexSecret:          c.BlindIndexSecret,
		AESMasterKey:              c.AESMasterKey,
		AESPreviousKeys:           c.AESPreviousKeys,
		AllowedOrigins:            c.AllowedOrigins,
		TrustedProxies:            c.TrustedProxies,
		RateLimitRPS:              def.RateLimitRPS,
		RateLimitBurst:            def.RateLimitBurst,
		MaxRateLimitClients:       def.MaxRateLimitClients,
		MaxBodyBytes:              def.MaxBodyBytes,
		HTTPReadHeaderTimeout:     def.HTTPReadHeaderTimeout,
		HTTPReadTimeout:           def.HTTPReadTimeout,
		HTTPWriteTimeout:          def.HTTPWriteTimeout,
		HTTPIdleTimeout:           def.HTTPIdleTimeout,
		BcryptCost:                defaultBcryptCost(c.AppEnv),
		MFAIssuer:                 def.MFAIssuer,
		MFAMaxAttempts:            def.MFAMaxAttempts,
		SessionIdleTTL:            c.SessionIdleTTL,
		SessionAbsTTL:             c.SessionAbsTTL,
		MFARequiredRoles:          c.MFARequiredRoles,
		LoginMaxAttempts:          c.LoginMaxAttempts,
		LoginLockoutDuration:      c.LoginLockoutDuration,
		AllowPersonalPaymentPilot: c.AllowPersonalPaymentPilot,
		PilotMaxPerTx:             c.PilotMaxPerTx,
		PilotMaxPerDay:            c.PilotMaxPerDay,
		QRISUniqueDigits:          c.QRISUniqueDigits,
		PaymentExpiryDuration:     c.PaymentExpiryDuration,
		PendingLimitPerMember:     c.PendingLimitPerMember,
		PendingLimitGlobal:        c.PendingLimitGlobal,
		PDPRejectedRetentionDays:  c.PDPRejectedRetentionDays,
		StorageKYCDir:             c.StorageKYCDir,
		MaxKYCUploadBytes:         def.MaxKYCUploadBytes,
		APIVersion:                def.APIVersion,
		PaymentsDisabled:          !c.PaymentsEnabled,
		RegistrationDisabled:      !c.RegistrationEnabled,
		CaptchaEnabled:            c.CaptchaEnabled,
		CaptchaProvider:           def.CaptchaProvider,
		CaptchaSiteKey:            c.CaptchaSiteKey,
		CaptchaSecretKey:          c.CaptchaSecretKey,
		PollingIntervalMS:         def.PollingIntervalMS,
		PaginationDefaultLimit:    def.PaginationDefaultLimit,
		PaginationMaxLimit:        def.PaginationMaxLimit,
	}
}

func (c *Config) Validate() error {
	if err := security.ValidateSecretKey("BLIND_INDEX_SECRET", decodeSecretBytes(c.BlindIndexSecret)); err != nil {
		return err
	}
	if err := security.ValidateSecretKey("AES_MASTER_KEY", decodeSecretBytes(c.AESMasterKey)); err != nil {
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

func ValidateProductionDatabasePassword(appEnv, dbPassword string) error {
	if !strings.EqualFold(strings.TrimSpace(appEnv), "production") {
		return nil
	}
	pass := strings.TrimSpace(dbPassword)
	if pass == "" {
		return errors.New("pada profil produksi, DB_PASSWORD wajib disetel")
	}
	// #nosec G101 -- literal pembanding untuk menolak password dev bawaan di profil produksi
	if pass == "kopdes_dev_password" {
		return errors.New("pada profil produksi, dilarang menggunakan password default dev (kopdes_dev_password)")
	}
	return nil
}

func DefaultConfig() Config {
	return Config{
		AppEnv:                   "development",
		AppTimezone:              "Asia/Jakarta",
		Port:                     8080,
		AllowedOrigins:           []string{"*"},
		RateLimitRPS:             30,
		RateLimitBurst:           60,
		MaxRateLimitClients:      10000,
		MaxBodyBytes:             10 * 1024 * 1024,
		HTTPReadHeaderTimeout:    5 * time.Second,
		HTTPReadTimeout:          10 * time.Second,
		HTTPWriteTimeout:         15 * time.Second,
		HTTPIdleTimeout:          60 * time.Second,
		BcryptCost:               10,
		MFAIssuer:                "Kopdes Merah Putih",
		MFAMaxAttempts:           3,
		SessionIdleTTL:           30 * time.Minute,
		SessionAbsTTL:            12 * time.Hour,
		MFARequiredRoles:         []string{"admin", "bendahara"},
		LoginMaxAttempts:         5,
		LoginLockoutDuration:     15 * time.Minute,
		PilotMaxPerTx:            500000,
		PilotMaxPerDay:           2000000,
		QRISUniqueDigits:         3,
		PaymentExpiryDuration:    15 * time.Minute,
		PendingLimitPerMember:    2,
		PendingLimitGlobal:       50,
		PDPRejectedRetentionDays: 30,
		StorageKYCDir:            "./storage/kyc",
		MaxKYCUploadBytes:        5 * 1024 * 1024,
		APIVersion:               "2.0.0-lean",
		PollingIntervalMS:        3000,
		PaginationDefaultLimit:   20,
		PaginationMaxLimit:       100,
		CaptchaProvider:          "turnstile",
	}
}

type Server struct {
	cfg         Config
	pool        *pgxpool.Pool
	keyProvider security.KeyProvider
	httpServer  *http.Server
	router      *http.ServeMux
}

func applyServerDefaults(cfg *Config) {
	def := DefaultConfig()
	if cfg.Port <= 0 {
		cfg.Port = def.Port
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
	if cfg.SessionIdleTTL <= 0 {
		cfg.SessionIdleTTL = def.SessionIdleTTL
	}
	if len(cfg.MFARequiredRoles) == 0 {
		cfg.MFARequiredRoles = def.MFARequiredRoles
	}
}

func NewServer(pool *pgxpool.Pool, cfg Config) *Server {
	applyServerDefaults(&cfg)

	var keyProvider security.KeyProvider
	if strings.TrimSpace(cfg.AESMasterKey) != "" {
		ver, kid, mat := security.ParseMasterKeySpec(cfg.AESMasterKey)
		if mkp, err := security.NewMultiKeyProviderFromConfig(ver, kid, mat, cfg.AESPreviousKeys); err == nil {
			keyProvider = mkp
		}
	}

	healthH := handlers.NewHealthHandler(pool)
	publicConfigH := handlers.NewPublicConfigHandler(handlers.PublicConfigOptions{
		APIVersion:                  cfg.APIVersion,
		Timezone:                    cfg.AppTimezone,
		MaxBodyBytes:                cfg.MaxBodyBytes,
		MaxKYCUploadBytes:           cfg.MaxKYCUploadBytes,
		PaymentExpirySeconds:        int64(cfg.PaymentExpiryDuration.Seconds()),
		PollingIntervalMS:           cfg.PollingIntervalMS,
		AccessTokenTTLSeconds:       int64(cfg.SessionIdleTTL.Seconds()),
		PaymentsEnabled:             !cfg.PaymentsDisabled,
		RegistrationEnabled:         !cfg.RegistrationDisabled,
		LoginEnabled:                true,
		ReadOnlyMode:                cfg.ReadOnlyMode,
		PersonalPaymentPilotEnabled: cfg.AllowPersonalPaymentPilot,
		CaptchaEnabled:              cfg.CaptchaEnabled,
		CaptchaProvider:             cfg.CaptchaProvider,
		CaptchaSiteKey:              cfg.CaptchaSiteKey,
		MFAMandatoryRoles:           cfg.MFARequiredRoles,
		PaginationDefaultLimit:      cfg.PaginationDefaultLimit,
		PaginationMaxLimit:          cfg.PaginationMaxLimit,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthH.HandleHealthz)
	mux.HandleFunc("GET /readyz", healthH.HandleReadyz)
	mux.HandleFunc("GET /api/v1/public/config", publicConfigH.HandleGetPublicConfig)

	corsCfg := middleware.DefaultCORSConfig()
	if len(cfg.AllowedOrigins) > 0 {
		corsCfg.AllowedOrigins = cfg.AllowedOrigins
	}
	var handler http.Handler = mux
	handler = middleware.BodyLimit(cfg.MaxBodyBytes)(handler)
	handler = middleware.CORS(corsCfg)(handler)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Recovery(handler)

	return &Server{
		cfg:         cfg,
		pool:        pool,
		keyProvider: keyProvider,
		router:      mux,
		httpServer: &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.Port),
			Handler:           handler,
			ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
			ReadTimeout:       cfg.HTTPReadTimeout,
			WriteTimeout:      cfg.HTTPWriteTimeout,
			IdleTimeout:       cfg.HTTPIdleTimeout,
		},
	}
}

func (s *Server) Router() http.Handler              { return s.httpServer.Handler }
func (s *Server) KeyProvider() security.KeyProvider { return s.keyProvider }
func (s *Server) Start() error {
	if err := s.cfg.Validate(); err != nil {
		return fmt.Errorf("gagal memulai server: konfigurasi keamanan tidak valid: %w", err)
	}
	return s.httpServer.ListenAndServe()
}
func (s *Server) Shutdown(ctx context.Context) error { return s.httpServer.Shutdown(ctx) }
