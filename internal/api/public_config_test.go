package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kopdes/merah-putih/internal/api"
)

func TestPublicConfig_ReturnsSafeMetadataAndNoSecrets(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	cfg := env.Config
	cfg.APIVersion = "1.1.0"
	cfg.AppTimezone = "Asia/Jakarta"
	cfg.MaxBodyBytes = 5242880
	cfg.MaxKYCUploadBytes = 2097152
	cfg.PaymentExpiryDuration = 10 * time.Minute
	cfg.PollingIntervalMS = 2500
	cfg.PaginationDefaultLimit = 25
	cfg.PaginationMaxLimit = 80
	cfg.QRISDynamicEnabled = false
	cfg.AllowPersonalPaymentPilot = false
	cfg.CaptchaEnabled = false
	cfg.CaptchaProvider = "turnstile"
	cfg.CaptchaSiteKey = "should-not-expose-when-disabled"

	srv := api.NewServer(env.Pool, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/config", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from GET /api/v1/public/config, got %d: %s", rec.Code, rec.Body.String())
	}

	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "max-age=") {
		t.Fatalf("Expected short Cache-Control header, got %q", cc)
	}

	raw := rec.Body.String()
	for _, secret := range []string{cfg.JWTSecret, cfg.AESMasterKey, cfg.BlindIndexSecret, cfg.QRISSecret, cfg.MetricsToken} {
		if secret != "" && strings.Contains(raw, secret) {
			t.Fatalf("SECURITY VIOLATION: secret value leaked in GET /api/v1/public/config")
		}
	}
	if strings.Contains(raw, "should-not-expose-when-disabled") {
		t.Fatalf("Expected captcha site_key to be empty when CAPTCHA_ENABLED=false, got body: %s", raw)
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			APIVersion string `json:"api_version"`
			ServerTime string `json:"server_time"`
			ServerUnix int64  `json:"server_unix_ms"`
			Timezone   string `json:"timezone"`
			Upload     struct {
				MaxBodyBytes      int64    `json:"max_body_bytes"`
				MaxKYCUploadBytes int64    `json:"max_kyc_upload_bytes"`
				AllowedMIMETypes  []string `json:"allowed_mime_types"`
				AllowedCSVMIME    []string `json:"allowed_csv_mime_types"`
			} `json:"upload_limits"`
			KYCRequired         []string `json:"kyc_required_document_types"`
			KYCSupported        []string `json:"kyc_supported_document_types"`
			PaymentExpirySecs   int64    `json:"payment_expiry_seconds"`
			PollingIntervalMS   int      `json:"polling_interval_ms"`
			Features            struct {
				PaymentsEnabled             bool `json:"payments_enabled"`
				RegistrationEnabled         bool `json:"registration_enabled"`
				LoginEnabled                bool `json:"login_enabled"`
				ReadOnlyMode                bool `json:"read_only_mode"`
				QRISDynamicEnabled          bool `json:"qris_dynamic_enabled"`
				PersonalPaymentPilotEnabled bool `json:"personal_payment_pilot_enabled"`
			} `json:"features"`
			Captcha struct {
				Enabled  bool   `json:"enabled"`
				Provider string `json:"provider"`
				SiteKey  string `json:"site_key"`
			} `json:"captcha"`
			MFAMandatoryRoles     []string `json:"mfa_mandatory_roles"`
			AccessTokenTTLSeconds int64    `json:"access_token_ttl_seconds"`
			Pagination            struct {
				DefaultLimit int `json:"default_limit"`
				MaxLimit     int `json:"max_limit"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v (%s)", err, raw)
	}

	d := resp.Data
	if !resp.Success || d.APIVersion != "1.1.0" || d.Timezone != "Asia/Jakarta" || d.ServerUnix <= 0 {
		t.Fatalf("Unexpected metadata in public config: %+v", d)
	}
	if _, err := time.Parse(time.RFC3339, d.ServerTime); err != nil {
		t.Fatalf("Expected server_time RFC3339, got %q: %v", d.ServerTime, err)
	}
	if d.Upload.MaxKYCUploadBytes != 2097152 || len(d.Upload.AllowedMIMETypes) < 3 || len(d.Upload.AllowedCSVMIME) == 0 {
		t.Fatalf("Unexpected upload_limits: %+v", d.Upload)
	}
	if len(d.KYCRequired) == 0 || d.KYCRequired[0] != "ktp" || len(d.KYCSupported) < 4 {
		t.Fatalf("Unexpected KYC document metadata: req=%v sup=%v", d.KYCRequired, d.KYCSupported)
	}
	if d.PaymentExpirySecs != 600 || d.PollingIntervalMS != 2500 {
		t.Fatalf("Unexpected payment/polling config: expiry=%d polling=%d", d.PaymentExpirySecs, d.PollingIntervalMS)
	}
	if !d.Features.PaymentsEnabled || !d.Features.RegistrationEnabled || !d.Features.LoginEnabled || d.Features.ReadOnlyMode {
		t.Fatalf("Unexpected feature flags: %+v", d.Features)
	}
	if d.Captcha.Enabled || d.Captcha.SiteKey != "" {
		t.Fatalf("Expected captcha disabled with empty site_key, got %+v", d.Captcha)
	}
	if len(d.MFAMandatoryRoles) != 3 || d.AccessTokenTTLSeconds <= 0 {
		t.Fatalf("Unexpected mfa_mandatory_roles / access_token_ttl_seconds: %+v", d)
	}
	if d.Pagination.DefaultLimit != 25 || d.Pagination.MaxLimit != 80 {
		t.Fatalf("Unexpected pagination: %+v", d.Pagination)
	}

	// Verify site_key IS exposed when CaptchaEnabled=true
	cfg.CaptchaEnabled = true
	cfg.CaptchaSiteKey = "0x4AAAAAAA_synthetic_site_key"
	srvCaptcha := api.NewServer(env.Pool, cfg)
	recCap := httptest.NewRecorder()
	srvCaptcha.Router().ServeHTTP(recCap, httptest.NewRequest(http.MethodGet, "/api/v1/public/config", nil))
	if recCap.Code != http.StatusOK || !strings.Contains(recCap.Body.String(), "0x4AAAAAAA_synthetic_site_key") {
		t.Fatalf("Expected site_key when CaptchaEnabled=true, got %d %s", recCap.Code, recCap.Body.String())
	}
}

func TestPublicConfig_FeatureKillSwitchesAndReadOnlyMode(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// 1. ReadOnlyMode blocks mutations with 503 READ_ONLY_MODE while /public/config stays 200
	cfgRO := env.Config
	cfgRO.ReadOnlyMode = true
	srvRO := api.NewServer(env.Pool, cfgRO)

	recPub := httptest.NewRecorder()
	srvRO.Router().ServeHTTP(recPub, httptest.NewRequest(http.MethodGet, "/api/v1/public/config", nil))
	if recPub.Code != http.StatusOK || !strings.Contains(recPub.Body.String(), `"read_only_mode":true`) {
		t.Fatalf("Expected 200 and read_only_mode:true, got %d %s", recPub.Code, recPub.Body.String())
	}

	reqCart := httptest.NewRequest(http.MethodPost, "/api/v1/toko/keranjang", strings.NewReader(`{}`))
	reqCart.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqCart.Header.Set("Content-Type", "application/json")
	recCart := httptest.NewRecorder()
	srvRO.Router().ServeHTTP(recCart, reqCart)
	if recCart.Code != http.StatusServiceUnavailable || !strings.Contains(recCart.Body.String(), `"READ_ONLY_MODE"`) {
		t.Fatalf("Expected 503 READ_ONLY_MODE on mutation, got %d %s", recCart.Code, recCart.Body.String())
	}

	// 2. PaymentsDisabled blocks POST /api/v1/pembayaran/qris with 503 FEATURE_DISABLED
	cfgPay := env.Config
	cfgPay.PaymentsDisabled = true
	srvPay := api.NewServer(env.Pool, cfgPay)
	reqPay := httptest.NewRequest(http.MethodPost, "/api/v1/pembayaran/qris", strings.NewReader(`{}`))
	reqPay.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqPay.Header.Set("Content-Type", "application/json")
	recPay := httptest.NewRecorder()
	srvPay.Router().ServeHTTP(recPay, reqPay)
	if recPay.Code != http.StatusServiceUnavailable || !strings.Contains(recPay.Body.String(), `"FEATURE_DISABLED"`) {
		t.Fatalf("Expected 503 FEATURE_DISABLED when PaymentsDisabled=true, got %d %s", recPay.Code, recPay.Body.String())
	}

	// 3. RegistrationDisabled blocks POST /api/v1/pendaftaran with 503 FEATURE_DISABLED
	cfgReg := env.Config
	cfgReg.RegistrationDisabled = true
	srvReg := api.NewServer(env.Pool, cfgReg)
	reqReg := httptest.NewRequest(http.MethodPost, "/api/v1/pendaftaran", strings.NewReader(`{}`))
	reqReg.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqReg.Header.Set("Content-Type", "application/json")
	recReg := httptest.NewRecorder()
	srvReg.Router().ServeHTTP(recReg, reqReg)
	if recReg.Code != http.StatusServiceUnavailable || !strings.Contains(recReg.Body.String(), `"FEATURE_DISABLED"`) {
		t.Fatalf("Expected 503 FEATURE_DISABLED when RegistrationDisabled=true, got %d %s", recReg.Code, recReg.Body.String())
	}

	// 4. LoginDisabled blocks POST /api/v1/auth/login with 503 FEATURE_DISABLED
	cfgLogin := env.Config
	cfgLogin.LoginDisabled = true
	srvLogin := api.NewServer(env.Pool, cfgLogin)
	reqLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	reqLogin.Header.Set("Content-Type", "application/json")
	recLogin := httptest.NewRecorder()
	srvLogin.Router().ServeHTTP(recLogin, reqLogin)
	if recLogin.Code != http.StatusServiceUnavailable || !strings.Contains(recLogin.Body.String(), `"FEATURE_DISABLED"`) {
		t.Fatalf("Expected 503 FEATURE_DISABLED when LoginDisabled=true, got %d %s", recLogin.Code, recLogin.Body.String())
	}
}
