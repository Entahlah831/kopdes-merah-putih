package iuran

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
)

func TestParsePeriode(t *testing.T) {
	th, bl, err := ParsePeriode("2026-11")
	if err != nil || th != 2026 || bl != 11 {
		t.Fatalf("ParsePeriode valid got (%d,%d,%v)", th, bl, err)
	}
	for _, bad := range []string{"", "2026/11", "2019-01", "2026-13", "abcd-01"} {
		if _, _, err := ParsePeriode(bad); err == nil {
			t.Fatalf("expected error for invalid periode %q", bad)
		}
	}
}

func TestApplyKeringananInputValidation(t *testing.T) {
	svc := &Service{}
	claims := &middleware.SessionClaims{UserID: uuid.New(), Role: "bendahara"}
	_, err := svc.ApplyKeringanan(context.Background(), claims, uuid.NewString(), KeringananInput{
		NominalKeringanan: -500,
		Alasan:            "uji",
	}, "127.0.0.1")
	var de *response.DomainError
	if !errors.As(err, &de) || de.Status != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative keringanan, got %v", err)
	}
}
