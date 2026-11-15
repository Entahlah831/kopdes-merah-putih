package anggota

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
)

func TestAnggotaRBACAndStatusValidation(t *testing.T) {
	svc := NewService(nil, Config{})
	ownAnggotaID := uuid.New()
	otherID := uuid.New().String()

	claimsMember := &middleware.SessionClaims{
		UserID:    uuid.New(),
		AnggotaID: &ownAnggotaID,
		Role:      "anggota",
	}
	var de *response.DomainError
	_, err := svc.GetDetail(context.Background(), claimsMember, otherID, false, "127.0.0.1")
	if !errors.As(err, &de) || de.Status != http.StatusForbidden {
		t.Fatalf("expected 403 FORBIDDEN querying other member, got %v", err)
	}

	claimsBendahara := &middleware.SessionClaims{UserID: uuid.New(), Role: "bendahara"}
	_, err = svc.GetDetail(context.Background(), claimsBendahara, ownAnggotaID.String(), true, "127.0.0.1")
	if !errors.As(err, &de) || de.Status != http.StatusForbidden {
		t.Fatalf("expected 403 UNMASK_FORBIDDEN for bendahara, got %v", err)
	}

	_, err = svc.UpdateStatus(context.Background(), claimsBendahara, ownAnggotaID.String(), UpdateStatusInput{Status: "dibekukan"}, "127.0.0.1")
	if !errors.As(err, &de) || de.Status != http.StatusBadRequest {
		t.Fatalf("expected 400 INVALID_STATUS, got %v", err)
	}
}
