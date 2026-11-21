package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/testseed"
)

func setupHighBurstAbuseServer(t *testing.T, relEnv string) (*e2eEnv, *httptest.Server) {
	t.Helper()
	e := setupE2EEnv(t, relEnv)
	_, _ = e.pool.Exec(context.Background(), `UPDATE users SET mfa_last_step = 0`)
	cfg := ConfigFromAppConfig(e.appCfg)
	cfg.StorageKYCDir = e.kycDir
	cfg.StorageBuktiDir = e.buktiDir
	cfg.RateLimitRPS = 2000
	cfg.RateLimitBurst = 2000
	ts := httptest.NewServer(NewServer(e.pool, cfg).Router())
	t.Cleanup(ts.Close)
	return e, ts
}

func abuseLogin(t *testing.T, baseURL, identifier, password, totpSecret string) string {
	t.Helper()
	st, body, raw := doJSONReq(t, http.MethodPost, baseURL+"/api/v1/auth/login", "", map[string]string{
		"identifier": identifier, "password": password,
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("abuseLogin %s status=%d raw=%s", identifier, st, string(raw))
	}
	data := body["data"].(map[string]interface{})
	tok, _ := data["session_token"].(string)
	if data["scope"] == "mfa_pending" && totpSecret != "" {
		for _, off := range []time.Duration{0, 30 * time.Second, 60 * time.Second} {
			code, _ := security.GenerateTOTPCode(totpSecret, time.Now().UTC().Add(off))
			mst, _, _ := doJSONReq(t, http.MethodPost, baseURL+"/api/v1/auth/mfa/verify", tok, map[string]string{"code": code}, nil)
			if mst == http.StatusOK {
				return tok
			}
		}
		t.Fatalf("abuseLogin %s mfa/verify gagal di semua jendela TOTP", identifier)
	}
	return tok
}

func createSyntheticExtremePNG(w, h uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // RGB
	_ = binary.Write(&buf, binary.BigEndian, uint32(13))
	buf.WriteString("IHDR")
	buf.Write(ihdr)
	crc := crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...))
	_ = binary.Write(&buf, binary.BigEndian, crc)
	_ = binary.Write(&buf, binary.BigEndian, uint32(0))
	buf.WriteString("IEND")
	_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE([]byte("IEND")))
	return buf.Bytes()
}

