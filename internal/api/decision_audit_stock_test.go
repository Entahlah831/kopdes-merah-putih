package api

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/pesanan"
	"github.com/kopdes/merah-putih/internal/testseed"
)

func lookupProdukSnapshot(t *testing.T, e *e2eEnv, sku string) map[string]interface{} {
	t.Helper()
	st, body, raw := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/katalog/produk?q="+sku, "", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("lookup produk %s failed: %d %s", sku, st, string(raw))
	}
	items := body["data"].([]interface{})
	if len(items) == 0 {
		t.Fatalf("produk %s tidak ditemukan", sku)
	}
	return items[0].(map[string]interface{})
}

func TestDecisionAudit_D1_StockReservationConcurrency50Buyers(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")
	petugasTok := loginSession(t, e, "petugas@kopdes-test.local", e.seedPwd, "")
	adminTok := loginSession(t, e, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"))
	memTok := loginSession(t, e, "anggota1@kopdes-test.local", e.seedPwd, "")

	sku := "CONC50-" + uuid.NewString()[:8]
	cpSt, cpBody, cpRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/katalog/produk", petugasTok, map[string]interface{}{
		"sku": sku, "nama": "Minyak Terbatas 1L", "satuan": "botol",
		"harga_umum": 18000, "harga_anggota": 15000, "stok": 5, "status": "aktif",
	}, nil)
	if cpSt != http.StatusCreated {
		t.Fatalf("create produk failed: %d %s", cpSt, string(cpRaw))
	}
	prodID := cpBody["data"].(map[string]interface{})["id"].(string)

	const buyers = 50
	type buyRes struct {
		status     int
		orderID    string
		stokStatus string
	}
	results := make([]buyRes, buyers)
	startCh := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < buyers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startCh
			st, body, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/toko/checkout", memTok, map[string]interface{}{
				"items": []map[string]interface{}{{"produk_id": prodID, "kuantitas": 1}},
				"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
				"nama_penerima": "Pembeli", "hp_penerima": "08110000004",
			}, map[string]string{"Idempotency-Key": "conc50-" + uuid.NewString()})
			var oid, sStat string
			if d, ok := body["data"].(map[string]interface{}); ok {
				oid, _ = d["id"].(string)
				sStat, _ = d["stok_status"].(string)
			}
			results[idx] = buyRes{status: st, orderID: oid, stokStatus: sStat}
		}(i)
	}
	close(startCh)
	wg.Wait()

	var won []string
	conflicts := 0
	for _, r := range results {
		switch r.status {
		case http.StatusCreated:
			if r.stokStatus != "reserved" {
				t.Fatalf("expected winning order stok_status=reserved, got %q", r.stokStatus)
			}
			won = append(won, r.orderID)
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected checkout status: %d", r.status)
		}
	}
	if len(won) != 5 || conflicts != 45 {
		t.Fatalf("expected exact 5 reserved and 45 conflicts, got won=%d conflicts=%d", len(won), conflicts)
	}

	snap := lookupProdukSnapshot(t, e, sku)
	if int(snap["stok"].(float64)) != 5 || int(snap["stok_tertahan"].(float64)) != 5 || int(snap["stok_tersedia"].(float64)) != 0 {
		t.Fatalf("expected stok=5 stok_tertahan=5 stok_tersedia=0 after 5 checkouts, got %v", snap)
	}

	// Setujui pembayaran order #0 -> stok fisik berkurang 5->4, stok_tertahan 5->4, stok_tersedia tetap 0
	payOrderAndApprove(t, e, memTok, adminTok, won[0])
	snap = lookupProdukSnapshot(t, e, sku)
	if int(snap["stok"].(float64)) != 4 || int(snap["stok_tertahan"].(float64)) != 4 || int(snap["stok_tersedia"].(float64)) != 0 {
		t.Fatalf("expected stok=4 stok_tertahan=4 stok_tersedia=0 after 1 approval, got %v", snap)
	}

	// Batalkan order #1 -> tahanan dilepas (stok_tertahan 4->3, stok_tersedia 0->1)
	bSt, _, bRaw := doJSONReq(t, http.MethodPatch, e.httpSrv.URL+"/api/v1/pesanan/"+won[1]+"/status", memTok, map[string]string{
		"status": "batal", "catatan": "Uji lepas reservasi",
	}, nil)
	if bSt != http.StatusOK {
		t.Fatalf("cancel order failed: %d %s", bSt, string(bRaw))
	}
	snap = lookupProdukSnapshot(t, e, sku)
	if int(snap["stok"].(float64)) != 4 || int(snap["stok_tertahan"].(float64)) != 3 || int(snap["stok_tersedia"].(float64)) != 1 {
		t.Fatalf("expected stok=4 stok_tertahan=3 stok_tersedia=1 after cancel, got %v", snap)
	}
}

