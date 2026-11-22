package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/testseed"
)

type ScenarioResult struct {
	Name            string  `json:"name"`
	ConcurrencyPeak int     `json:"concurrency_peak"`
	DurationSeconds float64 `json:"duration_seconds"`
	TotalRequests   int64   `json:"total_requests"`
	ReadRequests    int64   `json:"read_requests"`
	WriteRequests   int64   `json:"write_requests"`
	UnexpectedErrs  int64   `json:"unexpected_errors"`
	RateLimited429  int64   `json:"rate_limited_429"`
	Throttled503    int64   `json:"throttled_503"`
	ErrorRate       float64 `json:"error_rate"`
	RPS             float64 `json:"rps"`
	ReadP50MS       float64 `json:"read_p50_ms"`
	ReadP95MS       float64 `json:"read_p95_ms"`
	ReadP99MS       float64 `json:"read_p99_ms"`
	WriteP50MS      float64 `json:"write_p50_ms"`
	WriteP95MS      float64 `json:"write_p95_ms"`
	WriteP99MS      float64 `json:"write_p99_ms"`
	PGMaxConns      int     `json:"pg_max_conns"`
	PGMaxWaiting    int     `json:"pg_max_waiting_locks"`
	SLOReadPass     bool    `json:"slo_read_pass"`
	SLOWritePass    bool    `json:"slo_write_pass"`
	SLOErrorPass    bool    `json:"slo_error_pass"`
}

type Report struct {
	GeneratedAt   string           `json:"generated_at"`
	BaseURL       string           `json:"base_url"`
	AppEnv        string           `json:"app_env"`
	SLOReadP95MS  float64          `json:"slo_read_p95_ms"`
	SLOWriteP95MS float64          `json:"slo_write_p95_ms"`
	SLOMaxErrRate float64          `json:"slo_max_error_rate"`
	SoakNote      string           `json:"soak_note"`
	Scenarios     []ScenarioResult `json:"scenarios"`
}

type vuSession struct {
	email    string
	password string
	token    string
}

type harness struct {
	cfg        config.AppConfig
	pool       *pgxpool.Pool
	client     *http.Client
	baseURL    string
	adminToken string
	memberToks []string
	loadUsers  []vuSession
	produkID   string
	seedPwd    string
}

func main() {
	envFile := flag.String("env-file", ".env.test", "Path berkas env uji")
	baseURLFlag := flag.String("base-url", "", "Override URL API target (bawaan dari API_HOST_PORT)")
	scenariosFlag := flag.String("scenarios", "smoke,load,stress,spike,soak", "Daftar skenario dipisah koma")
	soakDur := flag.Duration("soak-duration", 2*time.Hour, "Durasi uji soak (mis. 2h atau 120s)")
	sloReadMS := flag.Float64("slo-read-p95-ms", 300, "Target SLO p95 baca (ms)")
	sloWriteMS := flag.Float64("slo-write-p95-ms", 800, "Target SLO p95 tulis (ms)")
	sloErr := flag.Float64("slo-max-error", 0.005, "Target SLO rasio error maksimal")
	outPath := flag.String("out", "reports/baseline/perf_baseline.json", "Berkas output JSON laporan")
	flag.Parse()

	cfg, err := config.LoadFromEnvAndFile(*envFile)
	if err != nil {
		fatalf("gagal muat %s: %v", *envFile, err)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(*baseURLFlag), "/")
	if baseURL == "" {
		port := cfg.APIHostPort
		if port <= 0 {
			port = cfg.APIPort
		}
		baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	dsn, err := cfg.BuildDatabaseDSN()
	if err != nil {
		cancel()
		fatalf("BuildDatabaseDSN: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		cancel()
		fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	h := &harness{
		cfg:     cfg,
		pool:    pool,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        512,
				MaxIdleConnsPerHost: 512,
				IdleConnTimeout:     60 * time.Second,
			},
		},
	}
	if err := h.bootstrap(ctx); err != nil {
		cancel()
		fatalf("bootstrap gagal: %v", err)
	}
	cancel()

	report := Report{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		BaseURL:       baseURL,
		AppEnv:        cfg.AppEnv,
		SLOReadP95MS:  *sloReadMS,
		SLOWriteP95MS: *sloWriteMS,
		SLOMaxErrRate: *sloErr,
		SoakNote:      fmt.Sprintf("Durasi soak berjalan %s (standar penuh 2h via -soak-duration=2h atau tests/perf/k6_mixed.js)", soakDur.String()),
	}

	for _, name := range strings.Split(*scenariosFlag, ",") {
		scName := strings.ToLower(strings.TrimSpace(name))
		if scName == "" {
			continue
		}
		fmt.Printf("[LOADTEST] Menjalankan skenario %s terhadap %s ...\n", scName, baseURL)
		res := h.runScenario(scName, *soakDur, *sloReadMS, *sloWriteMS, *sloErr)
		report.Scenarios = append(report.Scenarios, res)
		fmt.Printf("[LOADTEST] %s selesai: req=%d rps=%.1f read_p95=%.1fms write_p95=%.1fms err=%.4f pg_max=%d locks=%d\n",
			res.Name, res.TotalRequests, res.RPS, res.ReadP95MS, res.WriteP95MS, res.ErrorRate, res.PGMaxConns, res.PGMaxWaiting)
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o750); err != nil {
		fatalf("mkdir output: %v", err)
	}
	rawJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatalf("marshal json: %v", err)
	}
	// #nosec G304,G703 -- path output CLI lokal untuk laporan uji beban
	if err := os.WriteFile(*outPath, rawJSON, 0o600); err != nil {
		fatalf("tulis %s: %v", *outPath, err)
	}
	fmt.Printf("[LOADTEST] Laporan tersimpan ke %s\n", *outPath)
}

