package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/kopdes/merah-putih/internal/api/response"
	bayarapi "github.com/kopdes/merah-putih/internal/pembayaran/api"
	pengapi "github.com/kopdes/merah-putih/internal/pengaturan/api"
)

// PublicConfigOptions berisi metadata non-rahasia untuk konfigurasi runtime klien frontend.
type PublicConfigOptions struct {
	Settings                    pengapi.SettingsReader
	QRIS                        bayarapi.ActiveQRISReader
	APIVersion                  string
	Timezone                    string
	MaxBodyBytes                int64
	MaxKYCUploadBytes           int64
	PaymentExpirySeconds        int64
	PollingIntervalMS           int
	AccessTokenTTLSeconds       int64
	CookieSecure                string
	AllowBearerAuth             bool
	StockHoldMinutes            int
	PaymentsEnabled             bool
	RegistrationEnabled         bool
	LoginEnabled                bool
	ReadOnlyMode                bool
	QRISDynamicEnabled          bool
	PersonalPaymentPilotEnabled bool
	CaptchaEnabled              bool
	CaptchaProvider             string
	CaptchaSiteKey              string
	MFAMandatoryRoles           []string
	PaginationDefaultLimit      int
	PaginationMaxLimit          int
	NowFunc                     func() time.Time
}

// PublicConfigHandler menyajikan konfigurasi publik aman bagi aplikasi frontend.
type PublicConfigHandler struct {
	opts PublicConfigOptions
}

func NewPublicConfigHandler(opts PublicConfigOptions) *PublicConfigHandler {
	applyPublicConfigDefaults(&opts)
	return &PublicConfigHandler{opts: opts}
}

