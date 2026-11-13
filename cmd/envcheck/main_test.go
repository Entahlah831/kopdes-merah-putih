package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEnvCheck_Compare_AndNeverLeaksSecrets(t *testing.T) {
	tmp := t.TempDir()
	fileA := filepath.Join(tmp, "a.env")
	fileB := filepath.Join(tmp, "b.env")

	if err := os.WriteFile(fileA, []byte("APP_ENV=test\nAPI_PORT=8080\nJWT_SECRET=SecretValueShouldNeverAppearA\nONLY_IN_A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("APP_ENV=test\nAPI_PORT=18080\nJWT_SECRET=SecretValueShouldNeverAppearB\nONLY_IN_B=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := runCLI([]string{"--compare", fileA, fileB}, &out, &out)
	text := out.String()
	if code == 0 {
		t.Fatalf("Expected non-zero exit when compare finds mismatched keys ONLY_IN_A/ONLY_IN_B, output:\n%s", text)
	}
	if strings.Contains(text, "SecretValueShouldNeverAppear") {
		t.Fatalf("SECURITY VIOLATION: envcheck leaked secret in compare output:\n%s", text)
	}
	if !strings.Contains(text, "ONLY_IN_A") || !strings.Contains(text, "ONLY_IN_B") {
		t.Fatalf("Expected diff report to name ONLY_IN_A and ONLY_IN_B, got:\n%s", text)
	}
}

func TestEnvCheck_StrictAndPortConflictDetection(t *testing.T) {
	// Uji --strict terhadap .env.test yang lengkap dan valid
	root, _ := filepath.Abs("../..")
	var out bytes.Buffer
	code := runCLI([]string{"--strict", "--env-file", filepath.Join(root, ".env.test")}, &out, &out)
	if code != 0 {
		t.Fatalf("Expected --strict on .env.test to pass (code 0), got %d:\n%s", code, out.String())
	}

	// Uji --ports mendeteksi port host yang sedang dipakai oleh proses lain
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer ln.Close()
	busyPort := ln.Addr().(*net.TCPAddr).Port

	tmp := t.TempDir()
	envBusy := filepath.Join(tmp, "busy.env")
	baseBytes, err := os.ReadFile(filepath.Join(root, ".env.test"))
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(baseBytes), "API_HOST_PORT=8080", "API_HOST_PORT="+strconv.Itoa(busyPort), 1)
	if err := os.WriteFile(envBusy, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}

	var portOut bytes.Buffer
	portCode := runCLI([]string{"--ports", "--env-file", envBusy}, &portOut, &portOut)
	if portCode == 0 {
		t.Fatalf("Expected --ports to fail when API_HOST_PORT=%d is already bound, got code 0:\n%s", busyPort, portOut.String())
	}
	if !strings.Contains(portOut.String(), strconv.Itoa(busyPort)) {
		t.Fatalf("Expected output to report occupied port %d, got:\n%s", busyPort, portOut.String())
	}

	// Uji Set B (.env.test.alt) lulus --strict dan --ports (tanpa menyentuh kode Go)
	var setBOut bytes.Buffer
	setBCode := runCLI([]string{"--strict", "--ports", "--env-file", filepath.Join(root, ".env.test.alt")}, &setBOut, &setBOut)
	if setBCode != 0 {
		t.Fatalf("Expected Set B (.env.test.alt) --strict --ports to pass, got %d:\n%s", setBCode, setBOut.String())
	}

	// Uji deteksi konflik konfigurasi internal ketika dua service memakai host port sama
	altBytes, err := os.ReadFile(filepath.Join(root, ".env.test.alt"))
	if err != nil {
		t.Fatal(err)
	}
	envDupPort := filepath.Join(tmp, "dup_port.env")
	dupContent := strings.Replace(string(altBytes), "METRICS_HOST_PORT=19100", "METRICS_HOST_PORT=18080", 1)
	if err := os.WriteFile(envDupPort, []byte(dupContent), 0o600); err != nil {
		t.Fatal(err)
	}
	var dupOut bytes.Buffer
	if dupCode := runCLI([]string{"--ports", "--no-listen", "--env-file", envDupPort}, &dupOut, &dupOut); dupCode == 0 {
		t.Fatalf("Expected --ports to fail when API_HOST_PORT and METRICS_HOST_PORT collide on 18080")
	} else if !strings.Contains(dupOut.String(), "18080") {
		t.Fatalf("Expected conflict message to mention 18080, got:\n%s", dupOut.String())
	}
}
