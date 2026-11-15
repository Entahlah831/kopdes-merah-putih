package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

var validSQLIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

var logSanitizer = strings.NewReplacer("\r", "", "\n", "")

func sanitizeLogValue(v string) string {
	return logSanitizer.Replace(v)
}

func buildDBPool(ctx context.Context, appCfg config.AppConfig) (*pgxpool.Pool, string) {
	if !validSQLIdentifier.MatchString(config.DefaultDBAppRole) {
		log.Fatalf("KONFIGURASI ILEGAL: role %q tidak valid", config.DefaultDBAppRole)
	}
	dsn, err := appCfg.BuildDatabaseDSN()
	if err != nil {
		log.Fatalf("Konfigurasi koneksi PostgreSQL tidak valid: %v", err)
	}
	pgxCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("Gagal mem-parsing DSN PostgreSQL (%s): %v", sanitizeLogValue(security.RedactDSN(dsn)), err) // #nosec G706 -- DSN sudah diredaksi & disanitasi CR/LF
	}
	pgxCfg.MaxConns = 25
	pgxCfg.MinConns = 2
	pgxCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		sessionSQL := fmt.Sprintf(`
			SET statement_timeout = '15000ms';
			SET lock_timeout = '5000ms';
			SET idle_in_transaction_session_timeout = '30000ms';
			SET ROLE %s;
		`, config.DefaultDBAppRole)
		_, err := conn.Exec(ctx, sessionSQL)
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		log.Fatalf("Gagal inisialisasi koneksi pool PostgreSQL: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("PostgreSQL tidak dapat dihubungi (%s): %v", sanitizeLogValue(security.RedactDSN(dsn)), err) // #nosec G706 -- DSN sudah diredaksi & disanitasi CR/LF
	}
	if err := db.CheckDatabaseRole(ctx, pool, false); err != nil {
		log.Fatalf("Gagal validasi keamanan role database: %v", err)
	}
	return pool, dsn
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
	if strings.EqualFold(strings.TrimSpace(appCfg.AppEnv), "production") {
		if err := appCfg.ValidateProduction(); err != nil {
			log.Fatalf("KONFIGURASI PRODUKSI TIDAK VALID: %v", err)
		}
		if err := api.ValidateProductionDatabasePassword(appCfg.AppEnv, appCfg.DBPassword); err != nil {
			log.Fatalf("KONFIGURASI ILEGAL: %v", err)
		}
	}

	srvCfg := api.ConfigFromAppConfig(appCfg)
	if err := srvCfg.Validate(); err != nil {
		log.Fatalf("Konfigurasi keamanan aplikasi tidak valid: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, dsn := buildDBPool(ctx, appCfg)
	defer pool.Close()
	log.Printf("Koneksi ke PostgreSQL (%s) berhasil terhubung.", sanitizeLogValue(security.RedactDSN(dsn))) // #nosec G706 -- DSN sudah diredaksi & disanitasi CR/LF

	server := api.NewServer(pool, srvCfg)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Server API Kopdes Merah Putih berjalan pada http://0.0.0.0:%d", appCfg.APIPort) // #nosec G706 -- port tervalidasi numerik dari AppConfig
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server berhenti karena error: %v", err)
		}
	}()

	<-stop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error saat shutdown server: %v", err)
	}
}
