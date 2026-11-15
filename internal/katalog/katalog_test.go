package katalog

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/kopdes/merah-putih/internal/api/response"
)

func int64Ptr(v int64) *int64 { return &v }

func TestValidateProdukPrices(t *testing.T) {
	st, err := validateProdukPrices(int64Ptr(15000), int64Ptr(12000), 10, "")
	if err != nil || st != "aktif" {
		t.Fatalf("expected valid status aktif, got (%q, %v)", st, err)
	}
	if _, err := validateProdukPrices(int64Ptr(0), int64Ptr(10000), 5, "aktif"); err == nil {
		t.Fatal("expected error when harga_umum <= 0")
	}
	if _, err := validateProdukPrices(int64Ptr(10000), int64Ptr(9000), -1, "aktif"); err == nil {
		t.Fatal("expected error for negative stock")
	}
	if _, err := validateProdukPrices(int64Ptr(10000), nil, 1, "arsip"); err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestAssignActivePriceAndPromoWindow(t *testing.T) {
	var dto ProdukDTO
	assignActivePrice(&dto, pgtype.Int8{Int64: 15000, Valid: true}, pgtype.Int8{}, true, false)
	if dto.HargaAktifTersedia || dto.HargaAktif != nil {
		t.Fatal("expected HargaAktifTersedia=false when member price is NULL")
	}
	assignActivePrice(&dto, pgtype.Int8{Int64: 15000, Valid: true}, pgtype.Int8{Int64: 12000, Valid: true}, true, true)
	if !dto.HargaAktifTersedia || *dto.HargaAktif != 12000 || dto.HargaUmum == nil || dto.HargaAnggota == nil {
		t.Fatalf("unexpected active/staff price assignment: %+v", dto)
	}
	if _, _, err := parsePromoWindow("2026-12-01T00:00:00Z", "2026-11-01T00:00:00Z"); err == nil {
		t.Fatal("expected error when berakhir <= mulai")
	} else {
		var de *response.DomainError
		if !errors.As(err, &de) || de.Status != http.StatusBadRequest {
			t.Fatalf("expected 400 error, got %v", err)
		}
	}
}
