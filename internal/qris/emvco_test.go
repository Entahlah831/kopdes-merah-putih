package qris

import (
	"fmt"
	"strings"
	"testing"
)

func createValidStaticQRIS(tag26Val, merchantName, cityName string) string {
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	prefix := fmt.Sprintf(
		"000201010211%s5204541153033605802ID59%02d%s60%02d%s6304",
		tag26,
		len(merchantName), merchantName,
		len(cityName), cityName,
	)
	crc := CalculateCRC16(prefix)
	return prefix + crc
}

func TestCalculateCRC16(t *testing.T) {
	// Standard CCITT-FALSE test vector: "123456789" -> 0x29B1
	res := CalculateCRC16("123456789")
	if res != "29B1" {
		t.Fatalf("Expected CRC16 29B1, got %s", res)
	}
}

func TestGenerateDynamicQRIS_AutoLockNominal(t *testing.T) {
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	merchantName := "KOPDES MAKMUR"
	cityName := "BANDUNG"

	staticPayload := createValidStaticQRIS(tag26Val, merchantName, cityName)
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

func TestEMVCo_CRC_Verification_Invalid_Rejected(t *testing.T) {
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	staticPayload := createValidStaticQRIS(tag26Val, "KOPDES MAKMUR", "BANDUNG")

	// Rusak CRC di akhir
	corrupted := staticPayload[:len(staticPayload)-4] + "DEAD"
	err := ValidateQRIS(corrupted)
	if err == nil {
		t.Fatalf("Expected ValidateQRIS to reject invalid CRC 'DEAD', but passed")
	}

	_, err = GenerateDynamicQRIS(corrupted, 50000, "PAY-FAIL-1")
	if err == nil {
		t.Fatalf("Expected GenerateDynamicQRIS to reject corrupted static QRIS")
	}
}

func TestEMVCo_Tag00_Validation(t *testing.T) {
	// Tag 00 salah nilai ("02" alih-alih "01")
	prefix := "0002020102115204541153033605802ID5906KOPDES6007BANDUNG6304"
	crc := CalculateCRC16(prefix)
	badTag00 := prefix + crc

	err := ValidateQRIS(badTag00)
	if err == nil {
		t.Fatalf("Expected ValidateQRIS to reject Tag 00 != '01'")
	}
}

func TestEMVCo_DuplicateTag_Rejected(t *testing.T) {
	// Dua kali Tag 58 (Country Code)
	prefix := "0002010102115802ID5802ID5204541153033605906KOPDES6007BANDUNG6304"
	crc := CalculateCRC16(prefix)
	dupPayload := prefix + crc

	err := ValidateQRIS(dupPayload)
	if err == nil {
		t.Fatalf("Expected ValidateQRIS to reject duplicate tag 58")
	}
}

func TestEMVCo_Tag55_57_StrippedWhenTag54Present(t *testing.T) {
	// Payload statis memiliki Tag 55 (Tip), 56 (Fee Fixed), 57 (Fee %)
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	prefix := fmt.Sprintf(
		"000201010211%s520454115303360550201560510000570405005802ID5906KOPDES6007BANDUNG6304",
		tag26,
	)
	crc := CalculateCRC16(prefix)
	staticWithFees := prefix + crc

	if err := ValidateQRIS(staticWithFees); err != nil {
		t.Fatalf("Valid static with fees failed validation: %v", err)
	}

	dynamicQR, err := GenerateDynamicQRIS(staticWithFees, 100000, "PAY-FEE-TEST")
	if err != nil {
		t.Fatalf("Failed to generate dynamic QRIS: %v", err)
	}

	tlvs, err := ParseTLV(dynamicQR)
	if err != nil {
		t.Fatalf("Parse dynamic failed: %v", err)
	}

	for _, tl := range tlvs {
		if tl.Tag == "55" || tl.Tag == "56" || tl.Tag == "57" {
			t.Fatalf("Tag %s should have been stripped when Tag 54 fixed amount was set", tl.Tag)
		}
	}
}

func TestEMVCo_RoundTrip_PreservesUnknownTags(t *testing.T) {
	// Pasang Tag kustom ASPI / privat misal Tag 91
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	customVal := "PRIVATE_ASPI_DATA_123"
	tag91 := fmt.Sprintf("91%02d%s", len(customVal), customVal)

	prefix := fmt.Sprintf(
		"000201010211%s5204541153033605802ID5906KOPDES6007BANDUNG%s6304",
		tag26, tag91,
	)
	crc := CalculateCRC16(prefix)
	staticWithCustom := prefix + crc

	dynamicQR, err := GenerateDynamicQRIS(staticWithCustom, 50000, "PAY-CUSTOM")
	if err != nil {
		t.Fatalf("Generate dynamic error: %v", err)
	}

	tlvs, err := ParseTLV(dynamicQR)
	if err != nil {
		t.Fatalf("Parse TLV error: %v", err)
	}

	found91 := false
	for _, tl := range tlvs {
		if tl.Tag == "91" && tl.Value == customVal {
			found91 = true
			break
		}
	}
	if !found91 {
		t.Errorf("Custom tag 91 was lost during dynamic QRIS conversion")
	}

	// Verifikasi CRC akhir
	if err := ValidateQRIS(dynamicQR); err != nil {
		t.Errorf("Final dynamic QRIS failed ValidateQRIS: %v", err)
	}
}

func FuzzParseTLV(f *testing.F) {
	// Seed with valid and boundary payloads
	f.Add("00020101021153033605802ID63041234")
	f.Add("")
	f.Add("00")
	f.Add("0000")
	f.Add("0099")
	f.Add("6304ABCD")

	f.Fuzz(func(t *testing.T, payload string) {
		// Fuzz target must not panic under any malformed payload input
		_, _ = ParseTLV(payload)
		_ = ValidateQRIS(payload)
	})
}
