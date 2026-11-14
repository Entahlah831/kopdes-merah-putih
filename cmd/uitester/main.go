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

	"github.com/google/uuid"
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
	MetricsURL     string                 `json:"metrics_url"`
	KoperasiAID    string                 `json:"koperasi_a_id"`
	KoperasiBID    string                 `json:"koperasi_b_id"`
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

func newUITesterHandler(cfg config.AppConfig, pool *pgxpool.Pool, uiDir string) (http.Handler, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "production") {
		return nil, fmt.Errorf("KEAMANAN UITESTER: ditolak berjalan ketika APP_ENV=production")
	}

	absUIDir, err := filepath.Abs(uiDir)
	if err != nil {
		return nil, fmt.Errorf("direktori frontend-tester tidak valid: %w", err)
	}

	apiBase := strings.TrimRight(strings.TrimSpace(cfg.APIPublicURL), "/")
	if apiBase == "" {
		apiBase = fmt.Sprintf("http://127.0.0.1:%d", cfg.APIHostPort)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /config.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		metricsHostPort := cfg.MetricsHostPort
		if metricsHostPort <= 0 {
			metricsHostPort = cfg.MetricsPort
		}
		payload := configResponse{
			AppEnv:         cfg.AppEnv,
			APIPublicURL:   apiBase,
			UIBind:         cfg.UIBind,
			UIPort:         cfg.UIPort,
			MetricsURL:     fmt.Sprintf("http://127.0.0.1:%d/metrics", metricsHostPort),
			KoperasiAID:    testseed.KoperasiAID.String(),
			KoperasiBID:    testseed.KoperasiBID.String(),
			Resources:      testseed.Resources(),
			SeededAccounts: testseed.Accounts(),
		}
		_ = json.NewEncoder(w).Encode(payload)
	})

	mux.HandleFunc("POST /api/probe-cors", func(w http.ResponseWriter, r *http.Request) {
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

		client := &http.Client{Timeout: 5 * time.Second}
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
		for _, k := range []string{
			"Access-Control-Allow-Origin",
			"Access-Control-Allow-Methods",
			"Access-Control-Allow-Headers",
			"Access-Control-Expose-Headers",
			"Vary",
			"X-Request-ID",
		} {
			if v := resp.Header.Get(k); v != "" {
				headers[k] = v
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status_code": resp.StatusCode,
			"headers":     headers,
		})
	})

	mux.HandleFunc("POST /api/totp-helper", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "production") {
			http.Error(w, `{"error":"TOTP helper dinonaktifkan di production"}`, http.StatusForbidden)
			return
		}

		var req struct {
			SecretBase32 string `json:"secret_base32"`
			Email        string `json:"email"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, `{"error":"JSON tidak valid"}`, http.StatusBadRequest)
			return
		}

		secret := strings.TrimSpace(req.SecretBase32)
		if secret == "" && strings.TrimSpace(req.Email) != "" {
			if !strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "test") || !strings.HasSuffix(strings.TrimSpace(cfg.DBName), "_test") {
				http.Error(w, `{"error":"Lookup TOTP DB hanya diizinkan pada APP_ENV=test dan database *_test"}`, http.StatusForbidden)
				return
			}
			if pool == nil {
				http.Error(w, `{"error":"Koneksi database uji tidak tersedia untuk lookup email"}`, http.StatusServiceUnavailable)
				return
			}
			kp, kpErr := security.NewMultiKeyProviderFromConfig(cfg.AESKeyVersion, cfg.AESKeyID, cfg.AESMasterKey, cfg.AESPreviousKeys)
			if kpErr != nil {
				http.Error(w, `{"error":"KeyProvider tidak valid"}`, http.StatusInternalServerError)
				return
			}
			emailHash := security.HashBlindIndex(strings.TrimSpace(req.Email), []byte(cfg.BlindIndexSecret))
			var (
				userID    uuid.UUID
				pwdHash   string
				nama      string
				isAdmin   bool
				statusAct bool
				kopID     uuid.NullUUID
				roleName  string
			)
			err := pool.QueryRow(r.Context(), `
				SELECT user_id, password_hash, nama_lengkap, is_super_admin, status_aktif, koperasi_id, role_name
				FROM fn_lookup_user_login($1, NULL);
			`, emailHash).Scan(&userID, &pwdHash, &nama, &isAdmin, &statusAct, &kopID, &roleName)
			if err != nil {
				http.Error(w, `{"error":"Akun uji tidak ditemukan"}`, http.StatusNotFound)
				return
			}
			var (
				mfaEnabled  bool
				encSecret   []byte
				lastStep    int64
				encRecovery []byte
			)
			if err := pool.QueryRow(r.Context(), "SELECT mfa_enabled, totp_secret_enkripsi, last_totp_step, mfa_recovery_codes FROM fn_get_user_mfa($1);", userID).
				Scan(&mfaEnabled, &encSecret, &lastStep, &encRecovery); err != nil || !mfaEnabled || len(encSecret) == 0 {
				http.Error(w, `{"error":"MFA tidak aktif pada akun ini"}`, http.StatusNotFound)
				return
			}
			plain, decErr := kp.DecryptWithAAD(encSecret, []byte(fmt.Sprintf("users:totp:%s", userID)))
			if decErr != nil {
				http.Error(w, `{"error":"Gagal mendekripsi secret TOTP uji"}`, http.StatusInternalServerError)
				return
			}
			secret = string(plain)
		}

		if secret == "" {
			http.Error(w, `{"error":"Sertakan secret_base32 atau email akun uji"}`, http.StatusBadRequest)
			return
		}
		code, err := security.GenerateTOTPCode(secret, time.Now().UTC())
		if err != nil {
			http.Error(w, `{"error":"Gagal menghasilkan kode TOTP"}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
	})

	fileServer := http.FileServer(http.Dir(absUIDir))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		fileServer.ServeHTTP(w, r)
	}))

	return mux, nil
}

func main() {
	envFileFlag := flag.String("env-file", "", "Path berkas konfigurasi (.env.test atau .env.test.alt)")
	uiDirFlag := flag.String("ui-dir", "frontend-tester", "Direktori berkas statis Frontend Tester")
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
	if dsn, dsnErr := cfg.BuildDatabaseDSN(); dsnErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if p, err := pgxpool.New(ctx, dsn); err == nil {
			pool = p
			defer pool.Close()
		}
		cancel()
	}

	handler, err := newUITesterHandler(cfg, pool, *uiDirFlag)
	if err != nil {
		log.Fatal(err)
	}

	bind := strings.TrimSpace(cfg.UIBind)
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

	log.Printf("Frontend Tester Kopdes Merah Putih berjalan pada http://%s (API target: %s, APP_ENV=%s)", addr, cfg.APIPublicURL, cfg.AppEnv)
	log.Fatal(srv.ListenAndServe())
}
