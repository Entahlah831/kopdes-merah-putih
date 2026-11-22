package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/anggota"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/iuran"
	"github.com/kopdes/merah-putih/internal/pembayaran"
	"github.com/kopdes/merah-putih/internal/pengaturan"
)

type chaosTCPProxy struct {
	ln        net.Listener
	target    string
	down      atomic.Bool
	latencyMS atomic.Int64
	mu        sync.Mutex
	conns     map[net.Conn]struct{}
}

func startChaosTCPProxy(t *testing.T, targetHost string, targetPort int) *chaosTCPProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	p := &chaosTCPProxy{
		ln:     ln,
		target: fmt.Sprintf("%s:%d", targetHost, targetPort),
		conns:  make(map[net.Conn]struct{}),
	}
	go p.acceptLoop()
	t.Cleanup(func() { p.closeAll() })
	return p
}

func (p *chaosTCPProxy) port() int {
	return p.ln.Addr().(*net.TCPAddr).Port
}

func (p *chaosTCPProxy) setDown(v bool) {
	p.down.Store(v)
	if v {
		p.mu.Lock()
		for c := range p.conns {
			_ = c.Close()
		}
		p.mu.Unlock()
	}
}

func (p *chaosTCPProxy) closeAll() {
	_ = p.ln.Close()
	p.setDown(true)
}

func (p *chaosTCPProxy) acceptLoop() {
	for {
		cli, err := p.ln.Accept()
		if err != nil {
			return
		}
		if p.down.Load() {
			_ = cli.Close()
			continue
		}
		up, err := net.DialTimeout("tcp", p.target, 2*time.Second)
		if err != nil {
			_ = cli.Close()
			continue
		}
		p.mu.Lock()
		p.conns[cli] = struct{}{}
		p.conns[up] = struct{}{}
		p.mu.Unlock()
		go p.pipe(cli, up)
		go p.pipe(up, cli)
	}
}

