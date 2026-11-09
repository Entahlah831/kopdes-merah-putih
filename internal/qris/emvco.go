package qris

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CalculateCRC16 menghitung checksum CRC-16/CCITT-FALSE (Poly: 0x1021, Init: 0xFFFF, RefIn/RefOut: false)
// standar resmi EMVCo QR Code & QRIS Bank Indonesia.
func CalculateCRC16(data string) string {
	crc := uint16(0xFFFF)
	for i := 0; i < len(data); i++ {
		crc ^= uint16(data[i]) << 8
		for j := 0; j < 8; j++ {
			if (crc & 0x8000) != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc = crc << 1
			}
		}
	}
	return fmt.Sprintf("%04X", crc)
}

// TLV merepresentasikan pasangan Tag, Length, Value pada format EMVCo
type TLV struct {
	Tag   string
	Value string
}

// ParseTLV mem-parsing string EMVCo menjadi daftar TLV secara berurutan dan menolak duplikasi tag
func ParseTLV(payload string) ([]TLV, error) {
	var result []TLV
	seenTags := make(map[string]bool)
	idx := 0
	length := len(payload)

	for idx < length {
		// Minimal harus ada Tag (2 char) + Length (2 char) = 4 char
		if idx+4 > length {
			return nil, fmt.Errorf("format EMVCo terpotong pada indeks %d", idx)
		}

		tag := payload[idx : idx+2]
		idx += 2

		valLen, err := strconv.Atoi(payload[idx : idx+2])
		if err != nil {
			return nil, fmt.Errorf("panjang tag %s tidak valid: %w", tag, err)
		}
		if valLen < 0 {
			return nil, fmt.Errorf("panjang tag %s tidak boleh negatif", tag)
		}
		idx += 2

		if idx+valLen > length {
			return nil, fmt.Errorf("nilai tag %s melampaui panjang string", tag)
		}

		val := payload[idx : idx+valLen]
		idx += valLen

		if seenTags[tag] {
			return nil, fmt.Errorf("terdapat duplikasi tag %s pada payload EMVCo", tag)
		}
		seenTags[tag] = true

		result = append(result, TLV{Tag: tag, Value: val})
	}

	return result, nil
}

// ValidateQRIS memvalidasi kelayakan EMVCo, Tag 00 ("01"), dan integritas checksum CRC16 (Tag 63)
func ValidateQRIS(payload string) error {
	payload = strings.TrimSpace(payload)
	if len(payload) < 8 {
		return fmt.Errorf("panjang payload QRIS terlalu pendek")
	}

	// 1. Verifikasi posisi Tag 63 (CRC) pada 8 karakter terakhir
	crcTagIdx := strings.LastIndex(payload, "6304")
	if crcTagIdx == -1 || crcTagIdx != len(payload)-8 {
		return fmt.Errorf("tag 6304 (CRC) wajib berada pada 8 karakter terakhir payload")
	}

	expectedCRC := strings.ToUpper(payload[len(payload)-4:])
	calculatedCRC := CalculateCRC16(payload[:len(payload)-4])
	if expectedCRC != calculatedCRC {
		return fmt.Errorf("checksum CRC16 tidak valid: tercatat %s, seharusnya %s", expectedCRC, calculatedCRC)
	}

	// 2. Parse TLV & verifikasi tidak ada tag duplikat
	tlvs, err := ParseTLV(payload)
	if err != nil {
		return fmt.Errorf("struktur TLV tidak valid: %w", err)
	}

	// 3. Verifikasi Tag 00 (Payload Format Indicator) bernilai '01'
	if len(tlvs) == 0 || tlvs[0].Tag != "00" || tlvs[0].Value != "01" {
		return fmt.Errorf("tag 00 (Payload Format Indicator) wajib ada di awal dan bernilai '01'")
	}

	return nil
}

// FormatTLV mengonversi satu TLV menjadi string representasi EMVCo
func FormatTLV(tag, val string) string {
	return fmt.Sprintf("%s%02d%s", tag, len(val), val)
}

