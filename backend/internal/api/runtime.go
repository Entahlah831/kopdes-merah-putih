package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

// ValidSQLIdentifier memvalidasi nama role PostgreSQL aman sebelum dipakai SET ROLE.
var ValidSQLIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

var logSanitizer = strings.NewReplacer("\r", "", "\n", "")

// SanitizeLogValue menghapus CR dan LF untuk mencegah injeksi log.
func SanitizeLogValue(v string) string {
	return logSanitizer.Replace(v)
}

// BuildDBPool membuka pool koneksi PostgreSQL non-superuser (kopdes_app) beserta timeout sesi.
func BuildDBPool(ctx context.Context, appCfg config.AppConfig) (*pgxpool.Pool, string, error) {
	if !ValidSQLIdentifier.MatchString(config.DefaultDBAppRole) {
		return nil, "", fmt.Errorf("role database %q tidak valid", config.DefaultDBAppRole)
	}
	dsn, err := appCfg.BuildDatabaseDSN()
	if err != nil {
		return nil, "", fmt.Errorf("konfigurasi DSN PostgreSQL tidak valid: %w", err)
	}
	pgxCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, "", fmt.Errorf("parsing DSN gagal (%s): %w", SanitizeLogValue(security.RedactDSN(dsn)), err)
	}
	pgxCfg.MaxConns = 25
	pgxCfg.MinConns = 2
	pgxCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		const sessionSQL = "SET statement_timeout = '15000ms'; SET lock_timeout = '5000ms'; SET idle_in_transaction_session_timeout = '30000ms'; SET ROLE " + config.DefaultDBAppRole + ";"
		// nosemgrep: kopdes-no-raw-sql-in-internal
		_, err := conn.Exec(ctx, sessionSQL)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, "", fmt.Errorf("inisialisasi pool PostgreSQL gagal: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, "", fmt.Errorf("PostgreSQL tidak dapat dihubungi (%s): %w", SanitizeLogValue(security.RedactDSN(dsn)), err)
	}
	if err := db.CheckDatabaseRole(ctx, pool, false); err != nil {
		pool.Close()
		return nil, "", fmt.Errorf("validasi keamanan role database gagal: %w", err)
	}
	return pool, dsn, nil
}

// ServeAppConfig memvalidasi konfigurasi, membuka pool PostgreSQL, dan menjalankan HTTP API sampai ctx dibatalkan.
func ServeAppConfig(ctx context.Context, appCfg config.AppConfig) error {
	if strings.EqualFold(strings.TrimSpace(appCfg.AppEnv), "production") {
		if err := appCfg.ValidateProduction(); err != nil {
			return fmt.Errorf("konfigurasi produksi tidak valid: %w", err)
		}
		if err := ValidateProductionDatabasePassword(appCfg.AppEnv, appCfg.DBPassword); err != nil {
			return fmt.Errorf("konfigurasi password DB produksi ilegal: %w", err)
		}
	}
	srvCfg := ConfigFromAppConfig(appCfg)
	if err := srvCfg.Validate(); err != nil {
		return fmt.Errorf("konfigurasi keamanan tidak valid: %w", err)
	}
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, dsn, err := BuildDBPool(initCtx, appCfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Printf("Koneksi ke PostgreSQL (%s) berhasil terhubung.", SanitizeLogValue(security.RedactDSN(dsn))) // #nosec G706 -- teredaksi

	server := NewServer(pool, srvCfg)
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Server API Kopdes Merah Putih berjalan pada http://0.0.0.0:%d", appCfg.APIPort) // #nosec G706 -- tervalidasi numerik
		if sErr := server.Start(); sErr != nil && sErr != http.ErrServerClosed {
			errCh <- sErr
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		sCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer sCancel()
		return server.Shutdown(sCtx)
	case sErr := <-errCh:
		return sErr
	}
}
