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
	APIVersion                   string
	PaymentsDisabled             bool
	RegistrationDisabled         bool
	LoginDisabled                bool
	ReadOnlyMode                 bool
	CaptchaEnabled               bool
	CaptchaProvider              string
	CaptchaSiteKey               string
	PaymentExpiryDuration        time.Duration
	PollingIntervalMS            int
	PaginationDefaultLimit       int
	PaginationMaxLimit           int
}

func decodeSecretBytes(secret string) []byte {
	return security.DecodeSecretMaterial(secret)
}

func ConfigFromAppConfig(c config.AppConfig) Config {
	return Config{
		AppEnv:                    c.AppEnv,
		AppTimezone:               c.AppTimezone,
		Port:                      c.APIPort,
		BlindIndexSecret:          c.BlindIndexSecret,
		AESMasterKey:              c.AESMasterKey,
		AESKeyVersion:             c.AESKeyVersion,
		AESKeyID:                  c.AESKeyID,
		AESPreviousKeys:           c.AESPreviousKeys,
		AllowedOrigins:            c.AllowedOrigins,
		TrustedProxies:            c.TrustedProxies,
		MaxBodyBytes:              c.MaxBodyBytes,
		HTTPReadHeaderTimeout:     c.HTTPReadHeaderTimeout,
		HTTPReadTimeout:           c.HTTPReadTimeout,
		HTTPWriteTimeout:          c.HTTPWriteTimeout,
		HTTPIdleTimeout:           c.HTTPIdleTimeout,
		BcryptCost:                c.BcryptCost,
		MFAIssuer:                 c.MFAIssuer,
		MFAMaxAttempts:            c.MFAMaxAttempts,
		MFASessionTTL:             c.MFASessionTTL,
		MFASetupTTL:               c.MFASetupTTL,
		LoginMaxAttempts:          c.LoginMaxAttempts,
		LoginLockoutDuration:      c.LoginLockoutDuration,
		AllowPersonalPaymentPilot: c.AllowPersonalPaymentPilot,
		PilotDefaultMaxPerTx:      c.PilotDefaultMaxPerTx,
		PilotDefaultMaxPerDay:     c.PilotDefaultMaxPerDay,
		StorageKYCDir:             c.StorageKYCDir,
		MaxKYCUploadBytes:         c.MaxKYCUploadBytes,
		APIVersion:                c.APIVersion,
		PaymentsDisabled:          !c.PaymentsEnabled,
		RegistrationDisabled:      !c.RegistrationEnabled,
		LoginDisabled:             !c.LoginEnabled,
		ReadOnlyMode:              c.ReadOnlyMode,
		CaptchaEnabled:            c.CaptchaEnabled,
		CaptchaProvider:           c.CaptchaProvider,
		CaptchaSiteKey:            c.CaptchaSiteKey,
		PaymentExpiryDuration:     c.PaymentExpiryDuration,
		PollingIntervalMS:         c.PollingIntervalMS,
		PaginationDefaultLimit:    c.PaginationDefaultLimit,
		PaginationMaxLimit:        c.PaginationMaxLimit,
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
		AppEnv:                 "development",
		AppTimezone:            "Asia/Jakarta",
		Port:                   8080,
		AESKeyVersion:          "v1",
		AESKeyID:               "k1",
		AllowedOrigins:         []string{"*"},
		MaxBodyBytes:           10 * 1024 * 1024,
		HTTPReadHeaderTimeout:  5 * time.Second,
		HTTPReadTimeout:        10 * time.Second,
		HTTPWriteTimeout:       15 * time.Second,
		HTTPIdleTimeout:        60 * time.Second,
		BcryptCost:             12,
		MFAIssuer:              "Kopdes Merah Putih",
		MFAMaxAttempts:         3,
		MFASessionTTL:          5 * time.Minute,
		MFASetupTTL:            10 * time.Minute,
		LoginMaxAttempts:       5,
		LoginLockoutDuration:   15 * time.Minute,
		PilotDefaultMaxPerTx:   500000,
		PilotDefaultMaxPerDay:  2000000,
		StorageKYCDir:          "./storage/kyc",
		MaxKYCUploadBytes:      5 * 1024 * 1024,
		APIVersion:             "1.1.0",
		PaymentExpiryDuration:  15 * time.Minute,
		PollingIntervalMS:      3000,
		PaginationDefaultLimit: 20,
		PaginationMaxLimit:     100,
		CaptchaProvider:        "turnstile",
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

	var keyProvider security.KeyProvider
	if strings.TrimSpace(cfg.AESMasterKey) != "" {
		if mkp, err := security.NewMultiKeyProviderFromConfig(cfg.AESKeyVersion, cfg.AESKeyID, cfg.AESMasterKey, cfg.AESPreviousKeys); err == nil {
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
		PaymentsEnabled:             !cfg.PaymentsDisabled,
		RegistrationEnabled:         !cfg.RegistrationDisabled,
		LoginEnabled:                !cfg.LoginDisabled,
		ReadOnlyMode:                cfg.ReadOnlyMode,
		PersonalPaymentPilotEnabled: cfg.AllowPersonalPaymentPilot,
		CaptchaEnabled:              cfg.CaptchaEnabled,
		CaptchaProvider:             cfg.CaptchaProvider,
		CaptchaSiteKey:              cfg.CaptchaSiteKey,
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
