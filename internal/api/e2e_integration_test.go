package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/testseed"
)

type e2eEnv struct {
	appCfg  config.AppConfig
	pool    *pgxpool.Pool
	httpSrv *httptest.Server
	kycDir  string
	seedPwd string
	res     testseed.ResourceIDs
}

func setupE2EEnv(t *testing.T, relEnvPath string) *e2eEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("melewati uji integrasi DB pada mode -short")
	}
	root := repoRootDir(t)
	appCfg, err := config.LoadFromEnvAndFile(filepath.Join(root, relEnvPath))
	if err != nil {
		t.Fatalf("LoadFromEnvAndFile(%s): %v", relEnvPath, err)
	}
	dsn, err := appCfg.BuildDatabaseDSN()
	if err != nil {
		t.Fatalf("BuildDatabaseDSN: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.CheckDatabaseRole(ctx, pool, false); err != nil {
		t.Fatalf("CheckDatabaseRole (%s): %v", relEnvPath, err)
	}
	seedPwd, err := testseed.SeedAll(ctx, pool, appCfg, "")
	if err != nil {
		t.Fatalf("testseed.SeedAll (%s): %v", relEnvPath, err)
	}
	kycDir := t.TempDir()
	apiCfg := ConfigFromAppConfig(appCfg)
	apiCfg.StorageKYCDir = kycDir
	srv := NewServer(pool, apiCfg)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	return &e2eEnv{
		appCfg: appCfg, pool: pool, httpSrv: ts,
		kycDir: kycDir, seedPwd: seedPwd, res: testseed.LookupResources(ctx, pool),
	}
}

func makeUniquePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var rb [4]byte
	_, _ = rand.Read(rb[:])
	img.Set(0, 0, color.RGBA{R: rb[0], G: rb[1], B: rb[2], A: 255})
	img.Set(3, 3, color.RGBA{R: rb[3], G: 120, B: 80, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func doJSONReq(t *testing.T, method, url, token string, body interface{}, hdrs map[string]string) (int, map[string]interface{}, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]interface{}
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed, raw
}

func doMultipartReq(t *testing.T, url, token, fileField, filename string, fileBytes []byte, fields, hdrs map[string]string) (int, map[string]interface{}, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, err := mw.CreateFormFile(fileField, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = fw.Write(fileBytes)
	_ = mw.Close()
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		t.Fatalf("NewRequest multipart: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST multipart %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]interface{}
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed, raw
}

func loginSession(t *testing.T, e *e2eEnv, email, password, totpSecret string) string {
	t.Helper()
	st, body, raw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/login", "", map[string]string{
		"identifier": email, "password": password,
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("login %s status=%d body=%s", email, st, string(raw))
	}
	data := body["data"].(map[string]interface{})
	tok := data["session_token"].(string)
	if data["scope"] == "mfa_pending" {
		code, err := security.GenerateTOTPCode(totpSecret, time.Now().UTC())
		if err != nil {
			t.Fatalf("GenerateTOTPCode: %v", err)
		}
		mst, _, mraw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/mfa/verify", tok, map[string]string{"code": code}, nil)
		if mst != http.StatusOK {
			t.Fatalf("mfa/verify %s status=%d body=%s", email, mst, string(mraw))
		}
	}
	return tok
}

func randomDigits12() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(900_000_000_000))
	return fmt.Sprintf("%012d", n.Int64()+100_000_000_000)
}

