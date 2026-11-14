package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/testseed"
)

func validateSeedGuard(cfg config.AppConfig) error {
	if !strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "test") {
		return fmt.Errorf("KEAMANAN SEED: ditolak karena APP_ENV=%q (wajib 'test')", cfg.AppEnv)
	}
	if !strings.HasSuffix(strings.TrimSpace(cfg.DBName), "_test") {
		return fmt.Errorf("KEAMANAN SEED: ditolak karena DB_NAME=%q tidak berakhiran '_test'", cfg.DBName)
	}
	return nil
}

func main() {
	envFileFlag := flag.String("env-file", "", "Path berkas konfigurasi uji (.env.test atau .env.test.alt)")
	passwordFlag := flag.String("password", "", "Sandi override opsional untuk akun uji")
	flag.Parse()

	envFile := strings.TrimSpace(*envFileFlag)
	if envFile == "" {
		envFile = strings.TrimSpace(os.Getenv("ENV_FILE"))
	}
	if envFile == "" {
		envFile = ".env.test"
	}

	cleanEnvFile := strings.NewReplacer("\r", "", "\n", "").Replace(envFile)
	cfg, err := config.LoadFromEnvAndFile(envFile)
	if err != nil {
		log.Fatalf("Gagal memuat konfigurasi %s: %v", cleanEnvFile, err) // #nosec G706 -- nama berkas env sudah disanitasi CR/LF
	}
	if err := validateSeedGuard(cfg); err != nil {
		log.Fatal(err)
	}

	dsn, err := cfg.BuildDatabaseDSN()
	if err != nil {
		log.Fatalf("DSN tidak valid: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		cleanDSN := strings.NewReplacer("\r", "", "\n", "").Replace(security.RedactDSN(dsn))
		log.Fatalf("Gagal koneksi DB (%s): %v", cleanDSN, err) // #nosec G706 -- DSN sudah diredaksi & disanitasi CR/LF
	}
	defer pool.Close()

	usedPass, err := testseed.SeedAll(ctx, pool, cfg, *passwordFlag)
	if err != nil {
		log.Fatalf("Seed akun uji gagal: %v", err)
	}

	fmt.Printf("Seeded 11 akun uji sintetis ke database %s (%s) selesai.\n", cfg.DBName, security.RedactDSN(dsn))
	fmt.Printf("Gunakan kata sandi uji dari TEST_SEED_PASSWORD (panjang %d karakter) di input UI Tester.\n", len(usedPass))
	for _, a := range testseed.Accounts() {
		fmt.Printf("- [%s] %-32s role=%-10s mfa=%t\n", a.TenantLabel, a.Email, a.Role, a.MFAEnabled)
	}
}
