package qris

import (
	"fmt"
	"strings"
	"testing"
)

func TestCalculateCRC16(t *testing.T) {
	// Standard CCITT-FALSE test vector: "123456789" -> 0x29B1
	res := CalculateCRC16("123456789")
	if res != "29B1" {
		t.Fatalf("Expected CRC16 29B1, got %s", res)
	}
}

func TestGenerateDynamicQRIS_AutoLockNominal(t *testing.T) {
	// Tag 26 value:
	// 0011ID.CO.QRIS.WWW (18 chars)
	// 0118936005230000012345 (22 chars)
	// 0215ID1020304050607 (19 chars)
	// 0303UMI (7 chars)
	// Total 18 + 22 + 19 + 7 = 66 chars
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)

	merchantName := "KOPDES MAKMUR" // 13 chars
	cityName := "BANDUNG"           // 7 chars

	staticPayload := fmt.Sprintf(
		"000201010211%s5204541153033605802ID59%02d%s60%02d%s63041234",
		tag26,
		len(merchantName), merchantName,
		len(cityName), cityName,
	)

	nominalTotal := int64(20142)
	noPembayaran := "PAY-TEST-9999"

	dynamicQR, err := GenerateDynamicQRIS(staticPayload, nominalTotal, noPembayaran)
	if err != nil {
		t.Fatalf("Gagal menghasilkan dynamic QRIS: %v", err)
	}

	// 1. Parsing kembali hasil dynamic QRIS
	tlvs, err := ParseTLV(dynamicQR)
	if err != nil {
		t.Fatalf("Hasil dynamic QRIS tidak valid format TLV: %v", err)
	}

	tlvMap := make(map[string]string)
	for _, t := range tlvs {
		tlvMap[t.Tag] = t.Value
	}

	// 2. Verifikasi Tag 01 berubah menjadi '12' (Dinamis / Auto-Lock)
	if tlvMap["01"] != "12" {
		t.Errorf("Tag 01 harus '12' (Dinamis), dapat: %s", tlvMap["01"])
	}

	// 3. Verifikasi Tag 54 terkunci dengan nominal 20142
	if tlvMap["54"] != "20142" {
		t.Errorf("Tag 54 harus '20142', dapat: %s", tlvMap["54"])
	}

	// 4. Verifikasi Tag 62 memiliki sub-tag 01 dengan nomor pembayaran
	tag62Val := tlvMap["62"]
	if !strings.Contains(tag62Val, noPembayaran) {
		t.Errorf("Tag 62 harus memuat nomor pembayaran %s, dapat: %s", noPembayaran, tag62Val)
	}

	// 5. Verifikasi Tag 63 (CRC16) valid
	if len(tlvMap["63"]) != 4 {
		t.Errorf("Tag 63 CRC harus 4 karakter hex, dapat: %s", tlvMap["63"])
	}

	// Verifikasi hitung ulang CRC
	prefix := dynamicQR[:len(dynamicQR)-4]
	expectedCRC := CalculateCRC16(prefix)
	actualCRC := dynamicQR[len(dynamicQR)-4:]
	if actualCRC != expectedCRC {
		t.Errorf("CRC tidak cocok! Expected: %s, Actual: %s", expectedCRC, actualCRC)
	}
}