func TestE2E_SetA_PublicConfig_MFA_SetupReplay_And_CGNATLockout(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")

	// 1. GET /api/v1/public/config - no secret leaks
	st, cfgBody, rawCfg := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/public/config", "", nil, nil)
	if st != http.StatusOK || cfgBody["success"] != true {
		t.Fatalf("public/config status=%d body=%s", st, string(rawCfg))
	}
	for _, secretStr := range []string{e.appCfg.AESMasterKey, e.appCfg.BlindIndexSecret, e.appCfg.DBPassword} {
		if secretStr != "" && bytes.Contains(rawCfg, []byte(secretStr)) {
			t.Fatalf("secret leaked in public/config!")
		}
	}

	// 2. Admin MFA scope guard & fresh Bendahara MFA setup + TOTP replay prevention
	adminLoginSt, adminLogin, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/login", "", map[string]string{
		"identifier": "admin@kopdes-test.local", "password": e.seedPwd,
	}, nil)
	if adminLoginSt != http.StatusOK {
		t.Fatalf("admin login status=%d", adminLoginSt)
	}
	pendTok := adminLogin["data"].(map[string]interface{})["session_token"].(string)
	if guardSt, _, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/users", pendTok, nil, nil); guardSt != http.StatusForbidden && guardSt != http.StatusUnauthorized {
		t.Fatalf("mfa_pending session must be blocked from /users, got %d", guardSt)
	}
	adminCode, _ := security.GenerateTOTPCode(testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"), time.Now().UTC())
	vst, _, vraw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/mfa/verify", pendTok, map[string]string{"code": adminCode}, nil)
	if vst != http.StatusOK {
		t.Fatalf("admin mfa verify failed: %d %s", vst, string(vraw))
	}

	suffix := uuid.NewString()[:8]
	newEmail := "mfa-" + suffix + "@kopdes-test.local"
	newHP := "0818" + randomDigits12()[:8]
	cst, _, craw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/users", pendTok, map[string]string{
		"email": newEmail, "no_hp": newHP, "nama_lengkap": "Staff MFA Uji", "peran": "bendahara", "password": e.seedPwd,
	}, nil)
	if cst != http.StatusCreated {
		t.Fatalf("create user failed: %d %s", cst, string(craw))
	}

	st2, setupLogin, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/login", "", map[string]string{
		"identifier": newEmail, "password": e.seedPwd,
	}, nil)
	if st2 != http.StatusOK || setupLogin["data"].(map[string]interface{})["scope"] != "mfa_setup" {
		t.Fatalf("expected mfa_setup scope for new bendahara, got %d %v", st2, setupLogin)
	}
	setupSession := setupLogin["data"].(map[string]interface{})["session_token"].(string)
	sst, sbody, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/mfa/setup", setupSession, map[string]string{}, nil)
	if sst != http.StatusOK {
		t.Fatalf("mfa/setup init failed: %d", sst)
	}
	sdata := sbody["data"].(map[string]interface{})
	totpSec := sdata["secret"].(string)
	setupTok := sdata["setup_token"].(string)
	firstCode, _ := security.GenerateTOTPCode(totpSec, time.Now().UTC())
	cst2, _, craw2 := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/mfa/setup", setupSession, map[string]string{
		"code": firstCode, "setup_token": setupTok,
	}, nil)
	if cst2 != http.StatusOK {
		t.Fatalf("mfa/setup confirm failed: %d %s", cst2, string(craw2))
	}

	// Re-login & replay exact same TOTP code -> must be rejected 401
	_, relogin, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/login", "", map[string]string{
		"identifier": newEmail, "password": e.seedPwd,
	}, nil)
	relTok := relogin["data"].(map[string]interface{})["session_token"].(string)
	rst, _, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/mfa/verify", relTok, map[string]string{"code": firstCode}, nil)
	if rst != http.StatusUnauthorized {
		t.Fatalf("replayed TOTP code must return 401, got %d", rst)
	}

	// 3. CGNAT lockout: repeated wrong attempts for one user behind shared IP must not block anggota2
	sharedHdr := map[string]string{"X-Forwarded-For": "203.0.113.77"}
	for i := 0; i < e.appCfg.LoginMaxAttempts+1; i++ {
		doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/login", "", map[string]string{
			"identifier": "anggota1@kopdes-test.local", "password": "wrong-password-123",
		}, sharedHdr)
	}
	lockSt, _, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/login", "", map[string]string{
		"identifier": "anggota1@kopdes-test.local", "password": e.seedPwd,
	}, sharedHdr)
	if lockSt != http.StatusTooManyRequests {
		t.Fatalf("expected locked 429 for anggota1, got %d", lockSt)
	}
	okSt, _, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/login", "", map[string]string{
		"identifier": "anggota2@kopdes-test.local", "password": e.seedPwd,
	}, sharedHdr)
	if okSt != http.StatusOK {
		t.Fatalf("CGNAT neighbor anggota2 on same IP must still login 200, got %d", okSt)
	}
}

