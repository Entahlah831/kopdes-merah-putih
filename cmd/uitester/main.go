package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/testseed"
)

type configResponse struct {
	AppEnv         string                 `json:"app_env"`
	APIPublicURL   string                 `json:"api_public_url"`
	UIBind         string                 `json:"ui_bind"`
	UIPort         int                    `json:"ui_port"`
	Resources      testseed.ResourceIDs   `json:"resources"`
	SeededAccounts []testseed.AccountSpec `json:"seeded_accounts"`
}

func isAllowedProbeURL(targetRaw string, apiPublicURL string) bool {
	u, err := url.Parse(strings.TrimSpace(targetRaw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "127.0.0.1" || host == "localhost" || host == "api" {
		return true
	}
	if apiURL, err := url.Parse(strings.TrimSpace(apiPublicURL)); err == nil && strings.EqualFold(u.Host, apiURL.Host) {
		return true
	}
	return false
}

func newUITesterHandler(cfg config.AppConfig, pool *pgxpool.Pool, uiDir, apiURL string) (http.Handler, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "production") {
		return nil, fmt.Errorf("KEAMANAN UITESTER: ditolak berjalan ketika APP_ENV=production")
	}

	absUIDir, err := filepath.Abs(uiDir)
	if err != nil {
		return nil, fmt.Errorf("direktori frontend-tester tidak valid: %w", err)
	}

	apiBase := strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if apiBase == "" {
		hostPort := cfg.APIHostPort
		if hostPort <= 0 {
			hostPort = cfg.APIPort
		}
		apiBase = fmt.Sprintf("http://127.0.0.1:%d", hostPort)
	}

	mux := http.NewServeMux()
	registerUITesterRoutes(mux, cfg, pool, apiBase, absUIDir)
	return mux, nil
}

func registerUITesterRoutes(mux *http.ServeMux, cfg config.AppConfig, pool *pgxpool.Pool, apiBase, absUIDir string) {
	mux.HandleFunc("GET /config.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(configResponse{
			AppEnv:         cfg.AppEnv,
			APIPublicURL:   apiBase,
			UIBind:         "127.0.0.1",
			UIPort:         cfg.UIPort,
			Resources:      testseed.LookupResources(r.Context(), pool),
			SeededAccounts: testseed.Accounts(),
		})
	})

	mux.HandleFunc("POST /api/probe-cors", handleProbeCORS(apiBase))
	mux.HandleFunc("POST /api/totp-helper", handleTOTPHelper(cfg))

	fileServer := http.FileServer(http.Dir(absUIDir))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		fileServer.ServeHTTP(w, r)
	}))
}

func handleProbeCORS(apiBase string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")

		var req struct {
			TargetURL string `json:"target_url"`
			Origin    string `json:"origin"`
			Method    string `json:"method"`
			ReqHdr    string `json:"request_headers"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil {
			http.Error(w, `{"error":"JSON tidak valid"}`, http.StatusBadRequest)
			return
		}
		if !isAllowedProbeURL(req.TargetURL, apiBase) {
			http.Error(w, `{"error":"Target probe dibatasi hanya ke URL API uji"}`, http.StatusBadRequest)
			return
		}
		if req.Method == "" {
			req.Method = http.MethodGet
		}

		upReq, err := http.NewRequestWithContext(r.Context(), http.MethodOptions, req.TargetURL, nil)
		if err != nil {
			http.Error(w, `{"error":"Gagal membuat request preflight"}`, http.StatusBadRequest)
			return
		}
		if req.Origin != "" {
			upReq.Header.Set("Origin", req.Origin)
		}
		upReq.Header.Set("Access-Control-Request-Method", req.Method)
		if req.ReqHdr != "" {
			upReq.Header.Set("Access-Control-Request-Headers", req.ReqHdr)
		}

		client := &http.Client{Timeout: 5 * time.Second}
		// #nosec G704 -- URL sudah diverifikasi ketat oleh isAllowedProbeURL ke host loopback/service API uji
		resp, err := client.Do(upReq)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()

		headers := map[string]string{}
		for _, k := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers", "Access-Control-Expose-Headers", "Vary", "X-Request-ID"} {
			if v := resp.Header.Get(k); v != "" {
				headers[k] = v
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status_code": resp.StatusCode, "headers": headers})
	}
}

func handleTOTPHelper(cfg config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "production") {
			http.Error(w, `{"error":"TOTP helper dinonaktifkan di production"}`, http.StatusForbidden)
			return
		}

		var req struct {
			SecretBase32  string `json:"secret_base32"`
			AccountEmail  string `json:"account_email,omitempty"`
			OffsetSeconds int    `json:"offset_seconds,omitempty"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, `{"error":"JSON tidak valid"}`, http.StatusBadRequest)
			return
		}

		secret := strings.TrimSpace(req.SecretBase32)
		if secret == "" && strings.TrimSpace(req.AccountEmail) != "" {
			secret = testseed.DeterministicTOTPSecret(cfg, req.AccountEmail)
		}
		if secret == "" {
			http.Error(w, `{"error":"Sertakan secret_base32 atau account_email"}`, http.StatusBadRequest)
			return
		}
		targetTime := time.Now().UTC().Add(time.Duration(req.OffsetSeconds) * time.Second)
		code, err := security.GenerateTOTPCode(secret, targetTime)
		if err != nil {
			http.Error(w, `{"error":"Gagal menghasilkan kode TOTP"}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
	}
}

func main() {
	envFileFlag := flag.String("env-file", "", "Path berkas konfigurasi (.env.test atau .env.test.alt)")
	uiDirFlag := flag.String("ui-dir", "frontend-tester", "Direktori berkas statis Frontend Tester")
	bindFlag := flag.String("bind", "127.0.0.1", "Alamat bind HTTP Frontend Tester")
	apiURLFlag := flag.String("api-url", "", "Override URL publik API untuk Frontend Tester")
	flag.Parse()

	envFile := strings.TrimSpace(*envFileFlag)
	if envFile == "" {
		envFile = strings.TrimSpace(os.Getenv("ENV_FILE"))
	}
	if envFile == "" {
		envFile = ".env.test"
	}

	cfg, err := config.LoadFromEnvAndFile(envFile)
	if err != nil {
		cleanEnvFile := strings.NewReplacer("\r", "", "\n", "").Replace(envFile)
		log.Fatalf("Gagal memuat konfigurasi %s: %v", cleanEnvFile, err) // #nosec G706 -- nama berkas env sudah disanitasi CR/LF
	}
	if strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "production") {
		log.Fatalf("KEAMANAN UITESTER: APP_ENV=production terdeteksi; server tester dilarang dijalankan di produksi.")
	}

	var pool *pgxpool.Pool
	if dsn, dErr := cfg.BuildDatabaseDSN(); dErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if p, pErr := pgxpool.New(ctx, dsn); pErr == nil {
			if p.Ping(ctx) == nil {
				pool = p
				defer pool.Close()
			} else {
				p.Close()
			}
		}
		cancel()
	}

	handler, err := newUITesterHandler(cfg, pool, *uiDirFlag, *apiURLFlag)
	if err != nil {
		log.Fatal(err)
	}

	bind := strings.TrimSpace(*bindFlag)
	if bind == "" {
		bind = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", bind, cfg.UIPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Frontend Tester Kopdes Merah Putih berjalan pada http://%s (API target: http://127.0.0.1:%d, APP_ENV=%s)", addr, cfg.APIHostPort, cfg.AppEnv)
	log.Fatal(srv.ListenAndServe())
}
