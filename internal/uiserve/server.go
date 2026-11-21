package uiserve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
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

// NewHandler membangun HTTP handler UI uji/wireframe non-produksi serta endpoint utilitas /config.json.
func NewHandler(cfg config.AppConfig, pool *pgxpool.Pool, uiDir, apiURL string) (http.Handler, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "production") {
		return nil, fmt.Errorf("KEAMANAN UITESTER: ditolak berjalan ketika APP_ENV=production")
	}
	absUIDir, err := filepath.Abs(uiDir)
	if err != nil {
		return nil, fmt.Errorf("direktori UI tidak valid: %w", err)
	}
	apiBase := resolveAPIBase(cfg, apiURL)
	mux := http.NewServeMux()
	registerRoutes(mux, cfg, pool, apiBase, absUIDir)
	return mux, nil
}

// Serve menjalankan HTTP server penyaji UI uji/wireframe sampai ctx selesai.
func Serve(ctx context.Context, cfg config.AppConfig, pool *pgxpool.Pool, uiDir, bind, apiURL string) error {
	handler, err := NewHandler(cfg, pool, uiDir, apiURL)
	if err != nil {
		return err
	}
	b := strings.TrimSpace(bind)
	if b == "" {
		b = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", b, cfg.UIPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("UI Server Kopdes Merah Putih berjalan pada http://%s (API target: %s, APP_ENV=%s)", addr, resolveAPIBase(cfg, apiURL), cfg.AppEnv) // #nosec G706 -- tervalidasi numerik & APP_ENV
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		sCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		return srv.Shutdown(sCtx)
	case err := <-errCh:
		return err
	}
}

// TryOptionalPool membuka pool database opsional berbatas waktu untuk lookup manifest resource uji.
func TryOptionalPool(ctx context.Context, cfg config.AppConfig) *pgxpool.Pool {
	dsn, err := cfg.BuildDatabaseDSN()
	if err != nil {
		return nil
	}
	tCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	p, err := pgxpool.New(tCtx, dsn)
	if err != nil {
		return nil
	}
	if p.Ping(tCtx) != nil {
		p.Close()
		return nil
	}
	return p
}

func resolveAPIBase(cfg config.AppConfig, apiURL string) string {
	apiBase := strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if apiBase != "" {
		return apiBase
	}
	hostPort := cfg.APIHostPort
	if hostPort <= 0 {
		hostPort = cfg.APIPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d", hostPort)
}

func registerRoutes(mux *http.ServeMux, cfg config.AppConfig, pool *pgxpool.Pool, apiBase, absUIDir string) {
	mux.HandleFunc("GET /config.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(configResponse{
			AppEnv: cfg.AppEnv, APIPublicURL: apiBase, UIBind: "127.0.0.1",
			UIPort: cfg.UIPort, Resources: testseed.LookupResources(r.Context(), pool),
			SeededAccounts: testseed.Accounts(),
		})
	})
	mux.HandleFunc("POST /api/probe-cors", handleProbeCORS(apiBase))
	mux.HandleFunc("POST /api/totp-helper", handleTOTPHelper(cfg))

	fileServer := http.FileServer(http.Dir(absUIDir))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if shouldServeSPAIndex(absUIDir, r.URL.Path) {
			http.ServeFile(w, r, filepath.Join(absUIDir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	}))
}

func shouldServeSPAIndex(absUIDir, reqPath string) bool {
	if reqPath == "/" || reqPath == "" {
		return false
	}
	clean := strings.TrimPrefix(path.Clean("/"+reqPath), "/")
	if clean == "" || clean == "." || path.Ext(clean) != "" {
		return false
	}
	uiFS := os.DirFS(absUIDir)
	if _, err := fs.Stat(uiFS, clean); err == nil {
		return false
	}
	_, err := fs.Stat(uiFS, "index.html")
	return err == nil
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
		executeCORSProbe(w, r, req.TargetURL, req.Origin, req.Method, req.ReqHdr)
	}
}

func executeCORSProbe(w http.ResponseWriter, r *http.Request, targetURL, origin, method, reqHdr string) {
	if method == "" {
		method = http.MethodGet
	}
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodOptions, targetURL, nil)
	if err != nil {
		http.Error(w, `{"error":"Gagal membuat request preflight"}`, http.StatusBadRequest)
		return
	}
	if origin != "" {
		upReq.Header.Set("Origin", origin)
	}
	upReq.Header.Set("Access-Control-Request-Method", method)
	if reqHdr != "" {
		upReq.Header.Set("Access-Control-Request-Headers", reqHdr)
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
		code, err := security.GenerateTOTPCode(secret, time.Now().UTC().Add(time.Duration(req.OffsetSeconds)*time.Second))
		if err != nil {
			http.Error(w, `{"error":"Gagal menghasilkan kode TOTP"}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
	}
}