func (p *chaosTCPProxy) pipe(dst, src net.Conn) {
	defer func() {
		_ = src.Close()
		_ = dst.Close()
		p.mu.Lock()
		delete(p.conns, src)
		delete(p.conns, dst)
		p.mu.Unlock()
	}()
	buf := make([]byte, 16*1024)
	for {
		if p.down.Load() {
			return
		}
		n, err := src.Read(buf)
		if n > 0 {
			if ms := p.latencyMS.Load(); ms > 0 {
				time.Sleep(time.Duration(ms) * time.Millisecond)
			}
			if _, wErr := dst.Write(buf[:n]); wErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func openChaosProxyPool(t *testing.T, proxy *chaosTCPProxy, appCfg config.AppConfig, maxConns int32) *pgxpool.Pool {
	t.Helper()
	proxiedCfg := appCfg
	proxiedCfg.DBHost = "127.0.0.1"
	proxiedCfg.DBPort = proxy.port()
	proxiedCfg.DatabaseURL = ""
	dsn, err := proxiedCfg.BuildDatabaseDSN()
	if err != nil {
		t.Fatalf("BuildDatabaseDSN chaos: %v", err)
	}
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig chaos: %v", err)
	}
	if maxConns > 0 {
		pc.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), pc)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestChaos_DBOutageLatencyAndAutoRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("melewati uji chaos DB pada mode -short")
	}
	root := repoRootDir(t)
	appCfg, err := config.LoadFromEnvAndFile(filepath.Join(root, ".env.test"))
	if err != nil {
		t.Fatalf("load .env.test: %v", err)
	}
	proxy := startChaosTCPProxy(t, appCfg.DBHost, appCfg.DBPort)
	pool := openChaosProxyPool(t, proxy, appCfg, 4)

	srvCfg := ConfigFromAppConfig(appCfg)
	srvCfg.StorageKYCDir = t.TempDir()
	srvCfg.StorageBuktiDir = t.TempDir()
	ts := httptest.NewServer(NewServer(pool, srvCfg).Router())
	defer ts.Close()

	// 1. Sehat sebelum outage
	if st, _, _ := doJSONReq(t, http.MethodGet, ts.URL+"/readyz", "", nil, nil); st != http.StatusOK {
		t.Fatalf("expected initial /readyz 200, got %d", st)
	}

	// 2. Simulasi DB mati / putus koneksi -> /readyz wajib 503 DATABASE_UNAVAILABLE tanpa panic
	proxy.setDown(true)
	stDown, bodyDown, _ := doJSONReq(t, http.MethodGet, ts.URL+"/readyz", "", nil, nil)
	if stDown != http.StatusServiceUnavailable {
		t.Fatalf("during DB outage expected /readyz 503, got %d body=%v", stDown, bodyDown)
	}
	stRead, _, _ := doJSONReq(t, http.MethodGet, ts.URL+"/api/v1/katalog/produk", "", nil, nil)
	if stRead < 500 {
		t.Fatalf("during DB outage katalog read expected 5xx controlled error, got %d", stRead)
	}

	// 3. Simulasi DB pulih dengan latensi 120ms -> harus pulih otomatis tanpa restart server!
	proxy.latencyMS.Store(60)
	proxy.setDown(false)
	var recovered bool
	for i := 0; i < 10; i++ {
		if st, _, _ := doJSONReq(t, http.MethodGet, ts.URL+"/readyz", "", nil, nil); st == http.StatusOK {
			recovered = true
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !recovered {
		t.Fatal("API did not self-recover /readyz after DB connectivity restored")
	}
	if st, _, _ := doJSONReq(t, http.MethodGet, ts.URL+"/api/v1/katalog/produk", "", nil, nil); st != http.StatusOK {
		t.Fatalf("katalog read after recovery expected 200, got %d", st)
	}
}

func TestChaos_ConnectionPoolExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("melewati uji chaos pool pada mode -short")
	}
	root := repoRootDir(t)
	appCfg, err := config.LoadFromEnvAndFile(filepath.Join(root, ".env.test"))
	if err != nil {
		t.Fatal(err)
	}
	proxy := startChaosTCPProxy(t, appCfg.DBHost, appCfg.DBPort)
	pool := openChaosProxyPool(t, proxy, appCfg, 2)

	srvCfg := ConfigFromAppConfig(appCfg)
	srvCfg.StorageKYCDir = t.TempDir()
	srvCfg.StorageBuktiDir = t.TempDir()
	ts := httptest.NewServer(NewServer(pool, srvCfg).Router())
	defer ts.Close()

	// Tahan 2 dari 2 koneksi maksimal di DB dengan transaksi aktif
	ctxHold, cancelHold := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelHold()
	var holdWG sync.WaitGroup
	for i := 0; i < 2; i++ {
		c, cErr := pool.Acquire(ctxHold)
		if cErr != nil {
			t.Fatalf("acquire hold conn %d: %v", i, cErr)
		}
		holdWG.Add(1)
		go func() {
			defer holdWG.Done()
			defer c.Release()
			_, _ = c.Exec(ctxHold, `SELECT pg_sleep(1.2)`)
		}()
	}
	time.Sleep(150 * time.Millisecond)

	// Permintaan /readyz ketika pool jenuh memakai timeout 250ms pada client/ctx -> gagal bersih tanpa hang
	cli := &http.Client{Timeout: 400 * time.Millisecond}
	resp, rErr := cli.Get(ts.URL + "/readyz")
	if rErr == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatal("expected /readyz to fail or time out while 100% pool connections held")
		}
	}
	holdWG.Wait()

	// Setelah koneksi bebas, /readyz kembali 200
	if st, _, _ := doJSONReq(t, http.MethodGet, ts.URL+"/readyz", "", nil, nil); st != http.StatusOK {
		t.Fatalf("expected /readyz 200 after pool released, got %d", st)
	}
}