func TestE2E_SetA_Pendaftaran_KYC_Activation_And_SuspendRevocation(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")
	adminTok := loginSession(t, e, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"))

	nik := "3201" + randomDigits12()
	hp := "0817" + randomDigits12()[:8]
	email := "calon-" + uuid.NewString()[:8] + "@kopdes-test.local"
	rst, regBody, regRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/public/pendaftaran", "", map[string]string{
		"nama_lengkap": "Siti Uji E2E", "email": email, "nik": nik, "no_hp": hp,
		"tanggal_lahir": "1994-03-14", "alamat": "Dusun Uji RT 02", "pekerjaan": "Pedagang",
	}, nil)
	if rst != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", rst, string(regRaw))
	}
	regData := regBody["data"].(map[string]interface{})
	regID := regData["id"].(string)
	uploadTok := regData["upload_token"].(string)

	rawPNG := makeUniquePNG(t)
	ust, _, uraw := doMultipartReq(t, e.httpSrv.URL+"/api/v1/public/pendaftaran/"+regID+"/dokumen", "", "file", "ktp.png", rawPNG,
		map[string]string{"tipe_dokumen": "ktp"}, map[string]string{"X-Upload-Token": uploadTok})
	if ust != http.StatusCreated {
		t.Fatalf("upload kyc status=%d body=%s", ust, string(uraw))
	}

	// Verify encrypted .enc on disk inside separate kyc subdir has no raw PNG magic bytes
	kycSubDir := filepath.Join(e.kycDir, "kyc")
	entries, err := os.ReadDir(kycSubDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected encrypted KYC file in %s, err=%v count=%d", kycSubDir, err, len(entries))
	}
	encBytes, _ := os.ReadFile(filepath.Join(kycSubDir, entries[0].Name()))
	if bytes.HasPrefix(encBytes, []byte{0x89, 'P', 'N', 'G'}) || bytes.Contains(encBytes, []byte(nik)) {
		t.Fatal("KYC .enc file leaked plaintext header or NIK")
	}

	// Admin verifies KYC download + approves registration
	resp, err := http.DefaultClient.Do(mustNewReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/pendaftaran/"+regID+"/dokumen/ktp", adminTok))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("admin fetch kyc status=%v err=%v", resp, err)
	}
	decBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Equal(decBytes, rawPNG) {
		t.Fatal("decrypted KYC bytes do not match uploaded PNG")
	}

	vst, vbody, vraw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pendaftaran/"+regID+"/verifikasi", adminTok, map[string]string{
		"keputusan": "disetujui", "catatan": "KTP jelas",
	}, nil)
	if vst != http.StatusOK {
		t.Fatalf("verify pendaftaran failed: %d %s", vst, string(vraw))
	}
	vdata := vbody["data"].(map[string]interface{})
	angID := vdata["anggota_id"].(string)
	actCode := vdata["activation_code"].(string)

	// Activate password + login + admin suspend -> existing session must be revoked (401)
	newPwd := "SandiAnggotaBaru2026!"
	pst, _, praw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/auth/password", "", map[string]string{
		"code": actCode, "new_password": newPwd,
	}, nil)
	if pst != http.StatusOK {
		t.Fatalf("activate password failed: %d %s", pst, string(praw))
	}
	memTok := loginSession(t, e, email, newPwd, "")
	if mst, _, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/auth/me", memTok, nil, nil); mst != http.StatusOK {
		t.Fatalf("member /me before suspend expected 200, got %d", mst)
	}
	sst, _, sraw := doJSONReq(t, http.MethodPatch, e.httpSrv.URL+"/api/v1/anggota/"+angID+"/status", adminTok, map[string]string{
		"status": "nonaktif", "alasan": "Uji pencabutan sesi",
	}, nil)
	if sst != http.StatusOK {
		t.Fatalf("suspend member failed: %d %s", sst, string(sraw))
	}
	if revSt, _, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/auth/me", memTok, nil, nil); revSt != http.StatusUnauthorized {
		t.Fatalf("member session after suspend must be 401, got %d", revSt)
	}
}

