package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputePercentiles(t *testing.T) {
	p50, p95, p99 := computePercentiles(nil)
	if p50 != 0 || p95 != 0 || p99 != 0 {
		t.Fatalf("slice kosong harus (0,0,0), dapat (%v,%v,%v)", p50, p95, p99)
	}

	latencies := make([]time.Duration, 100)
	for i := 0; i < 100; i++ {
		latencies[i] = time.Duration(100-i) * time.Millisecond
	}
	p50, p95, p99 = computePercentiles(latencies)
	if p50 != 50.0 || p95 != 95.0 || p99 != 99.0 {
		t.Fatalf("percentiles=(%v,%v,%v), ingin (50,95,99)", p50, p95, p99)
	}
}

func TestReadIDsFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ids.txt")
	content := "# komentar uji\n\n11111111-1111-4111-8111-000000000001\n  22222222-2222-4222-8222-000000000002  \n"
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("gagal tulis file uji: %v", err)
	}

	ids, err := readIDsFile(p)
	if err != nil {
		t.Fatalf("readIDsFile gagal: %v", err)
	}
	if len(ids) != 2 || ids[0] != "11111111-1111-4111-8111-000000000001" || ids[1] != "22222222-2222-4222-8222-000000000002" {
		t.Fatalf("hasil ids tidak sesuai: %v", ids)
	}

	if _, err := readIDsFile(filepath.Join(dir, "tidak-ada.txt")); err == nil {
		t.Fatalf("harus error saat file tidak ditemukan")
	}
}