func (h *harness) bootstrap(ctx context.Context) error {
	seedPwd, err := testseed.SeedAll(ctx, h.pool, h.cfg, "")
	if err != nil {
		return err
	}
	h.seedPwd = seedPwd

	adminSecret := testseed.DeterministicTOTPSecret(h.cfg, "admin@kopdes-test.local")
	h.adminToken, err = h.loginHTTP("admin@kopdes-test.local", seedPwd, adminSecret, "10.250.0.1")
	if err != nil {
		return fmt.Errorf("login admin: %w", err)
	}

	// Buat / ambil produk beban berstok tinggi agar uji stres tidak langsung kehabisan stok
	sku := "LOADTEST-BERAS-5KG"
	_, body, err := h.doJSON(http.MethodGet, "/api/v1/katalog/produk?q="+sku, h.adminToken, nil, "10.250.0.1", nil)
	if err == nil {
		if list, ok := body["data"].([]interface{}); ok && len(list) > 0 {
			h.produkID = list[0].(map[string]interface{})["id"].(string)
		}
	}
	if h.produkID == "" {
		st, pBody, pErr := h.doJSON(http.MethodPost, "/api/v1/katalog/produk", h.adminToken, map[string]interface{}{
			"sku": sku, "nama": "Beras Loadtest 5Kg", "satuan": "sak",
			"harga_umum": 70000, "harga_anggota": 65000, "stok": 1000000, "status": "aktif",
		}, "10.250.0.1", nil)
		if st != http.StatusCreated {
			return fmt.Errorf("create loadtest produk status=%d err=%v body=%v", st, pErr, pBody)
		}
		h.produkID = pBody["data"].(map[string]interface{})["id"].(string)
	}

	// Siapkan 20 akun anggota tambahan agar kuota pending QRIS per anggota tidak langsung jenuh oleh 1 akun
	kpSet, err := security.NewPurposeKeySet(h.cfg.AESMasterKey, h.cfg.AESPreviousKeys)
	if err != nil {
		return err
	}
	blindKey := security.DecodeSecretMaterial(h.cfg.BlindIndexSecret)
	passHash, _ := security.HashPasswordWithCost(seedPwd, 10)
	q := db.New(h.pool)

	for i := 1; i <= 20; i++ {
		email := fmt.Sprintf("loadvu%02d@kopdes-test.local", i)
		hp := fmt.Sprintf("08190000%04d", i)
		emHash := security.HashBlindIndex(email, blindKey)
		if _, err := q.GetUserByEmailHash(ctx, response.ToPGText(emHash)); err != nil {
			uid := uuid.New()
			emEnc, _ := kpSet.DB.EncryptWithAAD([]byte(email), security.FormatAAD("users", "email", uid.String()))
			hpEnc, _ := kpSet.DB.EncryptWithAAD([]byte(hp), security.FormatAAD("users", "hp", uid.String()))
			uRow, cErr := q.CreateUser(ctx, db.CreateUserParams{
				ID: response.ToPGUUID(uid), EmailEnc: emEnc, EmailHash: response.ToPGText(emHash),
				HpEnc: hpEnc, HpHash: response.ToPGText(security.HashBlindIndex(hp, blindKey)),
				NamaLengkap: fmt.Sprintf("Load VU %02d", i), PasswordHash: passHash,
				Peran: "anggota", StatusAktif: true,
			})
			if cErr == nil {
				aid := uuid.New()
				nik := fmt.Sprintf("329901010101%04d", i)
				nikEnc, _ := kpSet.DB.EncryptWithAAD([]byte(nik), security.FormatAAD("anggota", "nik", aid.String()))
				aHPEnc, _ := kpSet.DB.EncryptWithAAD([]byte(hp), security.FormatAAD("anggota", "hp", aid.String()))
				alEnc, _ := kpSet.DB.EncryptWithAAD([]byte("Dusun Loadtest"), security.FormatAAD("anggota", "alamat", aid.String()))
				_, _ = q.CreateAnggota(ctx, db.CreateAnggotaParams{
					ID: response.ToPGUUID(aid), UserID: uRow.ID, NomorAnggota: fmt.Sprintf("KMP-LOAD-%04d", i),
					NamaLengkap: uRow.NamaLengkap, NikEnc: nikEnc, NikHash: security.HashBlindIndex(nik, blindKey),
					NikKeyVersion: kpSet.DB.KeyID(), NikMasked: security.MaskNIK(nik),
					HpEnc: aHPEnc, HpMasked: security.MaskPhone(hp), AlamatEnc: alEnc, Status: "aktif",
				})
			}
		}
		tok, lErr := h.loginHTTP(email, seedPwd, "", fmt.Sprintf("10.251.0.%d", i))
		if lErr != nil {
			return fmt.Errorf("login %s: %w", email, lErr)
		}
		h.memberToks = append(h.memberToks, tok)
		h.loadUsers = append(h.loadUsers, vuSession{email: email, password: seedPwd, token: tok})
	}
	return nil
}