func TestE2E_SetA_Iuran_Keringanan_QRIS_DuplicateProof_DualControl(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")
	adminTok := loginSession(t, e, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"))
	bendTok := loginSession(t, e, "bendahara@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "bendahara@kopdes-test.local"))
	memTok := loginSession(t, e, "anggota1@kopdes-test.local", e.seedPwd, "")

	nRand, _ := rand.Int(rand.Reader, big.NewInt(60))
	mRand, _ := rand.Int(rand.Reader, big.NewInt(12))
	periode := fmt.Sprintf("%04d-%02d", 2032+nRand.Int64(), 1+mRand.Int64())

	gst, _, graw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/iuran/jenis", bendTok, map[string]string{
		"action": "generate", "periode": periode,
	}, nil)
	if gst != http.StatusOK && gst != http.StatusCreated {
		t.Fatalf("iuran generate failed: %d %s", gst, string(graw))
	}

	lst, lbody, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/iuran/tagihan?status=belum_bayar&limit=10", memTok, nil, nil)
	if lst != http.StatusOK {
		t.Fatalf("list tagihan member status=%d", lst)
	}
	tags := lbody["data"].([]interface{})
	if len(tags) < 1 {
		t.Fatal("expected at least 1 unpaid tagihan for anggota1")
	}
	tagID := tags[0].(map[string]interface{})["id"].(string)

	kst, _, kraw := doJSONReq(t, http.MethodPatch, e.httpSrv.URL+"/api/v1/iuran/tagihan/"+tagID+"/keringanan", bendTok, map[string]interface{}{
		"nominal_keringanan": 5000, "alasan": "Keringanan E2E",
	}, nil)
	if kst != http.StatusOK {
		t.Fatalf("keringanan failed: %d %s", kst, string(kraw))
	}

	// Member creates QRIS payment
	pst, pbody, praw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran", memTok, map[string]interface{}{
		"jenis_transaksi": "iuran", "metode": "qris", "tagihan_ids": []string{tagID},
	}, map[string]string{"Idempotency-Key": "pay-" + uuid.NewString()})
	if pst != http.StatusCreated {
		t.Fatalf("create qris payment failed: %d %s", pst, string(praw))
	}
	payObj := pbody["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})
	payID := payObj["id"].(string)
	nomTotal := int64(payObj["nominal_total"].(float64))
	kodeUnik := int64(payObj["kode_unik"].(float64))
	if kodeUnik < 1 || kodeUnik > 999 || nomTotal != 20000+kodeUnik {
		t.Fatalf("unexpected unique code/total: kode=%d total=%d", kodeUnik, nomTotal)
	}

	// Upload unique proof PNG -> 200; verify encrypted .enc in separate bukti subdir + download decrypts PNG
	proofPNG := makeUniquePNG(t)
	ust, _, uraw := doMultipartReq(t, e.httpSrv.URL+"/api/v1/pembayaran", memTok, "file", "bukti.png", proofPNG,
		map[string]string{"pembayaran_id": payID, "catatan_pembayar": "Transfer QRIS"}, nil)
	if ust != http.StatusOK {
		t.Fatalf("upload bukti failed: %d %s", ust, string(uraw))
	}
	proofEncPath := filepath.Join(e.kycDir, "bukti", "bukti_"+payID+".enc")
	proofEncBytes, perr := os.ReadFile(proofEncPath)
	if perr != nil || bytes.HasPrefix(proofEncBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("expected encrypted proof file at %s without PNG header, err=%v", proofEncPath, perr)
	}
	presp, derr := http.DefaultClient.Do(mustNewReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/pembayaran?bukti_id="+payID, adminTok))
	if derr != nil || presp.StatusCode != http.StatusOK {
		t.Fatalf("admin fetch bukti status=%v err=%v", presp, derr)
	}
	decProof, _ := io.ReadAll(presp.Body)
	_ = presp.Body.Close()
	if !bytes.Equal(decProof, proofPNG) {
		t.Fatal("decrypted proof bytes do not match uploaded PNG")
	}

	// Verify dual-control violation when Bendahara creates tunai payment then tries verifying own payment
	mem2Tok := loginSession(t, e, "anggota2@kopdes-test.local", e.seedPwd, "")
	l2st, l2body, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/iuran/tagihan?status=belum_bayar&limit=5", mem2Tok, nil, nil)
	if l2st == http.StatusOK {
		tags2 := l2body["data"].([]interface{})
		if len(tags2) > 0 {
			t2ID := tags2[0].(map[string]interface{})["id"].(string)
			p2st, p2body, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran", bendTok, map[string]interface{}{
				"jenis_transaksi": "iuran", "metode": "qris", "tagihan_ids": []string{t2ID},
			}, map[string]string{"Idempotency-Key": "pay2-" + uuid.NewString()})
			if p2st == http.StatusCreated {
				p2ID := p2body["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})["id"].(string)
				dcSt, _, _ := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran/"+p2ID+"/verifikasi", bendTok, map[string]interface{}{
					"keputusan": "setuju", "nominal_dibayar": 25000,
				}, nil)
				if dcSt != http.StatusForbidden {
					t.Fatalf("self-verification must fail dual control 403, got %d", dcSt)
				}
				dupSt, _, _ := doMultipartReq(t, e.httpSrv.URL+"/api/v1/pembayaran", adminTok, "file", "dup.png", proofPNG,
					map[string]string{"pembayaran_id": p2ID}, nil)
				if dupSt != http.StatusConflict {
					t.Fatalf("duplicate proof hash must return 409, got %d", dupSt)
				}
			}
		}
	}

	// Admin (not creator) verifies anggota1's payment -> lunas + kwitansi
	vst, vbody, vraw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran/"+payID+"/verifikasi", adminTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nomTotal, "alasan": "Dana sesuai mutasi",
	}, nil)
	if vst != http.StatusOK {
		t.Fatalf("verify payment failed: %d %s", vst, string(vraw))
	}
	vdata := vbody["data"].(map[string]interface{})
	if vdata["status"] != "lunas" || !strings.HasPrefix(fmt.Sprint(vdata["nomor_kwitansi"]), "KWT-") {
		t.Fatalf("expected lunas + KWT kwitansi, got %v", vdata)
	}
}

