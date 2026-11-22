package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/testseed"
)

func TestDecisionAudit_D2_ManualRefundFlowAndReports(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")
	petugasTok := loginSession(t, e, "petugas@kopdes-test.local", e.seedPwd, "")
	bendTok := loginSession(t, e, "bendahara@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "bendahara@kopdes-test.local"))
	adminTok := loginSession(t, e, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"))
	memTok := loginSession(t, e, "anggota1@kopdes-test.local", e.seedPwd, "")

	sku := "REF-" + uuid.NewString()[:8]
	cpSt, cpBody, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/katalog/produk", petugasTok, map[string]interface{}{
		"sku": sku, "nama": "Kopi Desa 250g", "satuan": "pack",
		"harga_umum": 30000, "harga_anggota": 25000, "stok": 5, "status": "aktif",
	}, nil)
	if cpSt != http.StatusCreated {
		t.Fatalf("create produk status=%d", cpSt)
	}
	prodID := cpBody["data"].(map[string]interface{})["id"].(string)

	// Checkout 2 unit & lunasi pembayaran QRIS
	oSt, oBody, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/toko/checkout", memTok, map[string]interface{}{
		"items": []map[string]interface{}{{"produk_id": prodID, "kuantitas": 2}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
		"nama_penerima": "Anggota Satu", "hp_penerima": "08110000004",
	}, map[string]string{"Idempotency-Key": "reford-" + uuid.NewString()})
	if oSt != http.StatusCreated {
		t.Fatalf("checkout failed: %d", oSt)
	}
	ordID := oBody["data"].(map[string]interface{})["id"].(string)
	payID, nomTotal := createAndUploadQRISOrderPayment(t, e, memTok, ordID)
	vSt, _, vRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran/"+payID+"/verifikasi", adminTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nomTotal, "alasan": "Dana QRIS diterima",
	}, nil)
	if vSt != http.StatusOK {
		t.Fatalf("approve payment failed: %d %s", vSt, string(vRaw))
	}

	// Catat omzet sebelum refund
	beforeSales := sumLaporanOmzet(t, e, adminTok)
	if snap := lookupProdukSnapshot(t, e, sku); int(snap["stok"].(float64)) != 3 {
		t.Fatalf("expected stok=3 after approval, got %v", snap)
	}

	// 1. Bendahara mengajukan refund
	reqSt, reqBody, reqRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran/"+payID+"/refund", bendTok, map[string]interface{}{
		"aksi": "ajukan", "metode": "transfer", "nominal": nomTotal, "alasan": "Barang cacat produksi perlu pengembalian dana",
	}, nil)
	if reqSt != http.StatusCreated {
		t.Fatalf("refund request failed: %d %s", reqSt, string(reqRaw))
	}
	refID := reqBody["data"].(map[string]interface{})["id"].(string)

	// 2. Bendahara yang sama coba menyetujui sendiri -> ditolak 403 DUAL_CONTROL_VIOLATION
	selfSt, _, selfRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran/"+payID+"/refund", bendTok, map[string]interface{}{
		"aksi": "setujui", "catatan_pelaksana": "Coba setujui sendiri",
	}, nil)
	if selfSt != http.StatusForbidden {
		t.Fatalf("expected 403 dual control violation on self-approve refund, got %d %s", selfSt, string(selfRaw))
	}

	// 3. Admin menyetujui refund beserta unggah bukti transfer refund terenkripsi
	refundPNG := makeUniquePNG(t)
	apSt, apBody, apRaw := doMultipartReq(t, e.httpSrv.URL+"/api/v1/pembayaran/"+payID+"/refund", adminTok, "file", "refund.png", refundPNG, map[string]string{
		"aksi": "setujui", "catatan_pelaksana": "Transfer balik rekening anggota selesai",
	}, nil)
	if apSt != http.StatusOK {
		t.Fatalf("admin approve refund failed: %d %s", apSt, string(apRaw))
	}
	if apBody["data"].(map[string]interface{})["status"] != "selesai" {
		t.Fatalf("expected refund status selesai, got %v", apBody)
	}

	// 4. Cek pembayaran=dikembalikan, pesanan=batal, stok kembali 3->5, bukti terenkripsi di buktiDir
	pDetSt, pDetBody, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/pembayaran?id="+payID, adminTok, nil, nil)
	if pDetSt != http.StatusOK || pDetBody["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})["status"] != "dikembalikan" {
		t.Fatalf("expected pembayaran status dikembalikan, got %v", pDetBody)
	}
	if snap := lookupProdukSnapshot(t, e, sku); int(snap["stok"].(float64)) != 5 || int(snap["stok_tertahan"].(float64)) != 0 {
		t.Fatalf("expected stok restored to 5 and tertahan=0 after refund, got %v", snap)
	}
	encPath := filepath.Join(e.buktiDir, "refund_"+refID+".enc")
	encDisk, rErr := os.ReadFile(encPath)
	if rErr != nil || bytes.HasPrefix(encDisk, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("expected encrypted refund proof at %s without plaintext PNG header, err=%v", encPath, rErr)
	}
	resp, dErr := http.DefaultClient.Do(mustNewReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/pembayaran?refund_bukti_id="+refID, adminTok))
	if dErr != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch refund proof failed: err=%v resp=%v", dErr, resp)
	}
	gotBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Equal(gotBytes, refundPNG) {
		t.Fatal("decrypted refund proof mismatch")
	}

	// 5. Cek dampak laporan: penjualan turun kembali (order batal tidak dihitung) dan pembayaran mencatat total_dikembalikan
	afterSales := sumLaporanOmzet(t, e, adminTok)
	if afterSales != beforeSales-(2*25000+5000) && afterSales >= beforeSales {
		t.Fatalf("expected penjualan omzet to drop after refund: before=%d after=%d", beforeSales, afterSales)
	}
	payRepSt, payRepBody, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/laporan?jenis=pembayaran", adminTok, nil, nil)
	if payRepSt != http.StatusOK {
		t.Fatalf("laporan pembayaran status=%d", payRepSt)
	}
	rawRep, _ := json.Marshal(payRepBody)
	if !bytes.Contains(rawRep, []byte(`"dikembalikan"`)) || !bytes.Contains(rawRep, []byte(`"total_dikembalikan"`)) {
		t.Fatalf("expected dikembalikan and total_dikembalikan in laporan pembayaran: %s", string(rawRep))
	}
}