func TestSecurityAbuse_SlotExhaustionAndStockHold(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")
	_, _ = e.pool.Exec(context.Background(), `UPDATE users SET mfa_last_step = 0`)
	cfg := ConfigFromAppConfig(e.appCfg)
	cfg.StorageKYCDir = e.kycDir
	cfg.StorageBuktiDir = e.buktiDir
	cfg.PendingLimitPerMember = 2
	cfg.PendingLimitGlobal = 3
	cfg.RateLimitRPS = 1000
	cfg.RateLimitBurst = 1000
	srv := httptest.NewServer(NewServer(e.pool, cfg).Router())
	defer srv.Close()

	// Bersihkan pending QRIS sisa test sebelumnya pada DB uji
	_, _ = e.pool.Exec(context.Background(), `UPDATE pembayaran SET status = 'batal' WHERE metode = 'qris' AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi')`)

	petugasTok := abuseLogin(t, srv.URL, "petugas@kopdes-test.local", e.seedPwd, "")
	mem1Tok := abuseLogin(t, srv.URL, "anggota1@kopdes-test.local", e.seedPwd, "")
	mem2Tok := abuseLogin(t, srv.URL, "anggota2@kopdes-test.local", e.seedPwd, "")

	// Buat produk stok=1 dan promo kuota=1 sekali_per_anggota
	cpSt, cpBody, cpRaw := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/katalog/produk", petugasTok, map[string]interface{}{
		"sku": "ABUSE-HOLD-" + uuid.NewString()[:8], "nama": "Paket Abuse Stok", "satuan": "pcs",
		"harga_umum": 50000, "harga_anggota": 40000, "stok": 1, "status": "aktif",
	}, nil)
	if cpSt != http.StatusCreated {
		t.Fatalf("create produk: %d %s", cpSt, string(cpRaw))
	}
	prodID := cpBody["data"].(map[string]interface{})["id"].(string)

	promoKode := "PRABUSE" + uuid.NewString()[:6]
	prSt, _, prRaw := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/katalog/produk", petugasTok, map[string]interface{}{
		"action": "create_promo", "kode": promoKode, "nama": "Promo Kuota 1", "tipe_diskon": "nominal",
		"nilai_diskon": 5000, "min_belanja": 10000, "kuota_total": 1, "sekali_per_anggota": true,
	}, nil)
	if prSt != http.StatusCreated {
		t.Fatalf("create promo: %d %s", prSt, string(prRaw))
	}

	// Race 8 checkout untuk 1 stok -> tepat 1 sukses (201), 7 ditolak 409 OUT_OF_STOCK
	var wg sync.WaitGroup
	results := make([]int, 8)
	var promoOrdID string
	var promoMu sync.Mutex
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			st, b, _ := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/toko/checkout", mem1Tok, map[string]interface{}{
				"items":             []map[string]interface{}{{"produk_id": prodID, "kuantitas": 1}},
				"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
				"kode_promo": promoKode, "nama_penerima": "Abuser", "hp_penerima": "08110000004",
			}, map[string]string{"Idempotency-Key": "ab-hold-" + uuid.NewString()})
			results[idx] = st
			if st == http.StatusCreated {
				promoMu.Lock()
				promoOrdID = b["data"].(map[string]interface{})["id"].(string)
				promoMu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()
	won, conflicts := 0, 0
	for _, r := range results {
		if r == http.StatusCreated {
			won++
		} else if r == http.StatusConflict {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent hold status=%d", r)
		}
	}
	if won != 1 || conflicts != 7 {
		t.Fatalf("expected 1 won and 7 conflicts, got won=%d conflicts=%d", won, conflicts)
	}

	// Uji pengurasan slot kode unik per member (maks 2) dan batas global (maks 3)
	// Buat produk tambahan berstok cukup untuk order QRIS uji slot
	_, p2Body, _ := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/katalog/produk", petugasTok, map[string]interface{}{
		"sku": "ABUSE-SLOT-" + uuid.NewString()[:8], "nama": "Paket Uji Slot", "satuan": "pcs",
		"harga_umum": 30000, "harga_anggota": 25000, "stok": 10, "status": "aktif",
	}, nil)
	slotProdID := p2Body["data"].(map[string]interface{})["id"].(string)
	mkOrder := func(tok string) string {
		st, b, raw := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/toko/checkout", tok, map[string]interface{}{
			"items": []map[string]interface{}{{"produk_id": slotProdID, "kuantitas": 1}},
			"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
			"nama_penerima": "Slot Tester", "hp_penerima": "08110000004",
		}, map[string]string{"Idempotency-Key": "ab-slot-" + uuid.NewString()})
		if st != http.StatusCreated {
			t.Fatalf("mkOrder failed: %d %s", st, string(raw))
		}
		return b["data"].(map[string]interface{})["id"].(string)
	}
	o1 := promoOrdID
	o2 := mkOrder(mem1Tok)
	o3 := mkOrder(mem1Tok)
	o4 := mkOrder(mem2Tok)
	o5 := mkOrder(mem2Tok)

	for idx, oid := range []string{o1, o2, o3} {
		st, _, raw := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/pembayaran", mem1Tok, map[string]interface{}{
			"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": oid,
		}, map[string]string{"Idempotency-Key": "ab-pay1-" + uuid.NewString()})
		if idx < 2 && st != http.StatusCreated {
			t.Fatalf("member1 payment #%d failed: %d %s", idx+1, st, string(raw))
		}
		if idx == 2 && st != http.StatusTooManyRequests {
			t.Fatalf("member1 payment #3 must be rejected 429 TOO_MANY_PENDING_QRIS, got %d %s", st, string(raw))
		}
	}
	// Member2 mengambil slot ke-3 global (sukses), lalu ke-4 melampaui PendingLimitGlobal=3 -> 503 PAYMENT_QUEUE_FULL
	stM2First, _, rawM2First := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/pembayaran", mem2Tok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": o4,
	}, map[string]string{"Idempotency-Key": "ab-pay2a-" + uuid.NewString()})
	if stM2First != http.StatusCreated {
		t.Fatalf("member2 global slot #3 failed: %d %s", stM2First, string(rawM2First))
	}
	stM2Over, _, rawM2Over := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/pembayaran", mem2Tok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": o5,
	}, map[string]string{"Idempotency-Key": "ab-pay2b-" + uuid.NewString()})
	if stM2Over != http.StatusServiceUnavailable {
		t.Fatalf("global slot #4 must return 503 PAYMENT_QUEUE_FULL, got %d %s", stM2Over, string(rawM2Over))
	}
	// Lepas pending uji agar tidak memengaruhi tes lain
	_, _ = e.pool.Exec(context.Background(), `UPDATE pembayaran SET status = 'batal' WHERE metode = 'qris' AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi')`)
}

