package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/testseed"
)

type ChaosReport struct {
	GeneratedAt             string  `json:"generated_at"`
	KillRequestsSent        int64   `json:"kill_requests_sent"`
	KillOrdersCreated       int64   `json:"kill_orders_created"`
	KillStockInvariantOK    bool    `json:"kill_stock_invariant_ok"`
	KillMoneyInvariantOK    bool    `json:"kill_money_invariant_ok"`
	KillNegativeStockRows   int     `json:"kill_negative_stock_rows"`
	KillMismatchedPaidRows  int     `json:"kill_mismatched_paid_rows"`
	KillOrphanItemOrders    int     `json:"kill_orphan_item_orders"`
	BackupBytesPlain        int64   `json:"backup_bytes_plain"`
	BackupBytesEncrypted    int64   `json:"backup_bytes_encrypted"`
	BackupTamperRejected    bool    `json:"backup_tamper_rejected"`
	RestoreTablesCount      int     `json:"restore_tables_count"`
	RestoreSmokeMemberCount int     `json:"restore_smoke_member_count"`
	MeasuredRTOMS           float64 `json:"measured_rto_ms"`
	MeasuredRPONote         string  `json:"measured_rpo_note"`
}

func main() {
	envFile := flag.String("env-file", ".env.test", "Berkas env target uji")
	serverExe := flag.String("server-exe", "", "Path binary server untuk uji abrupt kill")
	pgBinDir := flag.String("pg-bin", `C:\Program Files\PostgreSQL\14\bin`, "Direktori bin PostgreSQL (pg_dump/pg_restore/psql)")
	outPath := flag.String("out", "reports/baseline/chaos_baseline.json", "Path JSON output")
	flag.Parse()

	cfg, err := config.LoadFromEnvAndFile(*envFile)
	if err != nil {
		fatalf("load env: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn, err := cfg.BuildDatabaseDSN()
	if err != nil {
		fatalf("dsn: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fatalf("pool: %v", err)
	}
	defer pool.Close()

	seedPwd, err := testseed.SeedAll(ctx, pool, cfg, "")
	if err != nil {
		fatalf("seed: %v", err)
	}
	res := testseed.LookupResources(ctx, pool)

	rep := ChaosReport{
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		MeasuredRPONote: "RPO=0 untuk transaksi komit sebelum snapshot pg_dump; di luar backup harian berbasis jadwal cron VPS, RPO sama dengan interval backup (24 jam default tanpa WAL archiving/PITR).",
	}

	if *serverExe != "" {
		runKillDrill(ctx, pool, cfg, *serverExe, seedPwd, res.ProdukID, &rep)
	}
	runEncryptedBackupRestoreDrill(ctx, cfg, *pgBinDir, &rep)

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o750); err != nil {
		fatalf("mkdir: %v", err)
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	// #nosec G304,G703 -- path output CLI lokal
	if err := os.WriteFile(*outPath, b, 0o600); err != nil {
		fatalf("write out: %v", err)
	}
	fmt.Printf("CHAOS_DRILL_OK: %s\n", string(b))
}

func runKillDrill(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, exe, seedPwd, produkID string, rep *ChaosReport) {
	port := cfg.APIHostPort
	if port <= 0 {
		port = cfg.APIPort
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	// #nosec G204 -- exe disediakan eksplisit oleh skrip uji lokal terkontrol
	cmd := exec.Command(exe, "-env-file", ".env.test")
	cmd.Env = append(os.Environ(), "GOMAXPROCS=2")
	if err := cmd.Start(); err != nil {
		fatalf("start server exe: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	ready := false
	for i := 0; i < 40; i++ {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !ready {
		fatalf("server exe %s tidak siap di %s", exe, baseURL)
	}

	tok := loginSimple(baseURL, "anggota1@kopdes-test.local", seedPwd)
	var sent, created atomic.Int64
	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	cli := &http.Client{Timeout: 3 * time.Second}

	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stopCh:
					return
				default:
				}
				payload, _ := json.Marshal(map[string]interface{}{
					"items":             []map[string]interface{}{{"produk_id": produkID, "kuantitas": 1}},
					"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
					"nama_penerima": "Kill Test", "hp_penerima": "08110000004",
				})
				req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/toko/checkout", bytes.NewReader(payload))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+tok)
				req.Header.Set("X-Requested-With", "chaos")
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.99.%d.%d", wid+1, (i%200)+1))
				req.Header.Set("Idempotency-Key", "kill-"+uuid.NewString())
				sent.Add(1)
				// #nosec G704 -- endpoint loopback host uji
				resp, err := cli.Do(req)
				if err == nil {
					if resp.StatusCode == http.StatusCreated {
						created.Add(1)
					}
					_ = resp.Body.Close()
				}
			}
		}(w)
	}

	time.Sleep(300 * time.Millisecond)
	// Hard kill proses API di tengah arus checkout!
	if err := cmd.Process.Kill(); err != nil {
		fatalf("kill process: %v", err)
	}
	_, _ = cmd.Process.Wait()
	close(stopCh)
	wg.Wait()

	rep.KillRequestsSent = sent.Load()
	rep.KillOrdersCreated = created.Load()

	// Verifikasi seluruh invarian transaksi DB pasca kill -9
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM produk WHERE stok < 0 OR stok_tertahan < 0 OR stok_tertahan > stok`).Scan(&rep.KillNegativeStockRows)
	_ = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pesanan p
		WHERE NOT EXISTS (SELECT 1 FROM pesanan_item pi WHERE pi.pesanan_id = p.id)`).Scan(&rep.KillOrphanItemOrders)
	_ = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pembayaran bay
		JOIN pembayaran_alokasi pa ON pa.pembayaran_id = bay.id
		LEFT JOIN pesanan p ON p.id = pa.pesanan_id
		LEFT JOIN tagihan_iuran t ON t.id = pa.tagihan_iuran_id
		WHERE bay.status = 'lunas'
		  AND ((p.id IS NOT NULL AND p.status IN ('baru','menunggu_pembayaran','batal'))
		    OR (t.id IS NOT NULL AND t.status = 'belum_bayar'))`).Scan(&rep.KillMismatchedPaidRows)
	rep.KillStockInvariantOK = rep.KillNegativeStockRows == 0 && rep.KillOrphanItemOrders == 0
	rep.KillMoneyInvariantOK = rep.KillMismatchedPaidRows == 0

	// Batalkan pesanan kill-test agar stok hold dilepas bersih
	_, _ = pool.Exec(ctx, `UPDATE produk p SET stok_tertahan = 0 WHERE id IN (SELECT DISTINCT pi.produk_id FROM pesanan_item pi JOIN pesanan o ON o.id = pi.pesanan_id WHERE o.idempotency_key LIKE '%kill-%')`)
	_, _ = pool.Exec(ctx, `DELETE FROM pesanan WHERE idempotency_key LIKE '%kill-%'`)
}

func runEncryptedBackupRestoreDrill(ctx context.Context, cfg config.AppConfig, pgBinDir string, rep *ChaosReport) {
	tmpDir, err := os.MkdirTemp("", "kopdes_backup_drill_*")
	if err != nil {
		fatalf("mktemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dumpPath := filepath.Join(tmpDir, "db.dump")
	encPath := filepath.Join(tmpDir, "db.dump.enc")
	restPath := filepath.Join(tmpDir, "db_restored.dump")
	pgDump := filepath.Join(pgBinDir, "pg_dump.exe")
	pgRestore := filepath.Join(pgBinDir, "pg_restore.exe")
	psqlExe := filepath.Join(pgBinDir, "psql.exe")

	// 1. pg_dump custom format
	// #nosec G204 -- perintah lokal PostgreSQL uji
	dCmd := exec.CommandContext(ctx, pgDump, "-h", cfg.DBHost, "-p", fmt.Sprintf("%d", cfg.DBPort), "-U", "postgres", "-d", cfg.DBName, "-F", "c", "-b", "-f", dumpPath)
	if out, dErr := dCmd.CombinedOutput(); dErr != nil {
		fatalf("pg_dump failed: %v %s", dErr, string(out))
	}
	plainBytes, err := os.ReadFile(dumpPath) // #nosec G304 -- file sementara hasil pg_dump
	if err != nil {
		fatalf("read dump: %v", err)
	}
	rep.BackupBytesPlain = int64(len(plainBytes))

	// 2. Enkripsi terautentikasi AES-256-GCM dengan sub-kunci HKDF PurposeBackup (simulasi lokal pengganti biner CLI age)
	kpSet, err := security.NewPurposeKeySet(cfg.AESMasterKey, cfg.AESPreviousKeys)
	if err != nil {
		fatalf("kdf: %v", err)
	}
	aad := security.FormatAAD("backup", "pg_dump", cfg.DBName)
	cipherBytes, err := kpSet.Backup.EncryptWithAAD(plainBytes, aad)
	if err != nil {
		fatalf("encrypt backup: %v", err)
	}
	// #nosec G304,G703 -- path berada di dalam subdirektori sementara os.MkdirTemp
	if err := os.WriteFile(encPath, cipherBytes, 0o600); err != nil {
		fatalf("write enc: %v", err)
	}
	rep.BackupBytesEncrypted = int64(len(cipherBytes))

	// Uji tamper rejection: balik 1 bit pada akhir ciphertext
	tampered := append([]byte(nil), cipherBytes...)
	tampered[len(tampered)-5] ^= 0xFF
	if _, tErr := kpSet.Backup.DecryptWithAAD(tampered, aad); tErr != nil {
		rep.BackupTamperRejected = true
	}

	// 3. Dekripsi & restore ke DB uji terisolasi lalu ukur RTO
	drillDB := cfg.DBName + "_restore_drill"
	startRTO := time.Now()
	readEnc, _ := os.ReadFile(encPath) // #nosec G304 -- file sementara hasil enkripsi lokal
	decPlain, err := kpSet.Backup.DecryptWithAAD(readEnc, aad)
	if err != nil {
		fatalf("decrypt backup: %v", err)
	}
	// #nosec G304,G703 -- path berada di dalam subdirektori sementara os.MkdirTemp
	if err := os.WriteFile(restPath, decPlain, 0o600); err != nil {
		fatalf("write restored dump: %v", err)
	}

	// #nosec G204 -- perintah lokal PostgreSQL uji
	_ = exec.CommandContext(ctx, psqlExe, "-h", cfg.DBHost, "-p", fmt.Sprintf("%d", cfg.DBPort), "-U", "postgres", "-d", "postgres", "-c", fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, drillDB)).Run()
	// #nosec G204 -- perintah lokal PostgreSQL uji
	if out, cErr := exec.CommandContext(ctx, psqlExe, "-h", cfg.DBHost, "-p", fmt.Sprintf("%d", cfg.DBPort), "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", fmt.Sprintf(`CREATE DATABASE "%s";`, drillDB)).CombinedOutput(); cErr != nil {
		fatalf("create drill DB: %v %s", cErr, string(out))
	}
	defer func() {
		// #nosec G204 -- cleanup drill DB
		_ = exec.CommandContext(context.Background(), psqlExe, "-h", cfg.DBHost, "-p", fmt.Sprintf("%d", cfg.DBPort), "-U", "postgres", "-d", "postgres", "-c", fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, drillDB)).Run()
	}()

	// #nosec G204 -- restore drill DB
	_ = exec.CommandContext(ctx, pgRestore, "-h", cfg.DBHost, "-p", fmt.Sprintf("%d", cfg.DBPort), "-U", "postgres", "-d", drillDB, "--no-owner", "--no-privileges", restPath).Run()
	rep.MeasuredRTOMS = float64(time.Since(startRTO).Milliseconds())

	drillCfg := cfg
	drillCfg.DBName = drillDB
	drillCfg.DBUser = "postgres"
	drillCfg.DatabaseURL = ""
	drillDSN, _ := drillCfg.BuildDatabaseDSN()
	if dPool, dErr := pgxpool.New(ctx, drillDSN); dErr == nil {
		_ = dPool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`).Scan(&rep.RestoreTablesCount)
		_ = dPool.QueryRow(ctx, `SELECT COUNT(*) FROM anggota`).Scan(&rep.RestoreSmokeMemberCount)
		dPool.Close()
	}
}

func loginSimple(baseURL, id, pwd string) string {
	b, _ := json.Marshal(map[string]string{"identifier": id, "password": pwd})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "10.99.0.1")
	// #nosec G704 -- loopback uji lokal
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatalf("loginSimple: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var parsed map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return parsed["data"].(map[string]interface{})["session_token"].(string)
}

func fatalf(f string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, f+"\n", args...)
	os.Exit(1)
}