func TestE2E_SetA_Katalog_Checkout_DaftarAntar_And_SetB_Isolation(t *testing.T) {
	eA := setupE2EEnv(t, ".env.test")
	petugasTok := loginSession(t, eA, "petugas@kopdes-test.local", eA.seedPwd, "")
	memTok := loginSession(t, eA, "anggota1@kopdes-test.local", eA.seedPwd, "")

	// Product with harga_umum=20000 and harga_anggota=nil -> 409 MISSING_ACTIVE_PRICE for member
	sku := "E2E-" + strings.ToUpper(uuid.NewString()[:8])
	hargaUmum := int64(20000)
	cpSt, cpBody, cpRaw := doJSONReq(t, http.MethodPost, eA.httpSrv.URL+"/api/v1/katalog/produk", petugasTok, map[string]interface{}{
		"sku": sku, "nama": "Gula Uji E2E", "satuan": "kg", "harga_umum": hargaUmum, "stok": 10, "status": "aktif",
	}, nil)
	if cpSt != http.StatusCreated {
		t.Fatalf("create produk failed: %d %s", cpSt, string(cpRaw))
	}
	prodID := cpBody["data"].(map[string]interface{})["id"].(string)

	mFailSt, _, _ := doJSONReq(t, http.MethodPost, eA.httpSrv.URL+"/api/v1/toko/checkout", memTok, map[string]interface{}{
		"items": []map[string]interface{}{{"produk_id": prodID, "kuantitas": 1}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "tunai",
		"nama_penerima": "Anggota Satu", "hp_penerima": "08110000004",
	}, nil)
	if mFailSt != http.StatusConflict {
		t.Fatalf("checkout missing member active price must return 409, got %d", mFailSt)
	}

	// Set harga_anggota=15000 then checkout with delivery + CSV formula payload + idempotency
	upSt, _, _ := doJSONReq(t, http.MethodPost, eA.httpSrv.URL+"/api/v1/katalog/produk", petugasTok, map[string]interface{}{
		"action": "update_produk", "id": prodID, "harga_umum": 20000, "harga_anggota": 15000, "stok": 10, "status": "aktif",
	}, nil)
	if upSt != http.StatusOK {
		t.Fatalf("update produk failed: %d", upSt)
	}
	idemKey := "ord-" + uuid.NewString()
	chkBody := map[string]interface{}{
		"items": []map[string]interface{}{{"produk_id": prodID, "kuantitas": 2}},
		"metode_pengiriman": "diantar", "metode_pembayaran": "tunai",
		"nama_penerima": "=CMD()", "hp_penerima": "08110000004", "alamat_pengantaran": "Jl. Mawar No. 9",
	}
	o1St, o1Body, o1Raw := doJSONReq(t, http.MethodPost, eA.httpSrv.URL+"/api/v1/toko/checkout", memTok, chkBody, map[string]string{"Idempotency-Key": idemKey})
	if o1St != http.StatusCreated {
		t.Fatalf("checkout failed: %d %s", o1St, string(o1Raw))
	}
	o2St, o2Body, _ := doJSONReq(t, http.MethodPost, eA.httpSrv.URL+"/api/v1/toko/checkout", memTok, chkBody, map[string]string{"Idempotency-Key": idemKey})
	if o2St != http.StatusOK || o2Body["data"].(map[string]interface{})["id"] != o1Body["data"].(map[string]interface{})["id"] {
		t.Fatalf("idempotent checkout replay failed: %d %v", o2St, o2Body)
	}

	// Verify stock is NOT deducted at checkout (still 10), then deducts to 8 when processed, and QRIS order deducts to 5 only after payment approval
	if got := lookupProdukStock(t, eA, sku); got != 10 {
		t.Fatalf("expected stock unchanged (10) right after checkout, got %d", got)
	}

	// Transition cash order to diproses -> deducts stock (10 -> 8), then check daftar-antar CSV neutralization + laporan penjualan
	ordID := o1Body["data"].(map[string]interface{})["id"].(string)
	pst, _, praw := doJSONReq(t, http.MethodPatch, eA.httpSrv.URL+"/api/v1/pesanan/"+ordID+"/status", petugasTok, map[string]string{
		"status": "diproses", "catatan": "Siap diantar E2E",
	}, nil)
	if pst != http.StatusOK {
		t.Fatalf("update pesanan status failed: %d %s", pst, string(praw))
	}
	if got := lookupProdukStock(t, eA, sku); got != 8 {
		t.Fatalf("expected stock 8 after cash order diproses, got %d", got)
	}
	verifyQRISOrderStockDeduction(t, eA, memTok, prodID, sku, 8)
	_, _, csvRaw := doJSONReq(t, http.MethodGet, eA.httpSrv.URL+"/api/v1/pesanan/daftar-antar?format=csv", petugasTok, nil, nil)
	if !bytes.Contains(csvRaw, []byte("'=CMD()")) {
		t.Fatalf("expected formula neutralization in daftar-antar CSV:\n%s", string(csvRaw))
	}
	if repSt, _, _ := doJSONReq(t, http.MethodGet, eA.httpSrv.URL+"/api/v1/laporan?jenis=penjualan&format=csv", petugasTok, nil, nil); repSt != http.StatusOK {
		t.Fatalf("laporan penjualan CSV status=%d", repSt)
	}

	// Set B Isolation (port 15433, payment expiry 20m = 1200s, pilot false, different AES keys)
	eB := setupE2EEnv(t, ".env.test.alt")
	_, cfgB, _ := doJSONReq(t, http.MethodGet, eB.httpSrv.URL+"/api/v1/public/config", "", nil, nil)
	dataB := cfgB["data"].(map[string]interface{})
	featsB := dataB["features"].(map[string]interface{})
	if int64(dataB["payment_expiry_seconds"].(float64)) != 1200 || featsB["personal_payment_pilot_enabled"] != false {
		t.Fatalf("Set B config isolation mismatch: %v", dataB)
	}
	vA, kA, mA := security.ParseMasterKeySpec(eA.appCfg.AESMasterKey)
	kpA, _ := security.NewMultiKeyProviderFromConfig(vA, kA, mA, "")
	vB, kB, mB := security.ParseMasterKeySpec(eB.appCfg.AESMasterKey)
	kpB, _ := security.NewMultiKeyProviderFromConfig(vB, kB, mB, "")
	ctA, _ := kpA.EncryptWithAAD([]byte("rahasia-set-a"), []byte("aad"))
	if _, decErr := kpB.DecryptWithAAD(ctA, []byte("aad")); decErr == nil {
		t.Fatal("Set B key must fail decrypting Set A ciphertext when Set A key is excluded")
	}
}

func mustNewReq(t *testing.T, method, url, bearer string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	return req
}

func lookupProdukStock(t *testing.T, e *e2eEnv, sku string) int {
	t.Helper()
	st, body, raw := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/katalog/produk?q="+sku, "", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("lookup produk %s failed: %d %s", sku, st, string(raw))
	}
	items := body["data"].([]interface{})
	if len(items) == 0 {
		t.Fatalf("produk %s not found", sku)
	}
	return int(items[0].(map[string]interface{})["stok"].(float64))
}

