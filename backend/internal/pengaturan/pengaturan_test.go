package pengaturan

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	pengapi "github.com/kopdes/merah-putih/internal/pengaturan/api"
)

var (
	_ pengapi.SettingsReader = (*Service)(nil)
	_ pengapi.AuditWriter    = (*Service)(nil)
)

func TestUpdatePengaturanValidation(t *testing.T) {
	svc := NewService(nil, nil)
	claims := &middleware.SessionClaims{UserID: uuid.New(), Role: "admin"}

	cases := []struct {
		name string
		in   UpdateInput
		code string
	}{
		{"empty nama", UpdateInput{}, "INVALID_NAMA_KOPERASI"},
		{"invalid qris digits", UpdateInput{NamaKoperasi: "Kopdes", QRISUniqueDigits: 5}, "INVALID_QRIS_DIGITS"},
		{"negative ongkir", UpdateInput{NamaKoperasi: "Kopdes", QRISUniqueDigits: 3, OngkirTetap: -1}, "INVALID_ONGKIR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.UpdatePengaturan(context.Background(), claims, tc.in, "127.0.0.1")
			var de *response.DomainError
			if !errors.As(err, &de) || de.Status != http.StatusBadRequest || de.Code != tc.code {
				t.Fatalf("expected 400 %s, got %v", tc.code, err)
			}
		})
	}
}

func TestListAuditLog_NilPoolAndMapping(t *testing.T) {
	svc := NewService(nil, nil)
	if _, err := svc.ListAuditLog(context.Background(), "", "", 20, 0); err == nil {
		t.Fatal("expected error on nil pool")
	}

	if testing.Short() {
		return
	}
	envPath := filepath.Clean(filepath.Join("..", "..", "..", ".env.test"))
	if _, err := os.Stat(envPath); err != nil {
		t.Skipf("env test tidak ditemukan: %v", err)
	}
	cfg, err := config.LoadFromEnvAndFile(envPath)
	if err != nil {
		t.Fatalf("LoadFromEnvAndFile: %v", err)
	}
	dsn, err := cfg.BuildDatabaseDSN()
	if err != nil {
		t.Fatalf("BuildDatabaseDSN: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	dbSvc := NewService(pool, nil)
	markerID := "audit-test-" + uuid.NewString()
	if err := dbSvc.Log(ctx, pengapi.AuditEntry{
		ActorPeran: "admin", Aksi: "test_audit_list", Entitas: "pengaturan",
		EntitasID: markerID, Ringkasan: "Uji GET audit log", IPAddress: "127.0.0.1",
	}); err != nil {
		t.Fatalf("Log failed: %v", err)
	}
	h := NewHandler(dbSvc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-log?entitas=pengaturan&aksi=test_audit_list&limit=5&offset=0", nil)
	rec := httptest.NewRecorder()
	h.HandleListAuditLog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HandleListAuditLog status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Success bool          `json:"success"`
		Data    []AuditLogDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !env.Success || len(env.Data) == 0 || env.Data[0].EntitasID != markerID {
		t.Fatalf("unexpected audit log result: %+v", env)
	}
	_ = db.CheckDatabaseRole
}