func TestDecisionAudit_D1_ApprovalAfterExpiredHold_GracefulTindakLanjut(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")
	petugasTok := loginSession(t, e, "petugas@kopdes-test.local", e.seedPwd, "")
	adminTok := loginSession(t, e, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"))
	mem1Tok := loginSession(t, e, "anggota1@kopdes-test.local", e.seedPwd, "")
	mem2Tok := loginSession(t, e, "anggota2@kopdes-test.local", e.seedPwd, "")

	sku := "EXPHOLD-" + uuid.NewString()[:8]
	cpSt, cpBody, cpRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/katalog/produk", petugasTok, map[string]interface{}{
		"sku": sku, "nama": "Gula Terbatas 1Kg", "satuan": "pack",
		"harga_umum": 20000, "harga_anggota": 18000, "stok": 2, "status": "aktif",
	}, nil)
	if cpSt != http.StatusCreated {
		t.Fatalf("create produk failed: %d %s", cpSt, string(cpRaw))
	}
	prodID := cpBody["data"].(map[string]interface{})["id"].(string)

	// Pembeli 1 checkout 2 unit & sudah bayar/upload bukti (menunggu_verifikasi)
	o1St, o1Body, o1Raw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/toko/checkout", mem1Tok, map[string]interface{}{
		"items": []map[string]interface{}{{"produk_id": prodID, "kuantitas": 2}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
		"nama_penerima": "Anggota Satu", "hp_penerima": "08110000004",
	}, map[string]string{"Idempotency-Key": "exp1-" + uuid.NewString()})
	if o1St != http.StatusCreated {
		t.Fatalf("checkout 1 failed: %d %s", o1St, string(o1Raw))
	}
	ord1ID := o1Body["data"].(map[string]interface{})["id"].(string)
	pay1ID, nom1 := createAndUploadQRISOrderPayment(t, e, mem1Tok, ord1ID)

	// Masa tahan order 1 kedaluwarsa lalu worker melepaskan hold
	if _, err := e.pool.Exec(context.Background(), "UPDATE pesanan SET stok_hold_expires_at = NOW() - INTERVAL '5 minutes' WHERE id = $1", ord1ID); err != nil {
		t.Fatalf("expire hold query: %v", err)
	}
	pesSvc := pesanan.NewService(e.pool, nil, pesanan.Config{})
	rel, err := pesSvc.ReleaseExpiredStockHolds(context.Background())
	if err != nil || rel != 1 {
		t.Fatalf("expected ReleaseExpiredStockHolds=1, got rel=%d err=%v", rel, err)
	}
	snap := lookupProdukSnapshot(t, e, sku)
	if int(snap["stok_tertahan"].(float64)) != 0 || int(snap["stok_tersedia"].(float64)) != 2 {
		t.Fatalf("expected released stock hold (tertahan=0, tersedia=2), got %v", snap)
	}

	// Pembeli 2 langsung checkout & melunasi 2 unit terakhir
	o2St, o2Body, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/toko/checkout", mem2Tok, map[string]interface{}{
		"items": []map[string]interface{}{{"produk_id": prodID, "kuantitas": 2}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
		"nama_penerima": "Anggota Dua", "hp_penerima": "08110000005",
	}, map[string]string{"Idempotency-Key": "exp2-" + uuid.NewString()})
	if o2St != http.StatusCreated {
		t.Fatalf("checkout 2 failed: %d", o2St)
	}
	ord2ID := o2Body["data"].(map[string]interface{})["id"].(string)
	payOrderAndApprove(t, e, mem2Tok, adminTok, ord2ID)
	snap = lookupProdukSnapshot(t, e, sku)
	if int(snap["stok"].(float64)) != 0 || int(snap["stok_tersedia"].(float64)) != 0 {
		t.Fatalf("expected stok=0 after pembeli 2 paid, got %v", snap)
	}

	// Admin sekarang memverifikasi Pembayaran 1 ('setuju') setelah stok terbeli habis -> wajib 200 status=perlu_tindak_lanjut!
	vSt, vBody, vRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran/"+pay1ID+"/verifikasi", adminTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nom1, "alasan": "Dana QRIS diterima namun terlambat verifikasi",
	}, nil)
	if vSt != http.StatusOK {
		t.Fatalf("approval after stock runout must return 200 perlu_tindak_lanjut, got %d body=%s", vSt, string(vRaw))
	}
	vData := vBody["data"].(map[string]interface{})
	if vData["status"] != "perlu_tindak_lanjut" {
		t.Fatalf("expected status=perlu_tindak_lanjut when stock insufficient after hold release, got %v", vData)
	}
}

func createAndUploadQRISOrderPayment(t *testing.T, e *e2eEnv, memTok, ordID string) (string, int64) {
	t.Helper()
	pSt, pBody, pRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran", memTok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": ordID,
	}, map[string]string{"Idempotency-Key": "qpay-" + uuid.NewString()})
	if pSt != http.StatusCreated {
		t.Fatalf("create qris order payment failed: %d %s", pSt, string(pRaw))
	}
	payObj := pBody["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})
	payID := payObj["id"].(string)
	nomTotal := int64(payObj["nominal_total"].(float64))
	uSt, _, uRaw := doMultipartReq(t, e.httpSrv.URL+"/api/v1/pembayaran", memTok, "file", "bukti_ord.png", makeUniquePNG(t),
		map[string]string{"pembayaran_id": payID}, nil)
	if uSt != http.StatusOK {
		t.Fatalf("upload bukti failed: %d %s", uSt, string(uRaw))
	}
	return payID, nomTotal
}

func payOrderAndApprove(t *testing.T, e *e2eEnv, memTok, adminTok, ordID string) {
	t.Helper()
	payID, nomTotal := createAndUploadQRISOrderPayment(t, e, memTok, ordID)
	vSt, _, vRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran/"+payID+"/verifikasi", adminTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nomTotal, "alasan": "Pembayaran valid",
	}, nil)
	if vSt != http.StatusOK {
		t.Fatalf("verify payment failed: %d %s", vSt, string(vRaw))
	}
}
