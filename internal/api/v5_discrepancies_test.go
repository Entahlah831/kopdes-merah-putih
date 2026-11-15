package api_test

import (
	"bytes"
	"context"
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
)

func TestV5_Discrepancy01_SetupTokenReturnsMFAEnrollmentRequired(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	tm := auth.NewTokenManager(env.Config.JWTSecret, env.Config.JWTIssuer)
	setupTok, err := tm.GenerateToken(env.AdminUserID, env.KoperasiID, "mfa_setup", false, 10*time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+setupTok)
	rec := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Error.Code != "MFA_ENROLLMENT_REQUIRED" {
		t.Fatalf("Expected error.code MFA_ENROLLMENT_REQUIRED, got %q (body: %s)", body.Error.Code, rec.Body.String())
	}
}

func TestV5_Discrepancy02_ExpirePesananRejectsAnggota(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/toko/pesanan/%s/expire", uuid.New()), nil)
	req.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	rec := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden when anggota calls /expire, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestV5_Discrepancy03_RequestIDAndCORSPreflight(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	cfg := env.Config
	cfg.AllowedOrigins = []string{"http://localhost:8081"}
	srv := api.NewServer(env.Pool, cfg)

	// 1. Default X-Request-ID generated + Allow/Expose headers on allowed Origin
	reqOK := httptest.NewRequest(http.MethodOptions, "/api/v1/katalog/produk", nil)
	reqOK.Header.Set("Origin", "http://localhost:8081")
	reqOK.Header.Set("Access-Control-Request-Method", "GET")
	reqOK.Header.Set("X-Request-ID", "req-test-abc-123")
	recOK := httptest.NewRecorder()
	srv.Router().ServeHTTP(recOK, reqOK)

	if recOK.Code != http.StatusNoContent {
		t.Fatalf("Expected 204 for allowed preflight, got %d", recOK.Code)
	}
	if got := recOK.Header().Get("X-Request-ID"); got != "req-test-abc-123" {
		t.Fatalf("Expected echoed X-Request-ID req-test-abc-123, got %q", got)
	}
	expose := recOK.Header().Get("Access-Control-Expose-Headers")
	if !strings.Contains(expose, "X-Request-ID") || !strings.Contains(expose, "Retry-After") {
		t.Fatalf("Expected Access-Control-Expose-Headers to expose X-Request-ID and Retry-After, got %q", expose)
	}

	// 2. Disallowed origin OPTIONS returns 403 without Access-Control-Allow-Origin
	reqBad := httptest.NewRequest(http.MethodOptions, "/api/v1/katalog/produk", nil)
	reqBad.Header.Set("Origin", "http://evil.invalid")
	reqBad.Header.Set("Access-Control-Request-Method", "GET")
	recBad := httptest.NewRecorder()
	srv.Router().ServeHTTP(recBad, reqBad)

	if recBad.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for disallowed CORS preflight, got %d", recBad.Code)
	}
	if recBad.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("Disallowed origin must NOT receive Access-Control-Allow-Origin")
	}

	// 3. Auto-generated X-Request-ID when client omits it
	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recHealth := httptest.NewRecorder()
	srv.Router().ServeHTTP(recHealth, reqHealth)
	if strings.TrimSpace(recHealth.Header().Get("X-Request-ID")) == "" {
		t.Fatalf("Expected auto-generated X-Request-ID header on response")
	}
}

func TestV5_Discrepancy04_CreateJenisIuranIncludesKlasifikasiAkuntansi(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// Ensure admin exists in users table for created_by FK if needed
	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	conn.Release()

	payload := fmt.Sprintf(`{"kode":"DISC04-%s","nama":"Iuran Discrepancy Test","nominal_standar":25000}`, uuid.New().String()[:6])
	req := httptest.NewRequest(http.MethodPost, "/api/v1/iuran/jenis", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer "+env.AdminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got := fmt.Sprintf("%v", resp.Data["klasifikasi_akuntansi"]); got != "KEPUTUSAN AKUNTAN" {
		t.Fatalf("Expected data.klasifikasi_akuntansi == 'KEPUTUSAN AKUNTAN', got %q (body: %s)", got, rec.Body.String())
	}
}

func TestV5_Discrepancy15_AntreanVerifikasiHandlesNullPembayaranBukti(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	billID := env.CreateBill(t, env.AnggotaID, 50000)

	qrisPayload := fmt.Sprintf(
		`{"tagihan_iuran_ids":[%q],"jenis_transaksi":"iuran_bulanan","anggota_id":%q,"rekening_qris_id":%q}`,
		billID.String(), env.AnggotaID.String(), env.RekeningID.String(),
	)
	reqQRIS := httptest.NewRequest(http.MethodPost, "/api/v1/pembayaran/qris", strings.NewReader(qrisPayload))
	reqQRIS.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqQRIS.Header.Set("Content-Type", "application/json")
	recQRIS := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recQRIS, reqQRIS)
	if recQRIS.Code != http.StatusCreated {
		t.Fatalf("Expected 201 for POST /pembayaran/qris, got %d (%s)", recQRIS.Code, recQRIS.Body.String())
	}

	var qrisResp struct {
		Data struct {
			PembayaranID string `json:"pembayaran_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recQRIS.Body.Bytes(), &qrisResp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	// Klaim RRN tanpa mengunggah baris pembayaran_bukti memicu status menunggu_verifikasi dengan pb.* = NULL
	klaimReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/pembayaran/"+qrisResp.Data.PembayaranID+"/bukti",
		strings.NewReader(`{"referensi_rrn":"RRN-DISC15","catatan":"tanpa baris pembayaran_bukti"}`),
	)
	klaimReq.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	klaimReq.Header.Set("Content-Type", "application/json")
	klaimRec := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(klaimRec, klaimReq)
	if klaimRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for POST /pembayaran/{id}/bukti, got %d (%s)", klaimRec.Code, klaimRec.Body.String())
	}

	antreanReq := httptest.NewRequest(http.MethodGet, "/api/v1/pembayaran/antrean", nil)
	antreanReq.Header.Set("Authorization", "Bearer "+env.AdminToken)
	antreanRec := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(antreanRec, antreanReq)
	if antreanRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for GET /api/v1/pembayaran/antrean, got %d (%s)", antreanRec.Code, antreanRec.Body.String())
	}
}