// GenerateDynamicQRIS mengonversi payload QRIS Statis menjadi QRIS Dinamis berfitur
// Auto-Lock Nominal (Tag 01 = '12', Tag 54 = NominalTotalRp, Tag 62 = NoPembayaran)
// dan menghitung ulang CRC16 (Tag 63) sesuai standar ASPI & PADG BI 21/18/PADG/2019.
func GenerateDynamicQRIS(staticPayload string, nominalTotalRp int64, noPembayaran string) (string, error) {
	if strings.TrimSpace(staticPayload) == "" {
		return "", fmt.Errorf("payload QRIS statis kosong")
	}

	// Validasi kepatuhan EMVCo & integritas CRC payload statis masukan
	if err := ValidateQRIS(staticPayload); err != nil {
		return "", fmt.Errorf("validasi payload QRIS statis gagal: %w", err)
	}

	tlvs, err := ParseTLV(staticPayload)
	if err != nil {
		return "", fmt.Errorf("gagal mem-parsing payload QRIS statis: %w", err)
	}

	tagMap := make(map[string]string)
	var tagOrder []string

	for _, t := range tlvs {
		// Abaikan tag 63 (CRC lama) karena akan dihitung ulang secara dinamis
		if t.Tag == "63" {
			continue
		}
		// Hapus Tag 55, 56, 57 (Tip / Convenience Fee) saat Tag 54 (Nominal Tetap) dipasang
		if t.Tag == "55" || t.Tag == "56" || t.Tag == "57" {
			continue
		}
		if _, exists := tagMap[t.Tag]; !exists {
			tagOrder = append(tagOrder, t.Tag)
		}
		tagMap[t.Tag] = t.Value
	}

	// 1. Ubah Tag 01 (Point of Initiation Method) dari '11' (Statis) menjadi '12' (Dinamis)
	tagMap["01"] = "12"
	if !containsTag(tagOrder, "01") {
		// Sisipkan setelah Tag 00
		tagOrder = insertTagAfter(tagOrder, "00", "01")
	}

	// 2. Set Tag 53 (Currency) ke '360' (IDR) jika belum ada
	if _, ok := tagMap["53"]; !ok {
		tagMap["53"] = "360"
		tagOrder = append(tagOrder, "53")
	}

	// 3. Set Tag 54 (Transaction Amount / Auto-Lock Nominal)
	nominalStr := fmt.Sprintf("%d", nominalTotalRp)
	tagMap["54"] = nominalStr
	if !containsTag(tagOrder, "54") {
		tagOrder = insertTagAfter(tagOrder, "53", "54")
	}

	// Bersihkan Tag 55, 56, 57 dari tagMap dan urutan tag
	delete(tagMap, "55")
	delete(tagMap, "56")
	delete(tagMap, "57")
	tagOrder = removeTags(tagOrder, "55", "56", "57")

	// 4. Set Tag 58 (Country Code) ke 'ID' jika belum ada
	if _, ok := tagMap["58"]; !ok {
		tagMap["58"] = "ID"
		tagOrder = append(tagOrder, "58")
	}

	// 5. Injeksi Tag 62 (Additional Data Field Template) dengan nomor pembayaran/invoice
	if noPembayaran != "" {
		// Potong nomor pembayaran maksimal 25 karakter sesuai standar ASPI Sub-tag 01 (Bill Number)
		billNo := noPembayaran
		if len(billNo) > 25 {
			billNo = billNo[:25]
		}
		subTag01 := FormatTLV("01", billNo)

		// Jika tag 62 sudah ada, kita gabungkan atau ganti sub-tag 01
		existingTag62 := tagMap["62"]
		if existingTag62 != "" {
			subTlvs, err := ParseTLV(existingTag62)
			if err == nil {
				var newSubTLVs []TLV
				found := false
				for _, st := range subTlvs {
					if st.Tag == "01" {
						newSubTLVs = append(newSubTLVs, TLV{Tag: "01", Value: billNo})
						found = true
					} else {
						newSubTLVs = append(newSubTLVs, st)
					}
				}
				if !found {
					newSubTLVs = append(newSubTLVs, TLV{Tag: "01", Value: billNo})
				}
				var sb strings.Builder
				for _, st := range newSubTLVs {
					sb.WriteString(FormatTLV(st.Tag, st.Value))
				}
				tagMap["62"] = sb.String()
			} else {
				tagMap["62"] = subTag01
			}
		} else {
			tagMap["62"] = subTag01
			tagOrder = append(tagOrder, "62")
		}
	}

	// Susun urutan tag sesuai standar EMVCo (urut berdasarkan nomor tag)
	sort.SliceStable(tagOrder, func(i, j int) bool {
		ti, errI := strconv.Atoi(tagOrder[i])
		tj, errJ := strconv.Atoi(tagOrder[j])
		if errI == nil && errJ == nil {
			return ti < tj
		}
		return tagOrder[i] < tagOrder[j]
	})

	// Rekonstruksi string EMVCo tanpa CRC
	var builder strings.Builder
	for _, tag := range tagOrder {
		builder.WriteString(FormatTLV(tag, tagMap[tag]))
	}

	// Tambahkan Tag 63 Length 04 untuk kalkulasi CRC
	builder.WriteString("6304")
	partialPayload := builder.String()

	// Hitung CRC16-CCITT False
	crc := CalculateCRC16(partialPayload)

	return partialPayload + crc, nil
}

func containsTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

func insertTagAfter(tags []string, target, newTag string) []string {
	var result []string
	inserted := false
	for _, t := range tags {
		result = append(result, t)
		if t == target && !inserted {
			result = append(result, newTag)
			inserted = true
		}
	}
	if !inserted {
		result = append(result, newTag)
	}
	return result
}

func removeTags(tags []string, targets ...string) []string {
	targetMap := make(map[string]bool)
	for _, t := range targets {
		targetMap[t] = true
	}
	var res []string
	for _, t := range tags {
		if !targetMap[t] {
			res = append(res, t)
		}
	}
	return res
}