func (h *harness) runScenario(name string, soakDur time.Duration, sloRead, sloWrite, sloErr float64) ScenarioResult {
	var peakVUs int
	var runPhase func(ctx context.Context, stopAt time.Time, workerFn func(vu int, iter int))

	switch name {
	case "smoke":
		peakVUs = 5
		runPhase = func(ctx context.Context, _ time.Time, fn func(int, int)) {
			runFixedVUs(ctx, 5, 10*time.Second, fn)
		}
	case "load":
		peakVUs = 100
		runPhase = func(ctx context.Context, _ time.Time, fn func(int, int)) {
			runFixedVUs(ctx, 100, 25*time.Second, fn)
		}
	case "stress":
		peakVUs = 500
		runPhase = func(ctx context.Context, _ time.Time, fn func(int, int)) {
			for _, stage := range []struct {
				vus int
				dur time.Duration
			}{{50, 8 * time.Second}, {150, 8 * time.Second}, {300, 8 * time.Second}, {500, 8 * time.Second}} {
				runFixedVUs(ctx, stage.vus, stage.dur, fn)
			}
		}
	case "spike":
		peakVUs = 250
		runPhase = func(ctx context.Context, _ time.Time, fn func(int, int)) {
			runFixedVUs(ctx, 20, 5*time.Second, fn)
			runFixedVUs(ctx, 250, 10*time.Second, fn)
			runFixedVUs(ctx, 20, 5*time.Second, fn)
		}
	case "soak":
		peakVUs = 50
		runPhase = func(ctx context.Context, _ time.Time, fn func(int, int)) {
			runFixedVUs(ctx, 50, soakDur, fn)
		}
	default:
		peakVUs = 20
		runPhase = func(ctx context.Context, _ time.Time, fn func(int, int)) {
			runFixedVUs(ctx, 20, 10*time.Second, fn)
		}
	}

	var mu sync.Mutex
	readLats := make([]float64, 0, 8192)
	writeLats := make([]float64, 0, 2048)
	var totalReq, readReq, writeReq, errCount, rl429, th503 atomic.Int64
	var maxPGConns, maxWaiting atomic.Int64

	pollCtx, pollCancel := context.WithCancel(context.Background())
	var pollWG sync.WaitGroup
	pollWG.Add(1)
	go func() {
		defer pollWG.Done()
		tk := time.NewTicker(500 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-pollCtx.Done():
				return
			case <-tk.C:
				var ac, wt int64
				_ = h.pool.QueryRow(pollCtx, `SELECT COUNT(*)::bigint FROM pg_stat_activity WHERE datname = current_database()`).Scan(&ac)
				_ = h.pool.QueryRow(pollCtx, `SELECT COUNT(*)::bigint FROM pg_locks WHERE NOT granted`).Scan(&wt)
				if ac > maxPGConns.Load() {
					maxPGConns.Store(ac)
				}
				if wt > maxWaiting.Load() {
					maxWaiting.Store(wt)
				}
			}
		}
	}()

	start := time.Now()
	runPhase(context.Background(), start, func(vu, iter int) {
		ip := fmt.Sprintf("10.%d.%d.%d", 10+(vu/250)%200, (vu%250)+1, (iter%240)+1)
		memTok := h.memberToks[vu%len(h.memberToks)]
		roll := (vu + iter) % 20
		if roll < 15 {
			// Mixed reads (~75%): public config, katalog, tagihan, pesanan, pembayaran, laporan
			paths := []struct {
				path string
				tok  string
			}{
				{"/api/v1/public/config", ""},
				{"/api/v1/katalog/produk", memTok},
				{"/api/v1/iuran/tagihan", memTok},
				{"/api/v1/pesanan", memTok},
				{"/api/v1/pembayaran", memTok},
				{"/api/v1/laporan?jenis=pembayaran", h.adminToken},
			}
			tPick := paths[(vu+iter)%len(paths)]
			t0 := time.Now()
			st, _, reqErr := h.doJSON(http.MethodGet, tPick.path, tPick.tok, nil, ip, nil)
			ms := float64(time.Since(t0).Microseconds()) / 1000.0
			totalReq.Add(1)
			readReq.Add(1)
			mu.Lock()
			readLats = append(readLats, ms)
			mu.Unlock()
			recordOutcome(st, reqErr, &errCount, &rl429, &th503)
			return
		}
		if roll < 19 {
			// Write: checkout + cancel (uji kunci produk & pesanan transaksional tanpa memenuhi antrean global QRIS)
			t0 := time.Now()
			st, b, reqErr := h.doJSON(http.MethodPost, "/api/v1/toko/checkout", memTok, map[string]interface{}{
				"items":             []map[string]interface{}{{"produk_id": h.produkID, "kuantitas": 1}},
				"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
				"nama_penerima": "Load Tester", "hp_penerima": "08110000004",
			}, ip, map[string]string{"Idempotency-Key": "lt-ord-" + uuid.NewString()})
			ms := float64(time.Since(t0).Microseconds()) / 1000.0
			totalReq.Add(1)
			writeReq.Add(1)
			mu.Lock()
			writeLats = append(writeLats, ms)
			mu.Unlock()
			recordOutcome(st, reqErr, &errCount, &rl429, &th503)
			if st == http.StatusCreated {
				if data, ok := b["data"].(map[string]interface{}); ok {
					if oid, ok := data["id"].(string); ok {
						_, _, _ = h.doJSON(http.MethodPatch, "/api/v1/pesanan/"+oid+"/status", memTok, map[string]string{
							"status": "batal", "catatan": "loadtest cleanup",
						}, ip, nil)
					}
				}
			}
			return
		}
		// Write (~5%): checkout + QRIS + upload PNG bukti + admin verify agar pipeline bukti terenkripsi terukur
		t0 := time.Now()
		st, b, reqErr := h.doJSON(http.MethodPost, "/api/v1/toko/checkout", memTok, map[string]interface{}{
			"items":             []map[string]interface{}{{"produk_id": h.produkID, "kuantitas": 1}},
			"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
			"nama_penerima": "Load QRIS", "hp_penerima": "08110000004",
		}, ip, map[string]string{"Idempotency-Key": "lt-qord-" + uuid.NewString()})
		ms := float64(time.Since(t0).Microseconds()) / 1000.0
		totalReq.Add(1)
		writeReq.Add(1)
		mu.Lock()
		writeLats = append(writeLats, ms)
		mu.Unlock()
		recordOutcome(st, reqErr, &errCount, &rl429, &th503)
		if st == http.StatusCreated {
			if ordData, ok := b["data"].(map[string]interface{}); ok {
				oid, _ := ordData["id"].(string)
				h.driveQRISPayAndVerify(memTok, oid, ip, &writeLats, &mu, &totalReq, &writeReq, &errCount, &rl429, &th503)
			}
		}
	})
	elapsed := time.Since(start).Seconds()
	pollCancel()
	pollWG.Wait()

	sort.Float64s(readLats)
	sort.Float64s(writeLats)
	tot := totalReq.Load()
	errN := errCount.Load()
	errRate := 0.0
	if tot > 0 {
		errRate = float64(errN) / float64(tot)
	}
	r95 := percentile(readLats, 0.95)
	w95 := percentile(writeLats, 0.95)
	return ScenarioResult{
		Name:            name,
		ConcurrencyPeak: peakVUs,
		DurationSeconds: round2(elapsed),
		TotalRequests:   tot,
		ReadRequests:    readReq.Load(),
		WriteRequests:   writeReq.Load(),
		UnexpectedErrs:  errN,
		RateLimited429:  rl429.Load(),
		Throttled503:    th503.Load(),
		ErrorRate:       round4(errRate),
		RPS:             round2(float64(tot) / math.Max(0.1, elapsed)),
		ReadP50MS:       round2(percentile(readLats, 0.50)),
		ReadP95MS:       round2(r95),
		ReadP99MS:       round2(percentile(readLats, 0.99)),
		WriteP50MS:      round2(percentile(writeLats, 0.50)),
		WriteP95MS:      round2(w95),
		WriteP99MS:      round2(percentile(writeLats, 0.99)),
		PGMaxConns:      int(maxPGConns.Load()),
		PGMaxWaiting:    int(maxWaiting.Load()),
		SLOReadPass:     r95 < sloRead,
		SLOWritePass:    w95 < sloWrite,
		SLOErrorPass:    errRate < sloErr,
	}
}

