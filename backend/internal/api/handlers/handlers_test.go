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
