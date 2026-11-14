package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
)

func TestUITester_RefusesProductionMode(t *testing.T) {
	cfg := config.AppConfig{
		AppEnv:       "production",
		UIPort:       8081,
		UIBind:       "127.0.0.1",
		APIPublicURL: "https://api.kopdes.example.id",
	}
	_, err := newUITesterHandler(cfg, nil, t.TempDir())
	if err == nil {
		t.Fatalf("Expected uitester to refuse APP_ENV=production, got nil error")
	}
}

func TestUITester_ConfigJSON_ProbeCORS_And_TOTPHelper(t *testing.T) {
	uiDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(uiDir, "index.html"), []byte("<html>tester</html>"), 0o600); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	cfg := config.AppConfig{
		AppEnv:           "test",
		UIPort:           18081,
		UIBind:           "127.0.0.1",
		APIPublicURL:     "http://127.0.0.1:18080",
		DBName:           "kopdes_alt_test",
		DBPassword:       "super_secret_db_pwd",
		JWTSecret:        strings.Repeat("x", 32),
		TestSeedPassword: "super_secret_seed_pwd",
	}

	handler, err := newUITesterHandler(cfg, nil, uiDir)
	if err != nil {
		t.Fatalf("newUITesterHandler unexpected error: %v", err)
	}

	// 1. /config.json exposes public URL + seeded manifest without secrets/passwords
	reqCfg := httptest.NewRequest(http.MethodGet, "/config.json", nil)
	recCfg := httptest.NewRecorder()
	handler.ServeHTTP(recCfg, reqCfg)
	if recCfg.Code != http.StatusOK {
		t.Fatalf("Expected /config.json 200, got %d: %s", recCfg.Code, recCfg.Body.String())
	}
	rawBody := recCfg.Body.String()
	for _, forbidden := range []string{"super_secret_db_pwd", "super_secret_seed_pwd", cfg.JWTSecret, "password_hash", "totp_secret"} {
		if strings.Contains(rawBody, forbidden) {
			t.Fatalf("/config.json leaked sensitive string %q", forbidden)
		}
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(recCfg.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("Invalid /config.json JSON: %v", err)
	}
	if parsed["api_public_url"] != "http://127.0.0.1:18080" || parsed["app_env"] != "test" {
		t.Fatalf("Unexpected /config.json metadata: %v", parsed)
	}

	// 2. /api/totp-helper generates valid 6-digit RFC6238 code for a given Base32 secret
	secret, err := security.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	totpPayload, _ := json.Marshal(map[string]string{"secret_base32": secret})
	reqTotp := httptest.NewRequest(http.MethodPost, "/api/totp-helper", bytes.NewReader(totpPayload))
	reqTotp.Header.Set("Content-Type", "application/json")
	recTotp := httptest.NewRecorder()
	handler.ServeHTTP(recTotp, reqTotp)
	if recTotp.Code != http.StatusOK {
		t.Fatalf("Expected /api/totp-helper 200, got %d: %s", recTotp.Code, recTotp.Body.String())
	}
	var totpRes struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recTotp.Body.Bytes(), &totpRes); err != nil {
		t.Fatalf("decode totp response: %v", err)
	}
	if !security.ValidateTOTPCode(secret, totpRes.Code, time.Now()) {
		t.Fatalf("Generated TOTP code %q failed validation", totpRes.Code)
	}

	// 3. /api/probe-cors probes CORS target preflight
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions && r.Header.Get("Origin") == "https://evil.example" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer targetSrv.Close()

	probePayload, _ := json.Marshal(map[string]string{
		"target_url": targetSrv.URL + "/api/v1/healthz",
		"origin":     "https://evil.example",
		"method":     "GET",
	})
	reqProbe := httptest.NewRequest(http.MethodPost, "/api/probe-cors", bytes.NewReader(probePayload))
	reqProbe.Header.Set("Content-Type", "application/json")
	recProbe := httptest.NewRecorder()
	handler.ServeHTTP(recProbe, reqProbe)
	if recProbe.Code != http.StatusOK {
		t.Fatalf("Expected /api/probe-cors 200, got %d: %s", recProbe.Code, recProbe.Body.String())
	}
	var probeRes struct {
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(recProbe.Body.Bytes(), &probeRes); err != nil {
		t.Fatalf("decode probe response: %v", err)
	}
	if probeRes.StatusCode != http.StatusForbidden {
		t.Fatalf("Expected upstream CORS status 403, got %d", probeRes.StatusCode)
	}
}
