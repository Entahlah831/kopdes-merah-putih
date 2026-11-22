package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/config"
)

var validSQLIdentifier = api.ValidSQLIdentifier

func sanitizeLogValue(v string) string {
	return api.SanitizeLogValue(v)
}

func main() {
	envFileFlag := flag.String("env-file", "", "Path berkas konfigurasi (.env / .env.test / .env.test.alt)")
	flag.Parse()

	envFile := strings.TrimSpace(*envFileFlag)
	if envFile == "" {
		envFile = strings.TrimSpace(os.Getenv("ENV_FILE"))
	}
	if envFile == "" {
		envFile = ".env"
	}

	appCfg, err := config.LoadFromEnvAndFile(envFile)
	if err != nil {
		log.Fatalf("Gagal memuat konfigurasi aplikasi: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := api.ServeAppConfig(ctx, appCfg); err != nil {
		log.Fatalf("Server API berhenti: %v", err)
	}
}