func TestSecurityAbuse_RegUploadSpamAndCredentialStuffingVsCGNAT(t *testing.T) {
	e, srvURL := func() (*e2eEnv, string) {
		e, s := setupHighBurstAbuseServer(t, ".env.test")
		return e, s.URL
	}()

	// 1. Pendaftaran spam dari 500 IP palsu berbeda -> tidak boleh memenuhi tabel rate_limits DB (Hybrid L1)
	var beforeRL int
	_ = e.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM rate_limits WHERE kategori = 'registration'`).Scan(&beforeRL)
	for i := 0; i < 500; i++ {
		fakeIP := fmt.Sprintf("198.51.%d.%d", i/250, (i%250)+1)
		// Body invalid sengaja agar 400 cepat, namun melewati middleware hybridLimit("registration")
		_, _, _ = doJSONReq(t, http.MethodPost, srvURL+"/api/v1/public/pendaftaran", "", map[string]string{
			"nama_lengkap": "",
		}, map[string]string{"X-Forwarded-For": fakeIP})
	}
	var afterRL int
	_ = e.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM rate_limits WHERE kategori = 'registration'`).Scan(&afterRL)
	if afterRL > beforeRL+10 {
		t.Fatalf("500 single-hit spoofed IPs persisted %d rows to DB rate_limits!", afterRL-beforeRL)
	}

	// Spam dari 1 IP -> kena 429 pada hit ke-21
	spamIP := "203.0.113.77"
	hit429 := false
	for i := 0; i < 25; i++ {
		st, _, _ := doJSONReq(t, http.MethodPost, srvURL+"/api/v1/public/pendaftaran", "", map[string]string{
			"nama_lengkap": "",
		}, map[string]string{"X-Forwarded-For": spamIP})
		if st == http.StatusTooManyRequests {
			hit429 = true
			break
		}
	}
	if !hit429 {
		t.Fatal("expected single-IP registration spam to hit 429")
	}

	// 2. Credential stuffing di IP seluler CGNAT bersama -> akun target terkunci, warga sah lain tetap bisa login!
	cgnatIP := "114.79.22.10"
	for i := 0; i < 6; i++ {
		_, _, _ = doJSONReq(t, http.MethodPost, srvURL+"/api/v1/auth/login", "", map[string]string{
			"identifier": "anggota1@kopdes-test.local", "password": "SalahPassword123!",
		}, map[string]string{"X-Forwarded-For": cgnatIP})
	}
	lockSt, _, _ := doJSONReq(t, http.MethodPost, srvURL+"/api/v1/auth/login", "", map[string]string{
		"identifier": "anggota1@kopdes-test.local", "password": e.seedPwd,
	}, map[string]string{"X-Forwarded-For": cgnatIP})
	if lockSt != http.StatusTooManyRequests {
		t.Fatalf("victim anggota1 under brute force expected 429 ACCOUNT_LOCKED, got %d", lockSt)
	}
	neighborSt, _, neighborRaw := doJSONReq(t, http.MethodPost, srvURL+"/api/v1/auth/login", "", map[string]string{
		"identifier": "anggota2@kopdes-test.local", "password": e.seedPwd,
	}, map[string]string{"X-Forwarded-For": cgnatIP})
	if neighborSt != http.StatusOK {
		t.Fatalf("legitimate neighbor sharing CGNAT IP must NOT be locked out, got %d %s", neighborSt, string(neighborRaw))
	}
	// Reset lockout anggota1 di DB setelah uji
	_, _ = e.pool.Exec(context.Background(), `UPDATE users SET failed_login_count = 0, locked_until = NULL`)
}