func applyPublicConfigDefaults(opts *PublicConfigOptions) {
	if strings.TrimSpace(opts.APIVersion) == "" {
		opts.APIVersion = "1.1.0"
	}
	if strings.TrimSpace(opts.Timezone) == "" {
		opts.Timezone = "Asia/Jakarta"
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 10 * 1024 * 1024
	}
	if opts.MaxKYCUploadBytes <= 0 {
		opts.MaxKYCUploadBytes = 5 * 1024 * 1024
	}
	if opts.PaymentExpirySeconds <= 0 {
		opts.PaymentExpirySeconds = 900
	}
	if opts.PollingIntervalMS <= 0 {
		opts.PollingIntervalMS = 3000
	}
	if opts.AccessTokenTTLSeconds <= 0 {
		opts.AccessTokenTTLSeconds = 900
	}
	if opts.StockHoldMinutes <= 0 {
		opts.StockHoldMinutes = 60
	}
	if strings.TrimSpace(opts.CaptchaProvider) == "" {
		opts.CaptchaProvider = "turnstile"
	}
	if !opts.CaptchaEnabled {
		opts.CaptchaSiteKey = ""
	}
	if opts.PaginationDefaultLimit <= 0 {
		opts.PaginationDefaultLimit = 20
	}
	if opts.PaginationMaxLimit < opts.PaginationDefaultLimit {
		opts.PaginationMaxLimit = 100
	}
	if len(opts.MFAMandatoryRoles) == 0 {
		opts.MFAMandatoryRoles = []string{"admin", "bendahara"}
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
}

// HandleGetPublicConfig mengembalikan konfigurasi publik non-rahasia tanpa autentikasi.
func (h *PublicConfigHandler) HandleGetPublicConfig(w http.ResponseWriter, r *http.Request) {
	payload := h.buildPublicPayload(r)
	w.Header().Set("Cache-Control", "public, max-age=60")
	response.JSON(w, http.StatusOK, payload)
}

func (h *PublicConfigHandler) buildPublicPayload(r *http.Request) map[string]interface{} {
	now := h.opts.NowFunc().UTC()
	if loc, err := time.LoadLocation(h.opts.Timezone); err == nil {
		now = now.In(loc)
	}
	siteKey := ""
	if h.opts.CaptchaEnabled {
		siteKey = strings.TrimSpace(h.opts.CaptchaSiteKey)
	}
	payload := map[string]interface{}{
		"api_version":    h.opts.APIVersion,
		"server_time":    now.Format(time.RFC3339),
		"server_unix_ms": now.UnixMilli(),
		"timezone":       h.opts.Timezone,
		"upload_limits": map[string]interface{}{
			"max_body_bytes":         h.opts.MaxBodyBytes,
			"max_kyc_upload_bytes":   h.opts.MaxKYCUploadBytes,
			"allowed_mime_types":     []string{"image/jpeg", "image/png"},
			"allowed_csv_mime_types": []string{"text/csv", "application/csv", "text/plain"},
		},
		"kyc_required_document_types":  []string{"ktp"},
		"kyc_supported_document_types": []string{"ktp", "kk", "selfie_ktp"},
		"payment_expiry_seconds":       h.opts.PaymentExpirySeconds,
		"polling_interval_ms":          h.opts.PollingIntervalMS,
		"features": map[string]interface{}{
			"payments_enabled":               h.opts.PaymentsEnabled,
			"registration_enabled":           h.opts.RegistrationEnabled,
			"login_enabled":                  h.opts.LoginEnabled,
			"read_only_mode":                 h.opts.ReadOnlyMode,
			"qris_dynamic_enabled":           h.opts.QRISDynamicEnabled,
			"personal_payment_pilot_enabled": h.opts.PersonalPaymentPilotEnabled,
		},
		"captcha": map[string]interface{}{
			"enabled":  h.opts.CaptchaEnabled,
			"provider": h.opts.CaptchaProvider,
			"site_key": siteKey,
		},
		"mfa_mandatory_roles":      h.opts.MFAMandatoryRoles,
		"access_token_ttl_seconds": h.opts.AccessTokenTTLSeconds,
		"session_idle_ttl_seconds": h.opts.AccessTokenTTLSeconds,
		"stock_hold_minutes":       h.opts.StockHoldMinutes,
		"auth": map[string]interface{}{
			"cookie_primary": true,
			"bearer_enabled": h.opts.AllowBearerAuth,
			"csrf_header":    "X-Requested-With",
		},
		"pagination": map[string]interface{}{
			"default_limit": h.opts.PaginationDefaultLimit,
			"max_limit":     h.opts.PaginationMaxLimit,
		},
	}
	h.attachKoperasiInfo(r, payload)
	return payload
}

func (h *PublicConfigHandler) attachKoperasiInfo(r *http.Request, payload map[string]interface{}) {
	if h.opts.Settings == nil {
		return
	}
	pub, err := h.opts.Settings.GetSettings(r.Context())
	if err != nil {
		return
	}
	var qris bayarapi.PublicQRISInfo
	if h.opts.QRIS != nil && pub.ActiveRekeningQRISID != nil {
		if qInfo, qErr := h.opts.QRIS.GetPublicQRISInfo(r.Context(), pub.ActiveRekeningQRISID); qErr == nil {
			qris = qInfo
		}
	}
	payload["koperasi"] = map[string]interface{}{
		"nama_koperasi":      pub.NamaKoperasi,
		"alamat_koperasi":    pub.AlamatKoperasi,
		"telepon_koperasi":   pub.TeleponKoperasi,
		"qris_unique_digits": pub.QRISUniqueDigits,
		"ongkir_tetap":       pub.OngkirTetap,
		"kebijakan_teks":     pub.KebijakanTeks,
		"active_qris": map[string]interface{}{
			"rekening_id":    qris.RekeningID,
			"nama_merchant":  qris.NamaMerchant,
			"nmid":           qris.NMID,
			"nama_bank":      qris.NamaBank,
			"jenis_pemilik":  qris.JenisPemilik,
			"qris_image_url": qris.QRISImageURL,
		},
	}
}