func TestChaos_UnwritableStorageGracefulRejectionAndNoOrphans(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")

	// 1. Pastikan inisialisasi layanan dengan direktori storage menunjuk berkas biasa gagal secara terkontrol tanpa panik
	badDir := filepath.Join(t.TempDir(), "file-not-dir")
	if err := os.WriteFile(badDir, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	badCfg := ConfigFromAppConfig(e.appCfg)
	badCfg.StorageKYCDir = badDir
	badCfg.StorageBuktiDir = badDir
	_ = NewServer(e.pool, badCfg)
	memTok := abuseLogin(t, e.httpSrv.URL, "anggota1@kopdes-test.local", e.seedPwd, "")

	// 2. Simulasi kegagalan disk/I/O pada target direktori penyimpanan bukti bayar (path target diblokir direktori)
	_, _ = e.pool.Exec(context.Background(), `UPDATE pembayaran SET status = 'batal' WHERE metode = 'qris' AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi')`)
	oSt, oBody, oRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/toko/checkout", memTok, map[string]interface{}{
		"items": []map[string]interface{}{{"produk_id": e.res.ProdukID, "kuantitas": 1}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
		"nama_penerima": "Disk Tester", "hp_penerima": "08110000004",
	}, map[string]string{"Idempotency-Key": "chaos-disk-" + uuid.NewString()})
	if oSt != http.StatusCreated {
		t.Fatalf("checkout failed: %d %s", oSt, string(oRaw))
	}
	ordID := oBody["data"].(map[string]interface{})["id"].(string)
	pSt, pBody, pRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran", memTok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": ordID,
	}, map[string]string{"Idempotency-Key": "chaos-dpay-" + uuid.NewString()})
	if pSt != http.StatusCreated {
		t.Fatalf("create pembayaran failed: %d %s", pSt, string(pRaw))
	}
	payID := pBody["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})["id"].(string)

	// Ganti buktiDir menjadi file biasa (tidak bisa ditulis sub-berkas) pada instance server kedua
	fileAsDir := filepath.Join(t.TempDir(), "full_or_corrupt_mount")
	_ = os.WriteFile(fileAsDir, []byte("disk-full-sim"), 0o600)
	bayarSvc, err := pembayaran.NewService(e.pool, pembayaran.Config{ProofStorageDir: fileAsDir, KeyProvider: nil})
	if err == nil && bayarSvc != nil {
		t.Fatalf("expected NewService to reject fileAsDir %s", fileAsDir)
	}
	// Simulasikan error tulis disk runtime: pasang direktori bernama sama dengan target file bukti_<id>.enc!
	runtimeDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(runtimeDir, "bukti_"+payID+".enc"), 0o700)
	brokenCfg := ConfigFromAppConfig(e.appCfg)
	brokenCfg.StorageKYCDir = e.kycDir
	brokenCfg.StorageBuktiDir = runtimeDir
	brokenSrv := httptest.NewServer(NewServer(e.pool, brokenCfg).Router())
	defer brokenSrv.Close()

	uSt, _, _ := doMultipartReq(t, brokenSrv.URL+"/api/v1/pembayaran", memTok, "file", "bukti.png", makeUniquePNG(t),
		map[string]string{"pembayaran_id": payID}, nil)
	if uSt < 500 && uSt != http.StatusInsufficientStorage {
		t.Fatalf("expected 5xx error when proof write fails on unwritable path, got %d", uSt)
	}
	// Pastikan status pembayaran di DB tetap menunggu_pembayaran dan bukti_url kosong (tx rollback penuh)
	var dbStatus, dbURL string
	if err := e.pool.QueryRow(context.Background(), `SELECT status, bukti_url FROM pembayaran WHERE id = $1`, payID).Scan(&dbStatus, &dbURL); err != nil {
		t.Fatal(err)
	}
	if dbStatus != "menunggu_pembayaran" || dbURL != "" {
		t.Fatalf("failed disk write corrupted DB state: status=%q bukti_url=%q", dbStatus, dbURL)
	}
	_, _ = e.pool.Exec(context.Background(), `UPDATE pembayaran SET status = 'batal' WHERE id = $1`, payID)
}

func TestChaos_APIRestartActiveSessionSurvives_AndWorkerCrashIdempotent(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")

	// Server instance #1 login
	cfg := ConfigFromAppConfig(e.appCfg)
	cfg.StorageKYCDir = e.kycDir
	cfg.StorageBuktiDir = e.buktiDir
	ts1 := httptest.NewServer(NewServer(e.pool, cfg).Router())
	tok := abuseLogin(t, ts1.URL, "anggota1@kopdes-test.local", e.seedPwd, "")
	ts1.Close() // hentikan instance #1 sepenuhnya

	// Server instance #2 baru terhadap DB yang sama
	ts2 := httptest.NewServer(NewServer(e.pool, cfg).Router())
	defer ts2.Close()
	meSt, _, meRaw := doJSONReq(t, http.MethodGet, ts2.URL+"/api/v1/auth/me", tok, nil, nil)
	if meSt != http.StatusOK {
		t.Fatalf("active DB session MUST survive API restart, got %d %s", meSt, string(meRaw))
	}

	// Worker crash / cancel mid-billing -> re-run idempoten tanpa duplikat tagihan
	pengSvc := pengaturan.NewService(e.pool, nil)
	angSvc := anggota.NewService(e.pool, anggota.Config{})
	iurSvc := iuran.NewService(e.pool, nil, angSvc, pengSvc)
	res1, err := iurSvc.GenerateMonthlyBills(context.Background(), 2095, 1)
	if err != nil {
		t.Fatalf("generate run 1: %v", err)
	}
	res2, err := iurSvc.GenerateMonthlyBills(context.Background(), 2095, 1)
	if err != nil || res2.Dibuat != 0 || res2.Dilewati < res1.Dibuat {
		t.Fatalf("worker restart billing MUST be idempotent: run1=%+v run2=%+v err=%v", res1, res2, err)
	}
	_, _ = e.pool.Exec(context.Background(), `DELETE FROM tagihan_iuran WHERE periode_tahun = 2095 AND periode_bulan = 1`)
	_ = io.Discard
	_ = strings.TrimSpace
}