func TestSecurityAbuse_MassAssignment_RejectedOnAllMutatingEndpoints(t *testing.T) {
	e, srv := setupHighBurstAbuseServer(t, ".env.test")
	adminTok := abuseLogin(t, srv.URL, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"))
	memTok := abuseLogin(t, srv.URL, "anggota1@kopdes-test.local", e.seedPwd, "")
	zeroID := "00000000-0000-0000-0000-000000000000"

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		body   map[string]interface{}
	}{
		{"public_pendaftaran", http.MethodPost, "/api/v1/public/pendaftaran", "", map[string]interface{}{
			"nama_lengkap": "Calon", "nik": "3201010101019991", "no_hp": "081299990001", "alamat": "Desa", "status": "disetujui", "peran": "admin",
		}},
		{"auth_login", http.MethodPost, "/api/v1/auth/login", "", map[string]interface{}{
			"identifier": "anggota1@kopdes-test.local", "password": e.seedPwd, "mfa_verified": true,
		}},
		{"auth_password", http.MethodPost, "/api/v1/auth/password", memTok, map[string]interface{}{
			"current_password": e.seedPwd, "new_password": "PasswordBaru123!", "peran": "admin",
		}},
		{"auth_mfa_verify", http.MethodPost, "/api/v1/auth/mfa/verify", adminTok, map[string]interface{}{
			"code": "123456", "scope": "full",
		}},
		{"pengaturan_put", http.MethodPut, "/api/v1/pengaturan", adminTok, map[string]interface{}{
			"nama_koperasi": "Kopdes", "role": "superuser",
		}},
		{"users_post", http.MethodPost, "/api/v1/users", adminTok, map[string]interface{}{
			"email": "mass@kopdes-test.local", "nama_lengkap": "User Mass", "peran": "anggota", "superuser": true,
		}},
		{"users_patch", http.MethodPatch, "/api/v1/users/" + zeroID, adminTok, map[string]interface{}{
			"status_aktif": true, "password_hash": "hacked",
		}},
		{"anggota_status", http.MethodPatch, "/api/v1/anggota/" + zeroID + "/status", adminTok, map[string]interface{}{
			"status": "aktif", "user_id": zeroID,
		}},
		{"iuran_jenis", http.MethodPost, "/api/v1/iuran/jenis", adminTok, map[string]interface{}{
			"kode": "MASS01", "nama": "Mass", "nominal": 1000, "extra_field": true,
		}},
		{"iuran_keringanan", http.MethodPatch, "/api/v1/iuran/tagihan/" + zeroID + "/keringanan", adminTok, map[string]interface{}{
			"nominal_keringanan": 1000, "alasan": "tes", "status": "lunas",
		}},
		{"katalog_write", http.MethodPost, "/api/v1/katalog/produk", adminTok, map[string]interface{}{
			"sku": "MASS-01", "nama": "Mass", "satuan": "pcs", "stok": 1, "stok_tertahan": 999,
		}},
		{"toko_checkout", http.MethodPost, "/api/v1/toko/checkout", memTok, map[string]interface{}{
			"items": []map[string]interface{}{{"produk_id": zeroID, "kuantitas": 1}},
			"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
			"nama_penerima": "A", "hp_penerima": "08110000004", "total_bayar": 1,
		}},
		{"pesanan_status", http.MethodPatch, "/api/v1/pesanan/" + zeroID + "/status", adminTok, map[string]interface{}{
			"status": "batal", "total_bayar": 0,
		}},
		{"pembayaran_post", http.MethodPost, "/api/v1/pembayaran", memTok, map[string]interface{}{
			"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": zeroID, "nominal_total": 1, "status": "lunas",
		}},
		{"pembayaran_verifikasi", http.MethodPost, "/api/v1/pembayaran/" + zeroID + "/verifikasi", adminTok, map[string]interface{}{
			"keputusan": "setuju", "nominal_dibayar": 1, "alasan": "ok", "status": "lunas",
		}},
		{"pembayaran_refund", http.MethodPost, "/api/v1/pembayaran/" + zeroID + "/refund", adminTok, map[string]interface{}{
			"aksi": "ajukan", "nominal": 100, "metode": "tunai", "alasan": "ok", "dilaksanakan_oleh": zeroID,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, body, raw := doJSONReq(t, tc.method, srv.URL+tc.path, tc.token, tc.body, nil)
			if st != http.StatusBadRequest {
				t.Fatalf("%s mass assignment with unknown field MUST return 400 INVALID_JSON, got %d body=%v raw=%s", tc.name, st, body, string(raw))
			}
		})
	}
}

