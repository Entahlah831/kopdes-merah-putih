package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler_HandleHealthz(t *testing.T) {
	h := NewHealthHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	h.HandleHealthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, ingin 200", rec.Code)
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("gagal parse JSON healthz: %v", err)
	}
	if !body.Success || body.Data.Status != "UP" {
		t.Fatalf("body=%+v, ingin success=true status=UP", body)
	}
}

func TestMatchMetricsTokenConstantTime(t *testing.T) {
	if matchMetricsTokenConstantTime("", "abc") || matchMetricsTokenConstantTime("abc", "") {
		t.Fatalf("token kosong harus selalu false")
	}
	if matchMetricsTokenConstantTime("token-aktif-32-byte-minimum-uji", "salah") {
		t.Fatalf("token berbeda harus false")
	}
	if !matchMetricsTokenConstantTime("token-aktif-32-byte-minimum-uji", "token-aktif-32-byte-minimum-uji") {
		t.Fatalf("token identik harus true")
	}
}

func TestMetricsHandler_RejectsUnauthorizedEvenFromPrivateIP(t *testing.T) {
	h := NewMetricsHandlerWithRotation(nil, "aktif-0123456789abcdef0123456789abcdef", "lama-0123456789abcdef0123456789abcdef", nil)

	for _, hdrVal := range []string{"", "Bearer salah", "Token biasa"} {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.RemoteAddr = "10.0.0.5:54321"
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		if hdrVal != "" {
			req.Header.Set("Authorization", hdrVal)
		}
		rec := httptest.NewRecorder()
		h.HandleMetrics(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("header %q harus ditolak 401, dapat %d", hdrVal, rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("WWW-Authenticate header wajib ada pada respons 401 /metrics")
		}
	}
}