func (h *harness) driveQRISPayAndVerify(
	memTok, ordID, ip string,
	writeLats *[]float64, mu *sync.Mutex,
	totalReq, writeReq, errCount, rl429, th503 *atomic.Int64,
) {
	t0 := time.Now()
	pSt, pBody, pErr := h.doJSON(http.MethodPost, "/api/v1/pembayaran", memTok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": ordID,
	}, ip, map[string]string{"Idempotency-Key": "lt-pay-" + uuid.NewString()})
	ms := float64(time.Since(t0).Microseconds()) / 1000.0
	totalReq.Add(1)
	writeReq.Add(1)
	mu.Lock()
	*writeLats = append(*writeLats, ms)
	mu.Unlock()
	recordOutcome(pSt, pErr, errCount, rl429, th503)
	if pSt != http.StatusCreated {
		_, _, _ = h.doJSON(http.MethodPatch, "/api/v1/pesanan/"+ordID+"/status", memTok, map[string]string{
			"status": "batal", "catatan": "qris slot full cleanup",
		}, ip, nil)
		return
	}
	payObj := pBody["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})
	payID := payObj["id"].(string)
	nomTotal := int64(payObj["nominal_total"].(float64))

	// Upload PNG bukti acak agar unik & dienkripsi ke disk
	t1 := time.Now()
	uSt, uErr := h.uploadBuktiPNG(memTok, payID, ip)
	msU := float64(time.Since(t1).Microseconds()) / 1000.0
	totalReq.Add(1)
	writeReq.Add(1)
	mu.Lock()
	*writeLats = append(*writeLats, msU)
	mu.Unlock()
	recordOutcome(uSt, uErr, errCount, rl429, th503)

	// Segera verifikasi setuju oleh admin agar slot pending kembali bebas untuk VU lain
	t2 := time.Now()
	vSt, _, vErr := h.doJSON(http.MethodPost, "/api/v1/pembayaran/"+payID+"/verifikasi", h.adminToken, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nomTotal, "alasan": "Loadtest auto verify",
	}, "10.250.0.1", nil)
	msV := float64(time.Since(t2).Microseconds()) / 1000.0
	totalReq.Add(1)
	writeReq.Add(1)
	mu.Lock()
	*writeLats = append(*writeLats, msV)
	mu.Unlock()
	recordOutcome(vSt, vErr, errCount, rl429, th503)
}

