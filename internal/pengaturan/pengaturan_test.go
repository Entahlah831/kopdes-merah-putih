package pengaturan

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
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
