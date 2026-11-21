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

func TestParseCSVRows_OptionalTanggalLahir(t *testing.T) {
	csvNoDOB := "nama_lengkap,nik,no_hp,alamat\nBudi Desa,3201010101910002,081234567891,Dusun Krajan RT 02\n"
	rows, errs := parseCSVRows(csvNoDOB)
	if len(errs) != 0 || len(rows) != 1 {
		t.Fatalf("expected CSV without tanggal_lahir to parse cleanly, rows=%d errs=%v", len(rows), errs)
	}
	if rows[0].TanggalLahir != "" {
		t.Fatalf("expected empty TanggalLahir, got %q", rows[0].TanggalLahir)
	}
	csvWithDOB := "nama_lengkap,nik,no_hp,tanggal_lahir,alamat\nSiti Desa,3201010101910003,081234567892,1991-04-12,Dusun Selatan\n"
	rows2, errs2 := parseCSVRows(csvWithDOB)
	if len(errs2) != 0 || len(rows2) != 1 || rows2[0].TanggalLahir != "1991-04-12" {
		t.Fatalf("expected CSV with valid tanggal_lahir to parse, rows=%+v errs=%v", rows2, errs2)
	}
}