func runFixedVUs(parent context.Context, vus int, dur time.Duration, fn func(vu, iter int)) {
	ctx, cancel := context.WithTimeout(parent, dur)
	defer cancel()
	var wg sync.WaitGroup
	for vu := 0; vu < vus; vu++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			iter := 0
			for ctx.Err() == nil {
				fn(id, iter)
				iter++
			}
		}(vu)
	}
	wg.Wait()
}

func recordOutcome(status int, err error, errCount, rl429, th503 *atomic.Int64) {
	if err != nil || status >= 500 || status == 0 {
		if status == http.StatusServiceUnavailable {
			th503.Add(1)
			return
		}
		errCount.Add(1)
		return
	}
	if status == http.StatusTooManyRequests {
		rl429.Add(1)
	}
}

func (h *harness) loginHTTP(email, password, totpSecret, ip string) (string, error) {
	st, body, lErr := h.doJSON(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": email, "password": password,
	}, ip, nil)
	if st != http.StatusOK {
		return "", fmt.Errorf("status=%d err=%v body=%v", st, lErr, body)
	}
	data := body["data"].(map[string]interface{})
	tok, _ := data["session_token"].(string)
	if reqMFA, _ := data["mfa_required"].(bool); reqMFA && totpSecret != "" {
		code, _ := security.GenerateTOTPCode(totpSecret, time.Now().UTC())
		mSt, mBody, mErr := h.doJSON(http.MethodPost, "/api/v1/auth/mfa/verify", tok, map[string]string{"code": code}, ip, nil)
		if mSt != http.StatusOK {
			// coba offset +30s jika window TOTP baru saja berpindah / terpakai
			code2, _ := security.GenerateTOTPCode(totpSecret, time.Now().UTC().Add(30*time.Second))
			mSt2, mBody2, mErr2 := h.doJSON(http.MethodPost, "/api/v1/auth/mfa/verify", tok, map[string]string{"code": code2}, ip, nil)
			if mSt2 != http.StatusOK {
				return "", fmt.Errorf("mfa verify status=%d (%v/%v) / %d (%v/%v)", mSt, mBody, mErr, mSt2, mBody2, mErr2)
			}
		}
	}
	return tok, nil
}

func (h *harness) doJSON(method, path, token string, body interface{}, ip string, extra map[string]string) (int, map[string]interface{}, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.baseURL+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Requested-With", "loadtest")
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	// #nosec G704 -- URL berasal dari loopback host target uji terkonfigurasi API_HOST_PORT
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed map[string]interface{}
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed, nil
}

func (h *harness) uploadBuktiPNG(token, payID, ip string) (int, error) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var rb [3]byte
	_, _ = rand.Read(rb[:])
	img.Set(0, 0, color.RGBA{R: rb[0], G: rb[1], B: rb[2], A: 255})
	var imgBuf bytes.Buffer
	_ = png.Encode(&imgBuf, img)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("pembayaran_id", payID)
	fw, _ := mw.CreateFormFile("file", "bukti.png")
	_, _ = fw.Write(imgBuf.Bytes())
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, h.baseURL+"/api/v1/pembayaran", &body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Requested-With", "loadtest")
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	// #nosec G704 -- URL berasal dari loopback host target uji terkonfigurasi API_HOST_PORT
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