func TestSecurityAbuse_BOLA_TokensCSRF_AndCrossSetIsolation(t *testing.T) {
	e, srv := setupHighBurstAbuseServer(t, ".env.test")
	adminTok := abuseLogin(t, srv.URL, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"))
	mem1Tok := abuseLogin(t, srv.URL, "anggota1@kopdes-test.local", e.seedPwd, "")
	mem2Tok := abuseLogin(t, srv.URL, "anggota2@kopdes-test.local", e.seedPwd, "")

	// Buat order + pembayaran QRIS + upload bukti milik mem1
	oSt, oBody, oRaw := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/toko/checkout", mem1Tok, map[string]interface{}{
		"items":             []map[string]interface{}{{"produk_id": e.res.ProdukID, "kuantitas": 1}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
		"nama_penerima": "Pemilik Sah", "hp_penerima": "08110000004",
	}, map[string]string{"Idempotency-Key": "bola-ord-" + uuid.NewString()})
	if oSt != http.StatusCreated {
		t.Fatalf("checkout mem1 failed: %d %s", oSt, string(oRaw))
	}
	ordID := oBody["data"].(map[string]interface{})["id"].(string)
	payID, _ := createAndUploadQRISOrderPayment(t, e, mem1Tok, ordID)

	// BOLA checks: mem2 tidak boleh membaca/ubah/unggah/unduh milik mem1
	if st, _, _ := doJSONReq(t, http.MethodGet, srv.URL+"/api/v1/pesanan?id="+ordID, mem2Tok, nil, nil); st != http.StatusForbidden {
		t.Fatalf("BOLA read pesanan mem1 by mem2 expected 403, got %d", st)
	}
	if st, _, _ := doJSONReq(t, http.MethodPatch, srv.URL+"/api/v1/pesanan/"+ordID+"/status", mem2Tok, map[string]string{"status": "batal"}, nil); st != http.StatusForbidden {
		t.Fatalf("BOLA cancel pesanan mem1 by mem2 expected 403, got %d", st)
	}
	if st, _, _ := doJSONReq(t, http.MethodGet, srv.URL+"/api/v1/pembayaran?id="+payID, mem2Tok, nil, nil); st != http.StatusForbidden {
		t.Fatalf("BOLA read pembayaran mem1 by mem2 expected 403, got %d", st)
	}
	if st, _, _ := doJSONReq(t, http.MethodGet, srv.URL+"/api/v1/pembayaran?bukti_id="+payID, mem2Tok, nil, nil); st != http.StatusForbidden {
		t.Fatalf("BOLA download bukti mem1 by mem2 expected 403, got %d", st)
	}
	if st, _, _ := doJSONReq(t, http.MethodGet, srv.URL+"/api/v1/anggota/"+e.res.AnggotaID+"?unmask=true", mem2Tok, nil, nil); st != http.StatusForbidden {
		t.Fatalf("BOLA/BFLA unmask anggota by mem2 expected 403, got %d", st)
	}

	// Token kedaluwarsa di DB -> 401; Cross-set token Set A dikirim ke Set B -> 401
	eSetB := setupE2EEnv(t, ".env.test.alt")
	if st, _, _ := doJSONReq(t, http.MethodGet, eSetB.httpSrv.URL+"/api/v1/auth/me", mem1Tok, nil, nil); st != http.StatusUnauthorized {
		t.Fatalf("Set A token on Set B server expected 401, got %d", st)
	}
	// CSRF & cookie checks on Set B (cookie-only)
	jar, _ := cookiejar.New(nil)
	clientB := &http.Client{Jar: jar}
	lBody, _ := json.Marshal(map[string]string{"identifier": "anggota1@kopdes-test.local", "password": eSetB.seedPwd})
	lResp, err := clientB.Post(eSetB.httpSrv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(lBody))
	if err != nil || lResp.StatusCode != http.StatusOK {
		t.Fatalf("Set B cookie login failed: err=%v resp=%v", err, lResp)
	}
	_ = lResp.Body.Close()

	badCSRF, _ := http.NewRequest(http.MethodPost, eSetB.httpSrv.URL+"/api/v1/auth/logout", strings.NewReader(`{}`))
	// sengaja tanpa X-Requested-With -> wajib 403
	badResp, err := clientB.Do(badCSRF)
	if err != nil {
		t.Fatal(err)
	}
	_ = badResp.Body.Close()
	if badResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 when X-Requested-With missing on cookie session, got %d", badResp.StatusCode)
	}
	_ = adminTok
	_, _ = e.pool.Exec(context.Background(), `UPDATE pembayaran SET status = 'batal' WHERE id = $1`, payID)
}

