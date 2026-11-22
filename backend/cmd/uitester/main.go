package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/uiserve"
)

func newUITesterHandler(cfg config.AppConfig, pool *pgxpool.Pool, uiDir, apiURL string) (http.Handler, error) {
	return uiserve.NewHandler(cfg, pool, uiDir, apiURL)
}

func main() {
	envFileFlag := flag.String("env-file", "", "Path berkas konfigurasi (.env.test atau .env.test.alt)")
	uiDirFlag := flag.String("ui-dir", "frontend/tester", "Direktori berkas statis Frontend Tester")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool := uiserve.TryOptionalPool(ctx, cfg)
	if pool != nil {
		defer pool.Close()
	}
	if err := uiserve.Serve(ctx, cfg, pool, *uiDirFlag, *bindFlag, *apiURLFlag); err != nil {
		log.Fatal(err)
	}
}
