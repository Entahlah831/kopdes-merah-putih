package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func serverHostPort(t *testing.T, rawURL string) (string, string) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", rawURL, err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", u.Host, err)
	}
	return host, port
}

func TestHealthcheck_APIPassAndFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	host, port := serverHostPort(t, srv.URL)
	t.Setenv("API_PORT", port)
	if err := runHealthcheckCommand(context.Background(), "", []string{"--host", host}); err != nil {
		t.Fatalf("healthcheck 200 OK harus nil, dapat: %v", err)
	}

	srvUnhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srvUnhealthy.Close()
	host2, port2 := serverHostPort(t, srvUnhealthy.URL)
	t.Setenv("API_PORT", port2)
	if err := runHealthcheckCommand(context.Background(), "", []string{"--host", host2}); err == nil {
		t.Fatal("healthcheck 503 harus mengembalikan error")
	}

	srvUnhealthy.Close()
	if err := runHealthcheckCommand(context.Background(), "", []string{"--host", host2, "--timeout", "300ms"}); err == nil {
		t.Fatal("healthcheck ke port tertutup harus mengembalikan error")
	}
}

func TestHealthcheck_WorkerAndValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, port := serverHostPort(t, srv.URL)
	t.Setenv("WORKER_PORT", port)
	if err := runHealthcheckCommand(context.Background(), "", []string{"--service", "worker", "--host", host}); err != nil {
		t.Fatalf("healthcheck worker harus nil, dapat: %v", err)
	}

	for _, badPort := range []string{"", "0", "abc", "70000"} {
		t.Setenv("API_PORT", badPort)
		if err := runHealthcheckCommand(context.Background(), "", nil); err == nil {
			t.Fatalf("API_PORT=%q harus ditolak", badPort)
		}
	}

	if err := runHealthcheckCommand(context.Background(), "", []string{"--service", "unknown"}); err == nil {
		t.Fatal("--service tidak dikenal harus ditolak")
	}
	if err := runHealthcheckCommand(context.Background(), "", []string{"--host", "example.com"}); err == nil {
		t.Fatal("--host non-loopback harus ditolak")
	}
}

func TestHealthcheck_EnvFileFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, portStr := serverHostPort(t, srv.URL)
	portNum, err := strconv.Atoi(portStr)
	if err != nil || portNum <= 0 {
		t.Fatalf("port tidak valid: %s", portStr)
	}
	t.Setenv("API_PORT", "")

	envFile := filepath.Join(t.TempDir(), "hc.env")
	if wErr := os.WriteFile(envFile, []byte("API_PORT="+strconv.Itoa(portNum)+"\n"), 0o600); wErr != nil {
		t.Fatal(wErr)
	}
	if err := runHealthcheckCommand(context.Background(), envFile, []string{"--host", host}); err != nil {
		t.Fatalf("healthcheck dengan fallback --env-file gagal: %v", err)
	}
}
