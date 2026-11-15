package pesanan

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestValidateCheckoutInput(t *testing.T) {
	idA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	idB := "11111111-1111-1111-1111-111111111111"
	vc, err := validateCheckoutInput(CheckoutInput{
		Items: []CheckoutItemInput{
			{ProdukID: idA, Kuantitas: 2},
			{ProdukID: idA, Kuantitas: 1},
			{ProdukID: idB, Kuantitas: 3},
		},
		MetodePengiriman:  "diantar",
		MetodePembayaran:  "qris",
		NamaPenerima:      "Budi",
		HPPenerima:        "+628123456789",
		AlamatPengantaran: "Jl. Desa No. 1",
		TanggalAntar:      "2026-12-01",
	})
	if err != nil {
		t.Fatalf("validateCheckoutInput unexpected error: %v", err)
	}
	if len(vc.items) != 2 || vc.items[0].id.String() != idB || vc.items[1].qty != 3 {
		t.Fatalf("expected merged and sorted items, got %+v", vc.items)
	}
	_, err = validateCheckoutInput(CheckoutInput{
		Items: []CheckoutItemInput{{ProdukID: idA, Kuantitas: 1}}, MetodePengiriman: "diantar",
		MetodePembayaran: "tunai", NamaPenerima: "Budi", HPPenerima: "08123456",
	})
	if err == nil {
		t.Fatal("expected error when alamat_pengantaran omitted for delivery")
	}
}

func TestExportDaftarAntarCSV_FormulaNeutralization(t *testing.T) {
	raw, err := ExportDaftarAntarCSV([]DaftarAntarItem{{
		ID:                uuid.New(),
		NomorPesanan:      "ORD-001",
		Status:            "dibayar",
		NamaPenerima:      "=CMD()",
		HPPenerima:        "081234",
		AlamatPengantaran: "@EVIL",
		TotalBayar:        25000,
		Catatan:           "+SUM(A1)",
	}})
	if err != nil {
		t.Fatalf("ExportDaftarAntarCSV: %v", err)
	}
	csvStr := string(raw)
	for _, neutral := range []string{"'=CMD()", "'@EVIL", "'+SUM(A1)"} {
		if !strings.Contains(csvStr, neutral) {
			t.Fatalf("expected formula neutralization token %q in CSV:\n%s", neutral, csvStr)
		}
	}
}