func verifyQRISOrderStockDeduction(t *testing.T, e *e2eEnv, memTok, prodID, sku string, curStock int) {
	t.Helper()
	adminTok := loginSession(t, e, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local"))
	oSt, oBody, oRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/toko/checkout", memTok, map[string]interface{}{
		"items": []map[string]interface{}{{"produk_id": prodID, "kuantitas": 3}},
		"metode_pengiriman": "ambil_sendiri", "metode_pembayaran": "qris",
		"nama_penerima": "Anggota Satu", "hp_penerima": "08110000004",
	}, map[string]string{"Idempotency-Key": "qris-ord-" + uuid.NewString()})
	if oSt != http.StatusCreated {
		t.Fatalf("qris checkout failed: %d %s", oSt, string(oRaw))
	}
	ordID := oBody["data"].(map[string]interface{})["id"].(string)
	if got := lookupProdukStock(t, e, sku); got != curStock {
		t.Fatalf("expected stock %d after qris checkout (not yet verified), got %d", curStock, got)
	}
	pSt, pBody, pRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran", memTok, map[string]interface{}{
		"jenis_transaksi": "pesanan", "metode": "qris", "pesanan_id": ordID,
	}, map[string]string{"Idempotency-Key": "qris-pay-" + uuid.NewString()})
	if pSt != http.StatusCreated {
		t.Fatalf("create qris order payment failed: %d %s", pSt, string(pRaw))
	}
	payObj := pBody["data"].(map[string]interface{})["pembayaran"].(map[string]interface{})
	payID := payObj["id"].(string)
	nomTotal := int64(payObj["nominal_total"].(float64))
	uSt, _, uRaw := doMultipartReq(t, e.httpSrv.URL+"/api/v1/pembayaran", memTok, "file", "bukti_ord.png", makeUniquePNG(t),
		map[string]string{"pembayaran_id": payID}, nil)
	if uSt != http.StatusOK {
		t.Fatalf("upload qris order proof failed: %d %s", uSt, string(uRaw))
	}
	if got := lookupProdukStock(t, e, sku); got != curStock {
		t.Fatalf("expected stock %d after proof upload (pending admin decision), got %d", curStock, got)
	}
	vSt, _, vRaw := doJSONReq(t, http.MethodPost, e.httpSrv.URL+"/api/v1/pembayaran/"+payID+"/verifikasi", adminTok, map[string]interface{}{
		"keputusan": "setuju", "nominal_dibayar": nomTotal, "alasan": "Pembayaran pesanan valid",
	}, nil)
	if vSt != http.StatusOK {
		t.Fatalf("verify qris order payment failed: %d %s", vSt, string(vRaw))
	}
	if got := lookupProdukStock(t, e, sku); got != curStock-3 {
		t.Fatalf("expected stock %d after admin setuju, got %d", curStock-3, got)
	}
}
