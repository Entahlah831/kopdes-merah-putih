package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kopdes/merah-putih/internal/config"
)

func isLoopbackHealthHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// runHealthcheckCommand memeriksa endpoint /healthz API atau worker berdasarkan port env.
func runHealthcheckCommand(ctx context.Context, envFlag string, args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	service := fs.String("service", "api", "Target layanan healthcheck (api atau worker)")
	host := fs.String("host", "127.0.0.1", "Host loopback pemeriksaan liveness /healthz")
	timeout := fs.Duration("timeout", 3*time.Second, "Batas waktu permintaan HTTP /healthz")
	envOverride := fs.String("env-file", envFlag, "Opsional berkas .env fallback bila env port kosong")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout healthcheck harus > 0")
	}
	cleanHost := strings.TrimSpace(*host)
	if !isLoopbackHealthHost(cleanHost) {
		return fmt.Errorf("host healthcheck wajib loopback (127.0.0.1/localhost), dapat %q", *host)
	}
	port, err := resolveHealthcheckPort(strings.ToLower(strings.TrimSpace(*service)), strings.TrimSpace(*envOverride))
	if err != nil {
		return err
	}
	targetURL := "http://" + net.JoinHostPort(cleanHost, strconv.Itoa(port)) + "/healthz"
	return probeHealthURL(ctx, targetURL, *timeout)
}

// resolveHealthcheckPort mengambil API_PORT atau WORKER_PORT dari env proses atau fallback .env.
func resolveHealthcheckPort(service, envFile string) (int, error) {
	var raw string
	var keyName string
	switch service {
	case "", "api":
		keyName = "API_PORT"
		raw = strings.TrimSpace(os.Getenv("API_PORT"))
	case "worker":
		keyName = "WORKER_PORT"
		raw = strings.TrimSpace(os.Getenv("WORKER_PORT"))
	default:
		return 0, fmt.Errorf("layanan healthcheck tidak dikenal %q (gunakan api atau worker)", service)
	}
	if raw == "" && envFile != "" {
		if m, err := config.ParseEnvFile(envFile); err == nil {
			raw = strings.TrimSpace(m[keyName])
		}
	}
	if raw == "" {
		return 0, fmt.Errorf("variabel lingkungan %s wajib disetel untuk healthcheck", keyName)
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("nilai %s tidak valid: %q", keyName, raw)
	}
	return port, nil
}

// probeHealthURL memanggil GET /healthz dan mengembalikan error bila status bukan 200 OK.
func probeHealthURL(ctx context.Context, targetURL string, timeout time.Duration) error {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// #nosec G704 -- targetURL dibatasi ketat ke host loopback dan port env tervalidasi oleh caller
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return fmt.Errorf("buat request healthcheck: %w", err)
	}
	client := &http.Client{Timeout: timeout}
	// #nosec G704 -- URL disusun deterministik ke endpoint liveness /healthz dari env port
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("probe /healthz gagal: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe /healthz mengembalikan status HTTP %d", resp.StatusCode)
	}
	return nil
}