func TestSecurityAbuse_MaliciousUploads_CSVInjection_AndStoredXSS(t *testing.T) {
	e, srv := setupHighBurstAbuseServer(t, ".env.test")
	adminTok := abuseLogin(t, srv.URL, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"))

	// 1. Pendaftaran dengan payload CSV formula injection + XSS string -> JSON tidak mengeksekusi HTML, CSV ter-neutralize!
	nik := "3208" + randomDigits12()
	hp := "0819" + randomDigits12()[:8]
	rSt, rBody, rRaw := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/public/pendaftaran", "", map[string]string{
		"nama_lengkap": `=CMD|'/C calc'!A0<script>alert(1)</script>`,
		"email":        "xss-" + uuid.NewString()[:8] + "@kopdes-test.local",
		"nik":          nik, "no_hp": hp, "alamat": "Desa Aman RT 01", "pekerjaan": "+SUM(1,2)",
	}, nil)
	if rSt != http.StatusCreated {
		t.Fatalf("register xss/csv probe failed: %d %s", rSt, string(rRaw))
	}
	regData := rBody["data"].(map[string]interface{})
	regID := regData["id"].(string)
	upTok := regData["upload_token"].(string)

	// 2. Unggahan jahat:
	// 2a. Fake PNG script payload
	poly := []byte("<?php echo 'hack'; ?><script>alert(1)</script>")
	if st, _, _ := doMultipartReq(t, srv.URL+"/api/v1/public/pendaftaran/"+regID+"/dokumen", "", "file", "../../evil.php", poly,
		map[string]string{"tipe_dokumen": "ktp"}, map[string]string{"X-Upload-Token": upTok}); st != http.StatusBadRequest {
		t.Fatalf("fake PNG polyglot expected 400, got %d", st)
	}
	// 2b. Extreme PNG dimensions 9001x9001
	extremePNG := createSyntheticExtremePNG(9001, 9001)
	if st, _, _ := doMultipartReq(t, srv.URL+"/api/v1/public/pendaftaran/"+regID+"/dokumen", "", "file", "huge.png", extremePNG,
		map[string]string{"tipe_dokumen": "ktp"}, map[string]string{"X-Upload-Token": upTok}); st != http.StatusBadRequest {
		t.Fatalf("9001x9001 pixel flood PNG expected 400 INVALID_IMAGE_DIMENSIONS, got %d", st)
	}
	// 2c. Oversized > MaxKYCUploadBytes (6 MB)
	oversize := make([]byte, 6<<20)
	copy(oversize, makeUniquePNG(t))
	if st, _, _ := doMultipartReq(t, srv.URL+"/api/v1/public/pendaftaran/"+regID+"/dokumen", "", "file", "big.png", oversize,
		map[string]string{"tipe_dokumen": "ktp"}, map[string]string{"X-Upload-Token": upTok}); st != http.StatusBadRequest && st != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload expected 400/413, got %d", st)
	}
	// 2d. Valid PNG dengan path traversal filename "../../evil.png" -> harus tersimpan dengan nama UUID .enc acak, BUKAN di parent dir
	uSt, _, uRaw := doMultipartReq(t, srv.URL+"/api/v1/public/pendaftaran/"+regID+"/dokumen", "", "file", "../../escape_ktp.png", makeUniquePNG(t),
		map[string]string{"tipe_dokumen": "ktp"}, map[string]string{"X-Upload-Token": upTok})
	if uSt != http.StatusCreated {
		t.Fatalf("valid PNG with traversal filename upload failed: %d %s", uSt, string(uRaw))
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(e.kycDir), "escape_ktp.png")); !os.IsNotExist(statErr) {
		t.Fatal("path traversal filename escaped into parent directory!")
	}

	// Setujui pendaftar ber-nama '=CMD...' lalu cek ekspor CSV anggota & header JSON anti-XSS
	vSt, _, vRaw := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/pendaftaran/"+regID+"/verifikasi", adminTok, map[string]string{
		"keputusan": "disetujui", "catatan": "Uji netralisasi CSV",
	}, nil)
	if vSt != http.StatusOK {
		t.Fatalf("approve xss/csv member failed: %d %s", vSt, string(vRaw))
	}
	csvReq := mustNewReq(t, http.MethodGet, srv.URL+"/api/v1/anggota?format=csv", adminTok)
	csvResp, err := http.DefaultClient.Do(csvReq)
	if err != nil {
		t.Fatal(err)
	}
	csvRaw, _ := io.ReadAll(csvResp.Body)
	_ = csvResp.Body.Close()
	if csvResp.StatusCode != http.StatusOK || csvResp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("CSV export invalid response: status=%d hdr=%v", csvResp.StatusCode, csvResp.Header)
	}
	if bytes.Contains(csvRaw, []byte(",=CMD|")) || !bytes.Contains(csvRaw, []byte("'=CMD|")) {
		t.Fatalf("CSV formula injection was NOT neutralized with leading quote: %s", string(csvRaw))
	}

	// Verifikasi Stored XSS pada respons JSON memakai Content-Type JSON + X-Content-Type-Options: nosniff + ter-escape UTF8
	listReq := mustNewReq(t, http.MethodGet, srv.URL+"/api/v1/anggota", adminTok)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatal(err)
	}
	listRaw, _ := io.ReadAll(listResp.Body)
	_ = listResp.Body.Close()
	if ct := listResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected application/json Content-Type, got %q", ct)
	}
	if bytes.Contains(listRaw, []byte("<script>alert(1)</script>")) {
		t.Fatalf("expected JSON encoding to escape HTML angle brackets, got raw <script> in %s", string(listRaw))
	}
}

