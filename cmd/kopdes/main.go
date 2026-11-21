package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/iuran"
	"github.com/kopdes/merah-putih/internal/pembayaran"
	"github.com/kopdes/merah-putih/internal/pendaftaran"
	"github.com/kopdes/merah-putih/internal/pesanan"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/worker"
)

var validRoleID = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	envFlag := ".env"
	args := os.Args[1:]
	if len(args) >= 2 && args[0] == "--env-file" {
		envFlag = args[1]
		args = args[2:]
	} else if v := strings.TrimSpace(os.Getenv("ENV_FILE")); v != "" {
		envFlag = v
	}
	if len(args) < 1 || (args[0] != "worker" && len(args) < 2) {
		printUsage()
		os.Exit(2)
	}

	cfg, err := config.LoadFromEnvAndFile(envFlag)
	if err != nil {
		log.Fatalf("gagal memuat env %s: %v", strings.NewReplacer("\r", "", "\n", "").Replace(envFlag), err) // #nosec G706 -- sanitized
	}

	if args[0] == "worker" {
		sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		pool, pErr := openPool(sigCtx, cfg)
		if pErr != nil {
			log.Fatalf("koneksi database gagal: %v", pErr)
		}
		defer pool.Close()
		if err := runWorkerCommand(sigCtx, pool, cfg, args[1:]); err != nil {
			log.Fatalf("worker gagal: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := openPool(ctx, cfg)
	if err != nil {
		log.Fatalf("koneksi database gagal: %v", err)
	}
	defer pool.Close()

	if err := dispatchCommand(ctx, pool, cfg, args); err != nil {
		log.Fatalf("perintah gagal: %v", err)
	}
}

func printUsage() {
	fmt.Println("Penggunaan: kopdes [--env-file .env] <domain> <aksi> [flag]")
	fmt.Println("  admin create          --email <e> --nama <n> --hp <hp> --password <pwd>")
	fmt.Println("  admin reset-mfa       --email <e>")
	fmt.Println("  admin reset-password  --email <e> (--password <pwd> | --password-stdin)")
	fmt.Println("  iuran generate        --periode YYYY-MM")
	fmt.Println("  pdp purge             [--days 30]")
	fmt.Println("  bukti purge           [--days 365]")
	fmt.Println("  pesanan expire")
	fmt.Println("  storage migrate-files")
	fmt.Println("  worker                [--tick 60s] [--once]")
}

func openPool(ctx context.Context, cfg config.AppConfig) (*pgxpool.Pool, error) {
	if !validRoleID.MatchString(config.DefaultDBAppRole) {
		return nil, fmt.Errorf("role database %q tidak valid", config.DefaultDBAppRole)
	}
	dsn, err := cfg.BuildDatabaseDSN()
	if err != nil {
		return nil, err
	}
	pgxCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pgxCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("SET ROLE %s", config.DefaultDBAppRole))
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, err
	}
	if err := db.CheckDatabaseRole(ctx, pool, false); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func dispatchCommand(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, args []string) error {
	if len(args) >= 1 && args[0] == "worker" {
		return runWorkerCommand(ctx, pool, cfg, args[1:])
	}
	if len(args) < 2 {
		return fmt.Errorf("argumen perintah tidak lengkap")
	}
	key := args[0] + " " + args[1]
	rest := args[2:]
	switch key {
	case "admin create":
		return runAdminCreate(ctx, pool, cfg, rest)
	case "admin reset-mfa":
		return runAdminResetMFA(ctx, pool, cfg, rest)
	case "admin reset-password":
		return runAdminResetPassword(ctx, pool, cfg, rest)
	case "iuran generate":
		return runIuranGenerate(ctx, pool, rest)
	case "pdp purge":
		return runPDPPurge(ctx, pool, cfg, rest)
	case "bukti purge":
		return runBuktiPurge(ctx, pool, cfg, rest)
	case "pesanan expire":
		return runPesananExpire(ctx, pool, cfg)
	case "storage migrate-files":
		return runStorageMigrateFiles(ctx, pool, cfg)
	default:
		printUsage()
		return fmt.Errorf("perintah tidak dikenal: %s", key)
	}
}

func runAdminCreate(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, args []string) error {
	fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
	email := fs.String("email", "", "Email admin")
	nama := fs.String("nama", "Admin Koperasi", "Nama lengkap admin")
	hp := fs.String("hp", "", "Nomor HP admin")
	password := fs.String("password", "", "Kata sandi admin (>=10 karakter)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	em := strings.ToLower(strings.TrimSpace(*email))
	hpNorm := security.NormalizePhone(*hp)
	if em == "" || len(*password) < 10 {
		return fmt.Errorf("email dan password (>=10 karakter) wajib diisi")
	}
	kpSet, err := security.NewPurposeKeySet(cfg.AESMasterKey, cfg.AESPreviousKeys)
	if err != nil {
		return err
	}
	kp := kpSet.DB
	blindKey := security.DecodeSecretMaterial(cfg.BlindIndexSecret)
	passHash, err := security.HashPasswordWithCost(*password, 12)
	if err != nil {
		return err
	}
	uid := uuid.New()
	emEnc, _ := kp.EncryptWithAAD([]byte(em), security.FormatAAD("users", "email", uid.String()))
	var hpEnc []byte
	if hpNorm != "" {
		hpEnc, _ = kp.EncryptWithAAD([]byte(hpNorm), security.FormatAAD("users", "hp", uid.String()))
	}
	row, err := db.New(pool).CreateUser(ctx, db.CreateUserParams{
		ID:           response.ToPGUUID(uid),
		EmailEnc:     emEnc,
		EmailHash:    response.ToPGText(security.HashBlindIndex(em, blindKey)),
		HpEnc:        hpEnc,
		HpHash:       response.ToPGText(security.HashBlindIndex(hpNorm, blindKey)),
		NamaLengkap:  strings.TrimSpace(*nama),
		PasswordHash: passHash,
		Peran:        "admin",
		StatusAktif:  true,
	})
	if err != nil {
		return fmt.Errorf("simpan admin: %w", err)
	}
	fmt.Printf("Admin dibuat: id=%s nama=%s (MFA setup wajib saat login pertama)\n", response.FromPGUUID(row.ID), row.NamaLengkap)
	return nil
}

func runIuranGenerate(ctx context.Context, pool *pgxpool.Pool, args []string) error {
	fs := flag.NewFlagSet("iuran generate", flag.ContinueOnError)
	periode := fs.String("periode", time.Now().UTC().Format("2006-01"), "Periode tagihan YYYY-MM")
	if err := fs.Parse(args); err != nil {
		return err
	}
	th, bl, err := iuran.ParsePeriode(*periode)
	if err != nil {
		return err
	}
	res, err := iuran.NewService(pool, nil).GenerateMonthlyBills(ctx, th, bl)
	if err != nil {
		return err
	}
	fmt.Printf("Tagihan %04d-%02d: dibuat=%d, dilewati=%d\n", res.PeriodeTahun, res.PeriodeBulan, res.Dibuat, res.Dilewati)
	return nil
}

func runPDPPurge(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, args []string) error {
	fs := flag.NewFlagSet("pdp purge", flag.ContinueOnError)
	days := fs.Int("days", cfg.PDPRejectedRetentionDays, "Retensi hari pendaftar ditolak")
	if err := fs.Parse(args); err != nil {
		return err
	}
	kpSet, err := security.NewPurposeKeySet(cfg.AESMasterKey, cfg.AESPreviousKeys)
	if err != nil {
		return err
	}
	kp := kpSet.DB
	store, err := pendaftaran.NewKYCFileStore(cfg.StorageKYCDir, kpSet.KYC, 5<<20)
	if err != nil {
		return err
	}
	svc := pendaftaran.NewService(pool, store, pendaftaran.Config{KeyProvider: kp, RetentionDays: *days})
	deleted, err := svc.PurgeExpiredRejected(ctx, *days)
	if err != nil {
		return err
	}
	fmt.Printf("PDP purge selesai: %d pendaftaran ditolak kedaluwarsa dihapus\n", deleted)
	return nil
}

func runPesananExpire(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig) error {
	kpSet, err := security.NewPurposeKeySet(cfg.AESMasterKey, cfg.AESPreviousKeys)
	if err != nil {
		return err
	}
	kp := kpSet.DB
	pesSvc := pesanan.NewService(pool, kp, pesanan.Config{StockHoldMinutes: cfg.StockHoldMinutes, Timezone: cfg.AppTimezone})
	ordCount, err := pesSvc.ExpireUnpaidOrders(ctx)
	if err != nil {
		return err
	}
	holdCount, err := pesSvc.ReleaseExpiredStockHolds(ctx)
	if err != nil {
		return err
	}
	paySvc, err := pembayaran.NewService(pool, pembayaran.Config{PaymentExpiry: cfg.PaymentExpiryDuration})
	if err != nil {
		return err
	}
	payCount, err := paySvc.ExpirePendingPayments(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Kedaluwarsa selesai: pesanan=%d, tahanan_stok=%d, pembayaran=%d\n", ordCount, holdCount, payCount)
	return nil
}

func runWorkerCommand(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, args []string) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	tick := fs.Duration("tick", 60*time.Second, "Interval penjadwalan tugas worker")
	once := fs.Bool("once", false, "Jalankan satu kali lalu keluar")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, err := worker.New(pool, cfg)
	if err != nil {
		return err
	}
	if *once {
		return w.RunOnce(ctx)
	}
	log.Printf("[worker] berjalan dengan interval tick=%s", *tick)
	w.Run(ctx, *tick)
	return nil
}
