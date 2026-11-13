package api_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/auth"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
)

func TestConfigBinding_SecurityAndOperationalParametersFromAppConfig(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	// Seed tenant + anggota + admin MFA via setupAPITestEnv lalu bangun server custom dari config.AppConfig
	baseEnv := setupAPITestEnv(t)
	defer baseEnv.Pool.Close()

	altCfgMap := map[string]string{
		"APP_ENV":                  "test",
		"APP_TIMEZONE":             "Asia/Jakarta",
		"API_PORT":                 "18080",
		"API_HOST_PORT":            "18080",
		"METRICS_PORT":             "19100",
		"METRICS_HOST_PORT":        "19100",
		"DB_HOST":                  "127.0.0.1",
		"DB_PORT":                  "15433",
		"DB_NAME":                  "kopdes_alt_test",
		"DB_USER":                  "kopdes_app",
		"DB_PASSWORD":              "AltTestPass123!",
		"JWT_SECRET":               "alt-jwt-secret-key-32-bytes-minimum-ok!",
		"JWT_PREVIOUS_SECRET":      "prev-jwt-secret-key-32-bytes-minimum-ok",
		"JWT_ALGORITHM":            "HS512",
		"JWT_ISSUER":               "kopdes-alt-issuer",
		"JWT_ACCESS_TTL":           "3m",
		"JWT_REFRESH_TTL":          "48h",
		"BLIND_INDEX_SECRET":       "test-blind-index-secret-32-bytes!",
		"AES_MASTER_KEY":           "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
		"AES_KEY_VERSION":          "v2",
		"AES_KEY_ID":               "k9",
		"AES_PREVIOUS_KEYS":        "v1:k1=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"QRIS_SECRET":              "alt-qris-secret-32-bytes-minimum-ok!!",
		"METRICS_TOKEN":            "alt-metrics-token-32-bytes-minimum-ok!",
		"METRICS_PREVIOUS_TOKEN":   "old-metrics-token-32-bytes-minimum-ok!",
		"BCRYPT_COST":              "4",
		"MFA_ISSUER":               "Kopdes Alt Test",
		"MFA_MAX_ATTEMPTS":         "2",
		"MFA_SESSION_TTL":          "2m",
		"MFA_SETUP_TTL":            "6m",
		"LOGIN_MAX_ATTEMPTS":       "2",
		"LOGIN_LOCKOUT_DURATION":   "4m",
		"RATE_LIMIT_RPS":           "1",
		"RATE_LIMIT_BURST":         "2",
		"RATE_LIMIT_MAX_CLIENTS":   "500",
		"MAX_BODY_BYTES":           "512",
		"HTTP_READ_HEADER_TIMEOUT": "3s",
		"HTTP_READ_TIMEOUT":        "6s",
		"HTTP_WRITE_TIMEOUT":       "9s",
		"HTTP_IDLE_TIMEOUT":        "20s",
		"ALLOWED_ORIGINS":          "http://127.0.0.1:13000",
	}

	appCfg, err := config.Load(func(k string) (string, bool) {
		v, ok := altCfgMap[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("config.Load gagal: %v", err)
	}

	srvCfg := api.ConfigFromAppConfig(appCfg)
	srv := api.NewServer(baseEnv.Pool, srvCfg)

	// Verifikasi METRICS_PORT terpisah terekam
	if srv.MetricsAddr() != ":19100" {
		t.Fatalf("Expected dedicated metrics listener addr :19100, got %s", srv.MetricsAddr())
	}

	// Buat user anggota & user MFA aktif dengan email diketahui
	ctx := context.Background()
	conn, err := baseEnv.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire gagal: %v", err)
	}
	defer conn.Release()
	_, _ = conn.Exec(ctx, "RESET ROLE;")

	emailAnggota := fmt.Sprintf("cfgbind_%s@kopdes.id", uuid.New().String()[:8])
	emailHash := security.HashBlindIndex(emailAnggota, []byte(srvCfg.BlindIndexSecret))
	pwdHash, _ := security.HashPasswordWithCost("BindPass123!", srvCfg.BcryptCost)
	bindUserID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'Binding Test User');
	`, bindUserID, []byte("e"), emailHash, pwdHash)
	if err != nil {
		t.Fatalf("insert user gagal: %v", err)
	}
	var roleID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'anggota' LIMIT 1;").Scan(&roleID)
	_, _ = conn.Exec(ctx, "INSERT INTO user_roles (user_id, koperasi_id, role_id) VALUES ($1, $2, $3);", bindUserID, baseEnv.KoperasiID, roleID)

	// 1. Uji MAX_BODY_BYTES = 512 -> payload 700 byte harus 413
	oversized := strings.Repeat("a", 700)
	reqBig := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"`+oversized+`"}`))
	reqBig.Header.Set("Content-Type", "application/json")
	reqBig.RemoteAddr = "10.10.10.1:1234"
	recBig := httptest.NewRecorder()
	srv.Router().ServeHTTP(recBig, reqBig)
	if recBig.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("Expected 413 when exceeding MAX_BODY_BYTES=512, got %d", recBig.Code)
	}

	// 2. Uji JWT_ACCESS_TTL=3m -> expires_in = 180 & JWT HS512
	loginBody, _ := json.Marshal(map[string]interface{}{
		"email":       emailAnggota,
		"password":    "BindPass123!",
		"koperasi_id": baseEnv.KoperasiID,
	})
	reqLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	reqLogin.Header.Set("Content-Type", "application/json")
	reqLogin.RemoteAddr = "10.10.10.2:1234"
	recLogin := httptest.NewRecorder()
	srv.Router().ServeHTTP(recLogin, reqLogin)
	if recLogin.Code != http.StatusOK {
		t.Fatalf("Login gagal: %d %s", recLogin.Code, recLogin.Body.String())
	}
	var loginResp struct {
		Data struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recLogin.Body.Bytes(), &loginResp)
	if loginResp.Data.ExpiresIn != 180 {
		t.Fatalf("Expected expires_in=180 (3m) from JWT_ACCESS_TTL, got %d", loginResp.Data.ExpiresIn)
	}
	tmCheck, _ := auth.NewTokenManagerWithConfig(srvCfg.JWTSecret, "", "HS512", srvCfg.JWTIssuer)
	claims, err := tmCheck.ValidateToken(loginResp.Data.AccessToken)
	if err != nil {
		t.Fatalf("Token gagal diverifikasi dengan HS512 & JWT_SECRET alt: %v", err)
	}
	if ttlActual := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time); ttlActual != 3*time.Minute {
		t.Fatalf("Expected token exp-iat == 3m, got %v", ttlActual)
	}

	// 3. Uji MFA_ISSUER = "Kopdes Alt Test" pada /api/v1/auth/mfa/setup
	reqMFA := httptest.NewRequest(http.MethodPost, "/api/v1/auth/mfa/setup", nil)
	reqMFA.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	reqMFA.RemoteAddr = "10.10.10.3:1234"
	recMFA := httptest.NewRecorder()
	srv.Router().ServeHTTP(recMFA, reqMFA)
	if recMFA.Code != http.StatusOK {
		t.Fatalf("MFA setup gagal: %d %s", recMFA.Code, recMFA.Body.String())
	}
	if !strings.Contains(recMFA.Body.String(), "Kopdes%20Alt%20Test") && !strings.Contains(recMFA.Body.String(), "Kopdes+Alt+Test") {
		t.Fatalf("Expected MFA_ISSUER 'Kopdes Alt Test' in qr_uri, got %s", recMFA.Body.String())
	}

	// 4. Uji MFA_MAX_ATTEMPTS = 2
	totpSec, _ := security.GenerateTOTPSecret()
	encTotp, err := srv.KeyProvider().EncryptWithAAD([]byte(totpSec), []byte(fmt.Sprintf("users:totp:%s", bindUserID)))
	if err != nil {
		t.Fatalf("EncryptWithAAD totp gagal: %v", err)
	}
	_, _ = conn.Exec(ctx, "SELECT fn_set_user_mfa($1, true, $2, 0, NULL);", bindUserID, encTotp)
	reqLoginMFA := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	reqLoginMFA.Header.Set("Content-Type", "application/json")
	reqLoginMFA.RemoteAddr = "10.10.10.4:1234"
	recLoginMFA := httptest.NewRecorder()
	srv.Router().ServeHTTP(recLoginMFA, reqLoginMFA)
	var mfaChallenge struct {
		Data struct {
			MFASessionToken string `json:"mfa_session_token"`
			ExpiresIn       int64  `json:"expires_in"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recLoginMFA.Body.Bytes(), &mfaChallenge)
	if mfaChallenge.Data.ExpiresIn != 120 {
		t.Fatalf("Expected MFA session expires_in=120 (2m), got %d", mfaChallenge.Data.ExpiresIn)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		badMFA, _ := json.Marshal(map[string]string{
			"mfa_session_token": mfaChallenge.Data.MFASessionToken,
			"totp_code":         "000000",
		})
		reqV := httptest.NewRequest(http.MethodPost, "/api/v1/auth/mfa/verify", bytes.NewReader(badMFA))
		reqV.Header.Set("Content-Type", "application/json")
		reqV.RemoteAddr = fmt.Sprintf("10.10.10.%d:1234", 10+attempt)
		recV := httptest.NewRecorder()
		srv.Router().ServeHTTP(recV, reqV)
		if attempt == 2 && recV.Code != http.StatusTooManyRequests {
			t.Fatalf("Expected 429 MFA_SESSION_LOCKED on attempt 2 (MFA_MAX_ATTEMPTS=2), got %d (%s)", recV.Code, recV.Body.String())
		}
	}

	// 5. Uji LOGIN_MAX_ATTEMPTS = 2
	badLogin, _ := json.Marshal(map[string]interface{}{
		"email":       emailAnggota,
		"password":    "WrongPass!",
		"koperasi_id": baseEnv.KoperasiID,
	})
	for i := 1; i <= 3; i++ {
		reqL := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(badLogin))
		reqL.Header.Set("Content-Type", "application/json")
		reqL.RemoteAddr = "10.10.20.99:5555"
		recL := httptest.NewRecorder()
		srv.Router().ServeHTTP(recL, reqL)
		if i == 3 && recL.Code != http.StatusTooManyRequests {
			t.Fatalf("Expected 429 lockout on attempt 3 when LOGIN_MAX_ATTEMPTS=2, got %d", recL.Code)
		}
	}

	// 6. Uji RATE_LIMIT_BURST = 2 (IP baru -> 2 sukses/non-429, ke-3 harus 429)
	for i := 1; i <= 3; i++ {
		reqR := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		reqR.RemoteAddr = "10.99.99.99:4321"
		recR := httptest.NewRecorder()
		srv.Router().ServeHTTP(recR, reqR)
		if i <= 2 && recR.Code == http.StatusTooManyRequests {
			t.Fatalf("Unexpected 429 within burst=2 on request %d", i)
		}
		if i == 3 && recR.Code != http.StatusTooManyRequests {
			t.Fatalf("Expected 429 rate limit exceeded on request 3 when RATE_LIMIT_BURST=2, got %d", recR.Code)
		}
	}

	// 7. Uji rotasi JWT (JWT_PREVIOUS_SECRET) di endpoint /api/v1/auth/me
	prevTM, _ := auth.NewTokenManagerWithConfig(srvCfg.JWTPreviousSecret, "", "HS512", srvCfg.JWTIssuer)
	oldJWT, _ := prevTM.GenerateToken(bindUserID, baseEnv.KoperasiID, "anggota", false, 2*time.Minute)
	reqOldJWT := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	reqOldJWT.Header.Set("Authorization", "Bearer "+oldJWT)
	reqOldJWT.RemoteAddr = "10.20.30.1:1111"
	recOldJWT := httptest.NewRecorder()
	srv.Router().ServeHTTP(recOldJWT, reqOldJWT)
	if recOldJWT.Code != http.StatusOK {
		t.Fatalf("Expected JWT_PREVIOUS_SECRET token to be accepted during transition, got %d (%s)", recOldJWT.Code, recOldJWT.Body.String())
	}

	// 8. Uji rotasi METRICS_TOKEN (METRICS_PREVIOUS_TOKEN) pada router & dedicated metrics handler
	for idx, tok := range []string{srvCfg.MetricsToken, srvCfg.MetricsPreviousToken} {
		reqM := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		reqM.Header.Set("Authorization", "Bearer "+tok)
		reqM.RemoteAddr = fmt.Sprintf("10.20.40.%d:2222", idx+1)
		recM := httptest.NewRecorder()
		srv.MetricsRouter().ServeHTTP(recM, reqM)
		if recM.Code != http.StatusOK {
			t.Fatalf("Expected metrics token #%d to be accepted on dedicated MetricsRouter, got %d", idx+1, recM.Code)
		}
	}
	reqBadM := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reqBadM.Header.Set("Authorization", "Bearer wrong-metrics-token")
	reqBadM.RemoteAddr = "10.20.40.99:2222"
	recBadM := httptest.NewRecorder()
	srv.MetricsRouter().ServeHTTP(recBadM, reqBadM)
	if recBadM.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 for invalid metrics token, got %d", recBadM.Code)
	}

	// 9. Uji AES keyProvider pada server mampu dekripsi v1:k1 lama + enkripsi v2:k9 baru
	oldKeyBytes, _ := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	oldKP, _ := security.NewMasterKeyProvider("v1", "k1", oldKeyBytes)
	oldCT, _ := oldKP.EncryptWithAAD([]byte("3201010101010099"), []byte("test:nik:1"))
	pt, err := srv.KeyProvider().DecryptWithAAD(oldCT, []byte("test:nik:1"))
	if err != nil || string(pt) != "3201010101010099" {
		t.Fatalf("Server KeyProvider failed to decrypt legacy v1:k1 ciphertext during rotation: %v", err)
	}
	newCT, _ := srv.KeyProvider().EncryptWithAAD([]byte("3201010101010088"), []byte("test:nik:2"))
	if !strings.HasPrefix(string(newCT), "v2:k9:") {
		t.Fatalf("Expected server KeyProvider new ciphertext to use prefix v2:k9:, got %q", string(newCT[:10]))
	}
}