func TestSecurityAbuse_ProofReplayAndCrossUserIdempotency(t *testing.T) {
	e, srv := setupHighBurstAbuseServer(t, ".env.test")
	_, _ = e.pool.Exec(context.Background(), `UPDATE pembayaran SET status = 'batal' WHERE metode = 'qris' AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi')`)
	mem1Tok := abuseLogin(t, srv.URL, "anggota1@kopdes-test.local", e.seedPwd, "")
	mem2Tok := abuseLogin(t, srv.URL, "anggota2@kopdes-test.local", e.seedPwd, "")

	// 1. Cross-user Idempotency-Key pada POST /api/v1/toko/checkout
	sharedOrderKey := "cross-ord-" + uuid.NewString()
	stA, bodyA, rawA := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/toko/checkout", mem1Tok, map[string]interface{}{
		"items":             []map[string]interface{}{{"produk_id": e.res.ProdukID, "kuantitas": 1}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
		"nama_penerima": "User A", "hp_penerima": "08110000004",
	}, map[string]string{"Idempotency-Key": sharedOrderKey})
	if stA != http.StatusCreated {
		t.Fatalf("User A checkout failed: %d %s", stA, string(rawA))
	}
	ordA := bodyA["data"].(map[string]interface{})["id"].(string)

	// Replay oleh User A -> 200 OK dengan ID pesanan yang sama
	stA2, bodyA2, _ := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/toko/checkout", mem1Tok, map[string]interface{}{
		"items":             []map[string]interface{}{{"produk_id": e.res.ProdukID, "kuantitas": 1}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
		"nama_penerima": "User A", "hp_penerima": "08110000004",
	}, map[string]string{"Idempotency-Key": sharedOrderKey})
	if stA2 != http.StatusOK || bodyA2["data"].(map[string]interface{})["id"].(string) != ordA {
		t.Fatalf("User A idempotent replay failed: status=%d body=%v", stA2, bodyA2)
	}

	// User B mengirim Idempotency-Key YANG SAMA -> DILARANG 500 atau bocor pesanan User A; harus terisolasi membuat pesanan milik User B (201)
	stB, bodyB, rawB := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/toko/checkout", mem2Tok, map[string]interface{}{
		"items":             []map[string]interface{}{{"produk_id": e.res.ProdukID, "kuantitas": 1}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
		"nama_penerima": "User B", "hp_penerima": "08110000005",
	}, map[string]string{"Idempotency-Key": sharedOrderKey})
	if stB != http.StatusCreated {
		t.Fatalf("cross-user Idempotency-Key checkout MUST return 201 isolated order (NOT 500), got %d %s", stB, string(rawB))
	}
	ordB := bodyB["data"].(map[string]interface{})["id"].(string)
	if ordB == ordA {
		t.Fatal("cross-user Idempotency-Key leaked User A order ID to User B!")
	}

	// 2. Cross-user Idempotency-Key pada POST /api/v1/pembayaran + Proof Replay
	sharedPayKey := "cross-pay-" + uuid.NewString()
	pStA, pBodyA, pRawA := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/pembayaran", mem1Tok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": ordA,
	}, map[string]string{"Idempotency-Key": sharedPayKey})
	if pStA != http.StatusCreated {
		t.Fatalf("User A pembayaran failed: %d %s", pStA, string(pRawA))
	}
	payA := pBodyA["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})["id"].(string)

	pStB, pBodyB, pRawB := doJSONReq(t, http.MethodPost, srv.URL+"/api/v1/pembayaran", mem2Tok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": ordB,
	}, map[string]string{"Idempotency-Key": sharedPayKey})
	if pStB != http.StatusCreated {
		t.Fatalf("cross-user Idempotency-Key pembayaran MUST return 201 isolated payment (NOT 500), got %d %s", pStB, string(pRawB))
	}
	payB := pBodyB["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})["id"].(string)
	if payB == payA {
		t.Fatal("cross-user Idempotency-Key leaked User A payment to User B!")
	}

	// Proof image replay -> gambar identik ditolak 409 DUPLICATE_BUKTI
	samePNG := makeUniquePNG(t)
	upASt, _, upARaw := doMultipartReq(t, srv.URL+"/api/v1/pembayaran", mem1Tok, "file", "buktiA.png", samePNG, map[string]string{"pembayaran_id": payA}, nil)
	if upASt != http.StatusOK {
		t.Fatalf("upload proof A failed: %d %s", upASt, string(upARaw))
	}
	upBSt, _, upBRaw := doMultipartReq(t, srv.URL+"/api/v1/pembayaran", mem2Tok, "file", "buktiB.png", samePNG, map[string]string{"pembayaran_id": payB}, nil)
	if upBSt != http.StatusConflict {
		t.Fatalf("proof image replay MUST be rejected 409 DUPLICATE_BUKTI, got %d %s", upBSt, string(upBRaw))
	}
	_, _ = e.pool.Exec(context.Background(), `UPDATE pembayaran SET status = 'batal' WHERE id IN ($1, $2)`, payA, payB)
	_ = config.Catalog()
}