func sumLaporanOmzet(t *testing.T, e *e2eEnv, tok string) int64 {
	t.Helper()
	st, body, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/laporan?jenis=penjualan", tok, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("laporan penjualan status=%d", st)
	}
	var sum int64
	if rows, ok := body["data"].([]interface{}); ok {
		for _, r := range rows {
			sum += int64(r.(map[string]interface{})["total_omzet"].(float64))
		}
	}
	return sum
}

func TestDecisionAudit_D7_DualControlNormalAndBreakglass(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")
	petugasTok := loginSession(t, e, "petugas@kopdes-test.local", e.seedPwd, "")
	adminSecret := testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local")
	bendSecret := testseed.DeterministicTOTPSecret(e.appCfg, "bendahara@kopdes-test.local")
	adminTok := loginSession(t, e, "admin@kopdes-test.local", e.seedPwd, adminSecret)
	bendTok := loginSession(t, e, "bendahara@kopdes-test.local", e.seedPwd, bendSecret)
	memTok := loginSession(t, e, "anggota1@kopdes-test.local", e.seedPwd, "")

	// Siapkan produk dan 2 pesanan tunai baru agar tidak bertabrakan dengan tagihan pending tes lain
	sku := "BG-" + uuid.NewString()[:8]
	cpSt, cpBody, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/katalog/produk", petugasTok, map[string]interface{}{
		"sku": sku, "nama": "Minyak Desa 1L", "satuan": "botol",
		"harga_umum": 20000, "harga_anggota": 18000, "stok": 10, "status": "aktif",
	}, nil)
	if cpSt != http.StatusCreated {
		t.Fatalf("create produk bg failed: %d", cpSt)
	}
	prodID := cpBody["data"].(map[string]interface{})["id"].(string)

	createCashOrder := func(prefix string) string {
		st, body, raw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/toko/checkout", memTok, map[string]interface{}{
			"items":             []map[string]interface{}{{"produk_id": prodID, "kuantitas": 1}},
			"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "tunai",
			"nama_penerima": "Anggota Satu", "hp_penerima": "08110000004",
		}, map[string]string{"Idempotency-Key": prefix + uuid.NewString()})
		if st != http.StatusCreated {
			t.Fatalf("checkout %s failed: %d %s", prefix, st, string(raw))
		}
		return body["data"].(map[string]interface{})["id"].(string)
	}
	ord1 := createCashOrder("bgord1-")
	ord2 := createCashOrder("bgord2-")

	// 1. Default DUAL_CONTROL_BREAKGLASS_ENABLED=false -> admin self-verify ditolak meski bawa breakglass + TOTP valid
	p1St, p1Body, p1Raw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran", adminTok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "tunai", "pesanan_id": ord1,
	}, map[string]string{"Idempotency-Key": "bg1-" + uuid.NewString()})
	if p1St != http.StatusCreated {
		t.Fatalf("create tunai by admin failed: %d %s", p1St, string(p1Raw))
	}
	pay1ID := p1Body["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})["id"].(string)
	nom1 := int64(p1Body["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})["nominal_total"].(float64))
	nextCode, _ := security.GenerateTOTPCode(adminSecret, time.Now().UTC().Add(30*time.Second))

	dcSt, _, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran/"+pay1ID+"/verifikasi", adminTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nom1, "alasan": "Darurat bendahara sedang berhalangan hari ini",
		"breakglass": true, "mfa_code": nextCode,
	}, nil)
	if dcSt != http.StatusForbidden {
		t.Fatalf("when DUAL_CONTROL_BREAKGLASS_ENABLED=false admin breakglass must be 403, got %d", dcSt)
	}

	// 2. Server dengan DUAL_CONTROL_BREAKGLASS_ENABLED=true
	bgCfg := ConfigFromAppConfig(e.appCfg)
	bgCfg.StorageKYCDir = e.kycDir
	bgCfg.StorageBuktiDir = e.buktiDir
	bgCfg.DualControlBreakglassEnabled = true
	bgSrv := httptest.NewServer(NewServer(e.pool, bgCfg).Router())
	defer bgSrv.Close()

	// Bendahara TIDAK boleh memakai breakglass walau DUAL_CONTROL_BREAKGLASS_ENABLED=true
	bPaySt, bPayBody, bPayRaw := doJSONReq(t, http.MethodPost, bgSrv.URL+"/api/v1/pembayaran", bendTok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "tunai", "pesanan_id": ord2,
	}, map[string]string{"Idempotency-Key": "bg-bend-" + uuid.NewString()})
	if bPaySt != http.StatusCreated {
		t.Fatalf("create tunai by bendahara failed: %d %s", bPaySt, string(bPayRaw))
	}
	bPayID := bPayBody["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})["id"].(string)
	bNom := int64(bPayBody["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})["nominal_total"].(float64))
	bNextCode, _ := security.GenerateTOTPCode(bendSecret, time.Now().UTC().Add(30*time.Second))
	bBGSt, _, _ := doJSONReq(t, http.MethodPost, bgSrv.URL+"/api/v1/pembayaran/"+bPayID+"/verifikasi", bendTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": bNom, "alasan": "Bendahara mencoba breakglass sendirian",
		"breakglass": true, "mfa_code": bNextCode,
	}, nil)
	if bBGSt != http.StatusForbidden {
		t.Fatalf("bendahara must NEVER use breakglass (expected 403), got %d", bBGSt)
	}

	bSelfSt, _, _ := doJSONReq(t, http.MethodPost, bgSrv.URL+"/api/v1/pembayaran/"+pay1ID+"/verifikasi", adminTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nom1, "alasan": "pendek", "breakglass": true, "mfa_code": nextCode,
	}, nil)
	if bSelfSt != http.StatusBadRequest {
		t.Fatalf("breakglass with short reason <10 chars must be 400, got %d", bSelfSt)
	}

	badMFASt, _, _ := doJSONReq(t, http.MethodPost, bgSrv.URL+"/api/v1/pembayaran/"+pay1ID+"/verifikasi", adminTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nom1, "alasan": "Darurat hanya 1 admin bertugas di kantor desa",
		"breakglass": true, "mfa_code": "000000",
	}, nil)
	if badMFASt != http.StatusUnauthorized {
		t.Fatalf("breakglass with invalid TOTP must be 401, got %d", badMFASt)
	}

	// Berhasil dengan TOTP step berikutnya + alasan >= 10 karakter
	freshCode, _ := security.GenerateTOTPCode(adminSecret, time.Now().UTC().Add(30*time.Second))
	okSt, okBody, okRaw := doJSONReq(t, http.MethodPost, bgSrv.URL+"/api/v1/pembayaran/"+pay1ID+"/verifikasi", adminTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nom1,
		"alasan":     "Darurat hanya ada satu admin aktif untuk menutup kas hari ini",
		"breakglass": true, "mfa_code": freshCode,
	}, nil)
	if okSt != http.StatusOK || okBody["data"].(map[string]interface{})["status"] != "lunas" {
		t.Fatalf("expected breakglass approval 200 lunas, got %d %s", okSt, string(okRaw))
	}

	var bgLogCount int
	if err := e.pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM audit_log WHERE aksi = 'pembayaran.verify_breakglass' AND entitas_id = $1", pay1ID).Scan(&bgLogCount); err != nil || bgLogCount != 1 {
		t.Fatalf("expected 1 audit_log pembayaran.verify_breakglass entry, got count=%d err=%v", bgLogCount, err)
	}
}
