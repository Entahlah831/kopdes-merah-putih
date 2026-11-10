package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

// loadEnv membaca file .env dan melakukan ekspansi variabel lingkungan secara dinamis
func loadEnv(filepath string) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			// Ekspansi variabel seperti ${DB_PORT} jika ada
			expanded := os.ExpandEnv(v)
			_ = os.Setenv(k, expanded)
		}
	}
}

// getServerPort mencari port server dari berbagai kandidat environment variable
func getServerPort() int {
	candidates := []string{"APP_PORT", "PORT", "SERVER_PORT", "API_PORT"}
	for _, envKey := range candidates {
		if val := os.Getenv(envKey); val != "" {
			if p, err := strconv.Atoi(val); err == nil && p > 0 {
				return p
			}
		}
	}
	return 8080
}

// getDatabaseDSN menyusun DSN PostgreSQL secara fleksibel dari variabel individu atau URL lengkap
func getDatabaseDSN() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		return dsn
	}

	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = os.Getenv("POSTGRES_PORT")
	}
	if port == "" {
		port = "5433" // Default fallback pengujian lokal saat ini
	}
	user := os.Getenv("DB_USER")
	if user == "" {
		user = "kopdes_app"
	}
	pass := os.Getenv("DB_PASSWORD")
	if pass == "" {
		pass = "kopdes_dev_password"
	}
	dbname := os.Getenv("DB_NAME")
	if dbname == "" {
		dbname = "kopdes_db"
	}
	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {
		sslmode = "disable"
	}

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, pass, host, port, dbname, sslmode)
}

func main() {
	loadEnv(".env")

	port := getServerPort()
	dsn := getDatabaseDSN()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pgxCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("Gagal mem-parsing DSN PostgreSQL (%s): %v", security.RedactDSN(dsn), err)
	}

	pgxCfg.MaxConns = 30
	pgxCfg.MinConns = 5
	pgxCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		// Pasang pembatas waktu eksekusi SQL di sisi PostgreSQL untuk mencegah hung query / lock leak (S3)
		_, err := conn.Exec(ctx, `
			SET statement_timeout = '15s';
			SET lock_timeout = '5s';
			SET idle_in_transaction_session_timeout = '30s';
			SET ROLE kopdes_app;
		`)
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
	log.Printf("Koneksi ke PostgreSQL berhasil terhubung.")

	allowSuperuser := os.Getenv("ALLOW_SUPERUSER_DB") == "true"
	if os.Getenv("APP_ENV") == "production" && allowSuperuser {
		log.Fatalf("KONFIGURASI ILEGAL: ALLOW_SUPERUSER_DB dilarang bernilai true pada APP_ENV=production")
	}
	if err := db.CheckDatabaseRole(ctx, pool, allowSuperuser); err != nil {
		log.Fatalf("Gagal validasi keamanan role database: %v", err)
	}

	cfg := api.DefaultConfig()
	cfg.Port = port
	if s := os.Getenv("JWT_SECRET"); s != "" {
		cfg.JWTSecret = s
	}
	if s := os.Getenv("BLIND_INDEX_SECRET"); s != "" {
		cfg.BlindIndexSecret = s
	}
	if s := os.Getenv("QRIS_SECRET"); s != "" {
		cfg.QRISSecret = s
	}
	if s := os.Getenv("AES_MASTER_KEY"); s != "" {
		cfg.AESMasterKey = s
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Konfigurasi keamanan aplikasi tidak valid: %v", err)
	}

	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		var list []string
		for _, o := range strings.Split(origins, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				list = append(list, trimmed)
			}
		}
		if len(list) > 0 {
			cfg.AllowedOrigins = list
		}
	}

	// Validasi kepatuhan konfigurasi produksi (S3)
	if os.Getenv("APP_ENV") == "production" {
		dbPass := os.Getenv("DB_PASSWORD")
		if strings.TrimSpace(dbPass) == "" || dbPass == "kopdes_dev_password" {
			log.Fatalf("KONFIGURASI ILEGAL: DB_PASSWORD dilarang menggunakan kata sandi bawaan pengujian pada APP_ENV=production")
		}
		if len(cfg.AllowedOrigins) == 0 || (len(cfg.AllowedOrigins) == 1 && cfg.AllowedOrigins[0] == "*") {
			log.Fatalf("KONFIGURASI ILEGAL: ALLOWED_ORIGINS wajib didefinisikan secara eksplisit dan dilarang '*' pada APP_ENV=production")
		}
	}

	if tp := os.Getenv("TRUSTED_PROXIES"); tp != "" {
		var list []string
		for _, p := range strings.Split(tp, ",") {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				list = append(list, trimmed)
			}
		}
		if len(list) > 0 {
			cfg.TrustedProxies = list
		}
	}

	cfg.QRISDynamicEnabled = strings.EqualFold(os.Getenv("QRIS_DYNAMIC_ENABLED"), "true")
	cfg.AllowPersonalPaymentPilot = strings.EqualFold(os.Getenv("ALLOW_PERSONAL_PAYMENT_PILOT"), "true")

	server := api.NewServer(pool, cfg)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	db.StartPartitionMaintenanceJob(workerCtx, pool, 24*time.Hour, 3)
	db.StartPDPPurgeWorker(workerCtx, pool, 24*time.Hour)

	// Channel sinyal shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Server API Kopdes Merah Putih berjalan pada http://0.0.0.0:%d", port)
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
