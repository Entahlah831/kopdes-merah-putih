package main

import (
	"context"
	"fmt"
	"log"
	"math"
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

func main() {
	envFile := strings.TrimSpace(os.Getenv("ENV_FILE"))
	if envFile == "" {
		envFile = ".env"
	}
	appCfg, err := config.LoadFromEnvAndFile(envFile)
	if err != nil {
		log.Fatalf("Gagal memuat konfigurasi aplikasi: %v", err)
	}

	if !validSQLIdentifier.MatchString(appCfg.DBAppRole) {
		log.Fatalf("KONFIGURASI ILEGAL: DB_APP_ROLE mengandung karakter tidak valid: %q", appCfg.DBAppRole)
	}

	dsn, err := appCfg.BuildDatabaseDSN()
	if err != nil {
		log.Fatalf("Konfigurasi koneksi PostgreSQL tidak valid: %v", err)
	}

	if strings.EqualFold(strings.TrimSpace(appCfg.AppEnv), "production") {
		if err := appCfg.ValidateProduction(); err != nil {
			log.Fatalf("KONFIGURASI PRODUKSI TIDAK VALID: %v", err)
		}
		if err := api.ValidateProductionDatabasePassword(appCfg.AppEnv, appCfg.DBPassword, appCfg.KopdesAppPassword); err != nil {
			log.Fatalf("KONFIGURASI ILEGAL: %v", err)
		}
	}

	srvCfg := api.ConfigFromAppConfig(appCfg)
	if err := srvCfg.Validate(); err != nil {
		log.Fatalf("Konfigurasi keamanan aplikasi tidak valid: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pgxCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("Gagal mem-parsing DSN PostgreSQL (%s): %v", security.RedactDSN(dsn), err)
	}

	if appCfg.DBPoolMaxConns > 0 && appCfg.DBPoolMaxConns <= math.MaxInt32 {
		pgxCfg.MaxConns = int32(appCfg.DBPoolMaxConns)
	}
	if appCfg.DBPoolMinConns > 0 && appCfg.DBPoolMinConns <= math.MaxInt32 {
		pgxCfg.MinConns = int32(appCfg.DBPoolMinConns)
	}

	stmtMs := appCfg.DBStatementTimeout.Milliseconds()
	if stmtMs <= 0 {
		stmtMs = 15000
	}
	lockMs := appCfg.DBLockTimeout.Milliseconds()
	if lockMs <= 0 {
		lockMs = 5000
	}
	idleMs := appCfg.DBIdleInTxTimeout.Milliseconds()
	if idleMs <= 0 {
		idleMs = 30000
	}
	appRole := appCfg.DBAppRole

	pgxCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		sessionSQL := fmt.Sprintf(`
			SET statement_timeout = '%dms';
			SET lock_timeout = '%dms';
			SET idle_in_transaction_session_timeout = '%dms';
			SET ROLE %s;
		`, stmtMs, lockMs, idleMs, appRole)
		_, err := conn.Exec(ctx, sessionSQL)
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		log.Fatalf("Gagal inisialisasi koneksi pool PostgreSQL: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("PostgreSQL tidak dapat dihubungi (%s): %v", security.RedactDSN(dsn), err)
	}
	log.Printf("Koneksi ke PostgreSQL (%s) berhasil terhubung.", security.RedactDSN(dsn))

	if err := db.CheckDatabaseRole(ctx, pool, appCfg.AllowSuperuserDB); err != nil {
		log.Fatalf("Gagal validasi keamanan role database: %v", err)
	}

	server := api.NewServer(pool, srvCfg)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	db.StartPartitionMaintenanceJob(workerCtx, pool, appCfg.PartitionJobInterval, appCfg.PartitionMonthsAhead)
	db.StartPDPPurgeWorker(workerCtx, pool, appCfg.PDPPurgeInterval)

	// Channel sinyal shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Server API Kopdes Merah Putih berjalan pada http://0.0.0.0:%d", appCfg.APIPort)
		if mAddr := server.MetricsAddr(); mAddr != "" {
			log.Printf("Endpoint internal Prometheus /metrics berjalan pada http://0.0.0.0%s/metrics", mAddr)
		}
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server berhenti karena error: %v", err)
		}
	}()

	<-stop
	log.Println("Menerima sinyal shutdown, mematikan server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error saat shutdown server: %v", err)
	}
	fmt.Println("Server berhasil dimatikan secara bersih.")
}
