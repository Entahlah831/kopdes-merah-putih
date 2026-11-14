package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/qris"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/testseed"
)

type scenarioFileSpec struct {
	Version   string             `json:"version"`
	Scenarios []scenarioItemSpec `json:"scenarios"`
}

type scenarioItemSpec struct {
	ClaimID              string             `json:"claim_id"`
	Title                string             `json:"title"`
	Operation            string             `json:"operation"`
	RequiresMetricsToken bool               `json:"requires_metrics_token"`
	Steps                []scenarioStepSpec `json:"steps"`
}

type scenarioStepSpec struct {
	Name                string                 `json:"name"`
	Action              string                 `json:"action"`
	Email               string                 `json:"email"`
	SecretVar           string                 `json:"secret_var"`
	OffsetSeconds       int                    `json:"offset_seconds"`
	SaveAs              string                 `json:"save_as"`
	AsAccount           string                 `json:"as_account"`
	TokenVar            string                 `json:"token_var"`
	ForceLogin          bool                   `json:"force_login"`
	Method              string                 `json:"method"`
	Path                string                 `json:"path"`
	Headers             map[string]string      `json:"headers"`
	Body                interface{}            `json:"body"`
	RawBody             *string                `json:"raw_body"`
	MultipartPNG        bool                   `json:"multipart_png"`
	MultipartCSV        string                 `json:"multipart_csv"`
	FileField           string                 `json:"file_field"`
	ExpectStatus        []int                  `json:"expect_status"`
	ExpectHeaderPresent []string               `json:"expect_header_present"`
	ExpectJSONContains  []string               `json:"expect_json_contains"`
	SaveJSON            map[string]string      `json:"save_json"`
}

type rolesMatrixFullSpec struct {
	Version   string                `json:"version"`
	Roles     []string              `json:"roles"`
	Endpoints []rolesMatrixRowFull  `json:"endpoints"`
}

type rolesMatrixRowFull struct {
	Method           string                 `json:"method"`
	Path             string                 `json:"path"`
	Group            string                 `json:"group"`
	AuthMode         string                 `json:"auth_mode"`
	SamplePath       string                 `json:"sample_path"`
	SampleBody       map[string]interface{} `json:"sample_body"`
	Roles            map[string]interface{} `json:"roles"`
	Unauthenticated  interface{}            `json:"unauthenticated"`
	CrossTenant      string                 `json:"cross_tenant"`
	CrossTenantProbe *struct {
		Path   string `json:"path"`
		Expect []int  `json:"expect"`
	} `json:"cross_tenant_probe"`
}

func e2eLoadDefaultTestConfig(t testing.TB, kycDir string) config.AppConfig {
	t.Helper()
	cfg, err := config.Load(func(key string) (string, bool) {
		overrides := map[string]string{
			"APP_ENV":                      "test",
			"APP_TIMEZONE":                 "Asia/Jakarta",
			"DB_NAME":                      "kopdes_test",
			"DB_USER":                      "kopdes_app",
			"DB_PASSWORD":                  "test_only_db_pwd",
			"JWT_SECRET":                   "test_jwt_secret_key_for_v5_e2e_suite_minimum_32bytes!",
			"JWT_ALGORITHM":                "HS256",
			"JWT_ISSUER":                   "kopdes-merah-putih",
			"AES_MASTER_KEY":               "test_aes_master_key_for_v5_e2e_minimum_32bytes!",
			"AES_KEY_VERSION":              "v1",
			"AES_KEY_ID":                   "k1",
			"BLIND_INDEX_SECRET":           "test_blind_index_secret_v5_e2e_32bytes!",
			"QRIS_SECRET":                  "test_qris_secret_v5_e2e_minimum_32bytes!",
			"METRICS_TOKEN":                "test_metrics_token_v5_e2e_32bytes_min!",
			"BCRYPT_COST":                  "4",
			"TEST_SEED_PASSWORD":           "SeedPassword123!",
			"ALLOWED_ORIGINS":              "http://127.0.0.1:18090",
			"ALLOW_PERSONAL_PAYMENT_PILOT": "false",
			"RATE_LIMIT_RPS":               "5000",
			"RATE_LIMIT_BURST":             "5000",
			"RATE_LIMIT_MAX_CLIENTS":       "10000",
			"STORAGE_KYC_DIR":              kycDir,
		}
		v, ok := overrides[key]
		return v, ok
	})
	if err != nil {
		t.Fatalf("config.Load gagal: %v", err)
	}
	return cfg
}

func e2eBuildStaticQRIS(merchantName string) string {
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	tag59 := fmt.Sprintf("59%02d%s", len(merchantName), merchantName)
	prefix := fmt.Sprintf("000201010211%s5204541153033605802ID%s6007Jakarta6304", tag26, tag59)
	return prefix + qris.CalculateCRC16(prefix)
}

func e2eTinyPNGBytes(t testing.TB) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode tiny png: %v", err)
	}
	return buf.Bytes()
}

func e2eLoginAccount(t testing.TB, client *http.Client, baseURL string, acc testseed.AccountSpec, password string, cfg config.AppConfig, totpOffsets map[string]int) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"email":    acc.Email,
		"password": password,
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/api/v1/auth/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("login %s HTTP error: %v", acc.Key, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s gagal status %d: %s", acc.Key, resp.StatusCode, string(body))
	}
	var loginOut struct {
		Data struct {
			AccessToken     string `json:"access_token"`
			MFARequired     bool   `json:"mfa_required"`
			MFASessionToken string `json:"mfa_session_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &loginOut); err != nil {
		t.Fatalf("decode login %s: %v", acc.Key, err)
	}
	if !loginOut.Data.MFARequired {
		return loginOut.Data.AccessToken
	}

	stepOffset := totpOffsets[acc.Email]
	totpOffsets[acc.Email] = stepOffset + 30
	targetTime := time.Now().Add(time.Duration(stepOffset) * time.Second)
	sec := testseed.DeterministicTOTPSecret(cfg, acc.Email)
	code, err := security.GenerateTOTPCode(sec, targetTime)
	if err != nil {
		t.Fatalf("generate TOTP %s: %v", acc.Key, err)
	}

	mfaPayload, _ := json.Marshal(map[string]string{
		"mfa_session_token": loginOut.Data.MFASessionToken,
		"totp_code":         code,
	})
	mfaReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/api/v1/auth/mfa/verify", bytes.NewReader(mfaPayload))
	mfaReq.Header.Set("Content-Type", "application/json")
	mfaResp, err := client.Do(mfaReq)
	if err != nil {
		t.Fatalf("mfa verify %s HTTP error: %v", acc.Key, err)
	}
	defer mfaResp.Body.Close()
	mfaBody, _ := io.ReadAll(mfaResp.Body)
	if mfaResp.StatusCode != http.StatusOK {
		t.Fatalf("mfa verify %s status %d: %s", acc.Key, mfaResp.StatusCode, string(mfaBody))
	}
	var verifyOut struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(mfaBody, &verifyOut); err != nil || verifyOut.Data.AccessToken == "" {
		t.Fatalf("decode mfa verify %s: %v body=%s", acc.Key, err, string(mfaBody))
	}
	return verifyOut.Data.AccessToken
}

func e2eMatchMatrixExpectation(exp interface{}, actual int) (bool, string) {
	switch v := exp.(type) {
	case float64:
		want := int(v)
		return actual == want, fmt.Sprintf("%d", want)
	case string:
		if v == "allow" {
			return actual != http.StatusUnauthorized && actual != http.StatusForbidden && actual < 500, "allow(!401,!403,<500)"
		}
		return false, v
	case []interface{}:
		allowed := make([]int, 0, len(v))
		for _, item := range v {
			if f, ok := item.(float64); ok {
				allowed = append(allowed, int(f))
				if int(f) == actual {
					return true, fmt.Sprintf("%v", allowed)
				}
			}
		}
		return false, fmt.Sprintf("%v", allowed)
	default:
		return false, fmt.Sprintf("%v", exp)
	}
}

func e2eExtractJSONPath(doc interface{}, path string) (string, bool) {
	parts := strings.Split(path, ".")
	cur := doc
	for _, p := range parts {
		switch node := cur.(type) {
		case map[string]interface{}:
			nxt, ok := node[p]
			if !ok {
				return "", false
			}
			cur = nxt
		case []interface{}:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(node) {
				return "", false
			}
			cur = node[idx]
		default:
			return "", false
		}
	}
	switch val := cur.(type) {
	case string:
		return val, true
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10), true
		}
		return strconv.FormatFloat(val, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(val), true
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}

func TestV5_LiveScenariosAndRolesMatrix_E2E(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	var currentDB string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB); err != nil {
		t.Fatalf("SELECT current_database(): %v", err)
	}

	appCfg := e2eLoadDefaultTestConfig(t, t.TempDir())
	appCfg.DBName = currentDB

	seedPass, err := testseed.SeedAll(ctx, pool, appCfg, "")
	if err != nil {
		t.Fatalf("testseed.SeedAll awal gagal: %v", err)
	}

	// Lookup ID produk Kop B untuk probe lintas-tenant /katalog/harga
	var produkBID uuid.UUID
	txLookup, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lookup Kop B: %v", err)
	}
	_, _ = txLookup.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", testseed.KoperasiBID.String())
	if err := txLookup.QueryRow(ctx, "SELECT id FROM produk WHERE koperasi_id = $1 AND deleted_at IS NULL LIMIT 1", testseed.KoperasiBID).Scan(&produkBID); err != nil {
		_ = txLookup.Rollback(ctx)
		t.Fatalf("lookup produk Kop B gagal: %v", err)
	}
	_ = txLookup.Commit(ctx)

	srv := api.NewServer(pool, api.ConfigFromAppConfig(appCfg))
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	client := ts.Client()

	accountsByKey := make(map[string]testseed.AccountSpec)
	userIDByRoleA := make(map[string]testseed.AccountSpec)
	for _, acc := range testseed.Accounts() {
		accountsByKey[acc.Key] = acc
		if acc.KoperasiID == testseed.KoperasiAID && acc.Key != "admin_nomfa_a" && acc.Key != "pengurus2_a" {
			userIDByRoleA[acc.Role] = acc
		}
	}

	totpOffsets := make(map[string]int)
	resources := testseed.Resources()
	forbiddenLeaks := []string{
		"3201000000001001",
		"3201000000002001",
		seedPass,
		"password_hash",
		appCfg.JWTSecret,
		appCfg.AESMasterKey,
	}
	rawResponses := make([]string, 0, 512)
	checkBodyNoLeaks := func(label, body string) {
		rawResponses = append(rawResponses, body)
		for _, leak := range forbiddenLeaks {
			if leak != "" && strings.Contains(body, leak) {
				t.Fatalf("KEBOCORAN PII/RAHASIA pada %s: mengandung %q", label, leak)
			}
		}
	}

	// -----------------------------------------------------------------------
	// PART A: Eksekusi langsung 100% docs/roles_matrix.json (54 operasi x 8 subjek + 7 cross-tenant)
	// -----------------------------------------------------------------------
	rawMatrix, err := os.ReadFile(filepath.Join("..", "..", "docs", "roles_matrix.json"))
	if err != nil {
		t.Fatalf("baca docs/roles_matrix.json gagal: %v", err)
	}
	var matrix rolesMatrixFullSpec
	if err := json.Unmarshal(rawMatrix, &matrix); err != nil {
		t.Fatalf("parse docs/roles_matrix.json gagal: %v", err)
	}

	roleTokensA := make(map[string]string)
	for _, r := range matrix.Roles {
		acc, ok := userIDByRoleA[r]
		if !ok {
			t.Fatalf("akun Kop A untuk role %s tidak ditemukan di seed", r)
		}
		roleTokensA[r] = e2eLoginAccount(t, client, ts.URL, acc, seedPass, appCfg, totpOffsets)
	}

	replaceMatrixVars := func(p string) string {
		r := strings.NewReplacer(
			"{anggota_a_id}", resources.AnggotaAID,
			"{produk_a_id}", resources.ProdukAID,
			"{user_admin_a_id}", accountsByKey["admin_a"].UserID.String(),
			"{anggota_b_id}", resources.AnggotaBID,
			"{produk_b_id}", produkBID.String(),
			"{user_admin_b_id}", accountsByKey["admin_b"].UserID.String(),
			"{pembayaran_b_id}", resources.PembayaranBID,
			"{rekening_qris_b_id}", resources.RekeningQRISBID,
		)
		return r.Replace(p)
	}

	doMatrixReq := func(method, relPath, bearer string, sampleBody map[string]interface{}) (int, string) {
		var bodyReader io.Reader
		contentType := ""
		if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
			if sampleBody != nil {
				rawB, _ := json.Marshal(sampleBody)
				bodyReader = bytes.NewReader(rawB)
			} else {
				bodyReader = bytes.NewReader([]byte("{}"))
			}
			contentType = "application/json"
		}
		req, err := http.NewRequestWithContext(ctx, method, ts.URL+relPath, bodyReader)
		if err != nil {
			t.Fatalf("new request %s %s: %v", method, relPath, err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("execute matrix %s %s: %v", method, relPath, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 500 {
			t.Fatalf("matrix %s %s mengembalikan 5xx (%d): %s", method, relPath, resp.StatusCode, string(b))
		}
		checkBodyNoLeaks(method+" "+relPath, string(b))
		return resp.StatusCode, string(b)
	}

	matrixCellCount := 0
	crossTenantCount := 0
	for _, ep := range matrix.Endpoints {
		targetPath := replaceMatrixVars(ep.SamplePath)

		unauthCode, unauthBody := doMatrixReq(ep.Method, targetPath, "", ep.SampleBody)
		if ok, desc := e2eMatchMatrixExpectation(ep.Unauthenticated, unauthCode); !ok {
			t.Fatalf("MATRIX UNAUTH MISMATCH %s %s: expected %s, got %d (%s)", ep.Method, targetPath, desc, unauthCode, unauthBody)
		}
		matrixCellCount++

		for _, r := range matrix.Roles {
			expVal, exists := ep.Roles[r]
			if !exists {
				t.Fatalf("matrix %s %s tidak mendefinisikan role %s", ep.Method, ep.Path, r)
			}
			code, rBody := doMatrixReq(ep.Method, targetPath, roleTokensA[r], ep.SampleBody)
			if ok, desc := e2eMatchMatrixExpectation(expVal, code); !ok {
				t.Fatalf("MATRIX ROLE MISMATCH role=%s %s %s: expected %s, got %d (%s)", r, ep.Method, targetPath, desc, code, rBody)
			}
			matrixCellCount++
		}

		if ep.CrossTenantProbe != nil {
			probePath := replaceMatrixVars(ep.CrossTenantProbe.Path)
			probeCode, probeBody := doMatrixReq(ep.Method, probePath, roleTokensA["admin"], ep.SampleBody)
			matched := false
			for _, w := range ep.CrossTenantProbe.Expect {
				if probeCode == w {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("CROSS-TENANT MISMATCH %s %s: expected %v, got %d (%s)", ep.Method, probePath, ep.CrossTenantProbe.Expect, probeCode, probeBody)
			}
			crossTenantCount++
		}
	}

	// -----------------------------------------------------------------------
	// PART B: Browser Hardening Checks (CORS origin/expose + >10MB BodyLimit 413)
	// -----------------------------------------------------------------------
	optAllowed, _ := http.NewRequestWithContext(ctx, http.MethodOptions, ts.URL+"/api/v1/katalog/produk", nil)
	optAllowed.Header.Set("Origin", "http://127.0.0.1:18090")
	optAllowed.Header.Set("Access-Control-Request-Method", "GET")
	resOptAllowed, err := client.Do(optAllowed)
	if err != nil {
		t.Fatalf("CORS allowed preflight error: %v", err)
	}
	resOptAllowed.Body.Close()
	if resOptAllowed.Header.Get("Access-Control-Allow-Origin") != "http://127.0.0.1:18090" {
		t.Fatalf("expected ACAO http://127.0.0.1:18090, got %q", resOptAllowed.Header.Get("Access-Control-Allow-Origin"))
	}
	exposeHdr := resOptAllowed.Header.Get("Access-Control-Expose-Headers")
	if !strings.Contains(exposeHdr, "X-Request-ID") || !strings.Contains(exposeHdr, "Retry-After") {
		t.Fatalf("expected Access-Control-Expose-Headers X-Request-ID & Retry-After, got %q", exposeHdr)
	}

	optEvil, _ := http.NewRequestWithContext(ctx, http.MethodOptions, ts.URL+"/api/v1/katalog/produk", nil)
	optEvil.Header.Set("Origin", "https://evil.example")
	optEvil.Header.Set("Access-Control-Request-Method", "GET")
	resOptEvil, err := client.Do(optEvil)
	if err != nil {
		t.Fatalf("CORS evil preflight error: %v", err)
	}
	resOptEvil.Body.Close()
	if resOptEvil.StatusCode != http.StatusForbidden || resOptEvil.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected 403 & no ACAO for https://evil.example, got status=%d acao=%q", resOptEvil.StatusCode, resOptEvil.Header.Get("Access-Control-Allow-Origin"))
	}

	hugePayload := bytes.Repeat([]byte("A"), 10*1024*1024+1024)
	reqHuge, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/auth/login", bytes.NewReader(hugePayload))
	reqHuge.Header.Set("Content-Type", "application/json")
	resHuge, err := client.Do(reqHuge)
	if err != nil {
		t.Fatalf("oversized payload request error: %v", err)
	}
	hugeBodyBytes, _ := io.ReadAll(resHuge.Body)
	resHuge.Body.Close()
	if resHuge.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected HTTP 413 for >10MB body, got %d (%s)", resHuge.StatusCode, string(hugeBodyBytes))
	}
	checkBodyNoLeaks("oversized-413", string(hugeBodyBytes))

	// -----------------------------------------------------------------------
	// PART C: Re-seed bersih lalu eksekusi seluruh 26 skenario frontend-tester/scenarios.json
	// -----------------------------------------------------------------------
	if _, err := testseed.SeedAll(ctx, pool, appCfg, ""); err != nil {
		t.Fatalf("testseed.SeedAll pra-scenarios gagal: %v", err)
	}
	totpOffsets = make(map[string]int)

	// Pastikan pengulangan go test beruntun (<5 menit) di DB test lokal tidak mewarisi burst >3 transaksi uji sebelumnya
	if txAge, err := pool.Begin(ctx); err == nil {
		_, _ = txAge.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", testseed.KoperasiAID.String())
		_, _ = txAge.Exec(ctx, `UPDATE pembayaran SET status = 'kedaluwarsa', created_at = NOW() - INTERVAL '1 hour' WHERE koperasi_id = $1 AND (status = 'menunggu_pembayaran' OR created_at >= NOW() - INTERVAL '10 minutes');`, testseed.KoperasiAID)
		_ = txAge.Commit(ctx)
	}

	rawScen, err := os.ReadFile(filepath.Join("..", "..", "frontend-tester", "scenarios.json"))
	if err != nil {
		t.Fatalf("baca frontend-tester/scenarios.json gagal: %v", err)
	}
	var scFile scenarioFileSpec
	if err := json.Unmarshal(rawScen, &scFile); err != nil {
		t.Fatalf("parse frontend-tester/scenarios.json gagal: %v", err)
	}

	ctxVars := map[string]string{
		"koperasi_a_id":        testseed.KoperasiAID.String(),
		"koperasi_b_id":        testseed.KoperasiBID.String(),
		"user_admin_a_id":      accountsByKey["admin_a"].UserID.String(),
		"user_admin_b_id":      accountsByKey["admin_b"].UserID.String(),
		"rekening_qris_a_id":   resources.RekeningQRISAID,
		"rekening_qris_b_id":   resources.RekeningQRISBID,
		"metode_pembayaran_a_id": resources.MetodePembayaranAID,
		"jenis_iuran_a_id":     resources.JenisIuranAID,
		"tagihan_iuran_a_id":   resources.TagihanIuranAID,
		"tagihan_iuran_a2_id":  resources.TagihanIuranAID2,
		"produk_a_id":          resources.ProdukAID,
		"produk_b_id":          resources.ProdukBID,
		"promo_a_id":           resources.PromoAID,
		"promo_a_kode":         resources.PromoAKode,
		"anggota_a_id":         resources.AnggotaAID,
		"anggota_b_id":         resources.AnggotaBID,
		"tagihan_iuran_b_id":   resources.TagihanIuranBID,
		"pembayaran_b_id":      resources.PembayaranBID,
		"sample_static_qris":   e2eBuildStaticQRIS("Kopdes Uji Browser"),
		"seed_password":        seedPass,
		"metrics_token":        appCfg.MetricsToken,
	}

	tmplRe := regexp.MustCompile(`\{\{([a-zA-Z0-9_]+)\}\}`)
	nikCounter := 0
	var expandString func(string) string
	expandString = func(in string) string {
		if in == "{{new_uuid}}" {
			return uuid.NewString()
		}
		return tmplRe.ReplaceAllStringFunc(in, func(m string) string {
			k := m[2 : len(m)-2]
			switch k {
			case "new_uuid":
				return uuid.NewString()
			case "unix_ts":
				return strconv.FormatInt(time.Now().UnixNano()/1e6, 10)
			case "new_nik":
				nikCounter++
				nik := fmt.Sprintf("320102%010d", (time.Now().UnixNano()%1000000000)+int64(nikCounter))
				forbiddenLeaks = append(forbiddenLeaks, nik)
				return nik
			default:
				return ctxVars[k]
			}
		})
	}
	var expandAny func(interface{}) interface{}
	expandAny = func(v interface{}) interface{} {
		switch node := v.(type) {
		case string:
			return expandString(node)
		case []interface{}:
			out := make([]interface{}, len(node))
			for i, item := range node {
				out[i] = expandAny(item)
			}
			return out
		case map[string]interface{}:
			out := make(map[string]interface{}, len(node))
			for k, val := range node {
				out[k] = expandAny(val)
			}
			return out
		default:
			return v
		}
	}

	tinyPNG := e2eTinyPNGBytes(t)
	cachedTokens := make(map[string]string)
	scenarioStepsCount := 0

	for _, sc := range scFile.Scenarios {
		for idx, step := range sc.Steps {
			scenarioStepsCount++
			if step.Action == "totp_for_secret" {
				sec := ctxVars[step.SecretVar]
				targetT := time.Now().Add(time.Duration(step.OffsetSeconds) * time.Second)
				code, err := security.GenerateTOTPCode(sec, targetT)
				if err != nil {
					t.Fatalf("[%s step %d %s] totp_for_secret error: %v", sc.ClaimID, idx, step.Name, err)
				}
				ctxVars[step.SaveAs] = code
				continue
			}
			if step.Action == "totp_for_email" {
				email := expandString(step.Email)
				sec := testseed.DeterministicTOTPSecret(appCfg, email)
				targetT := time.Now().Add(time.Duration(step.OffsetSeconds) * time.Second)
				code, err := security.GenerateTOTPCode(sec, targetT)
				if err != nil {
					t.Fatalf("[%s step %d %s] totp_for_email error: %v", sc.ClaimID, idx, step.Name, err)
				}
				ctxVars[step.SaveAs] = code
				continue
			}

			tok := ""
			if step.TokenVar != "" {
				tok = ctxVars[step.TokenVar]
			} else if step.AsAccount != "" && step.AsAccount != "none" && step.AsAccount != "unauthenticated" {
				if !step.ForceLogin {
					tok = cachedTokens[step.AsAccount]
				}
				if tok == "" {
					acc, ok := accountsByKey[step.AsAccount]
					if !ok {
						t.Fatalf("[%s] akun %q tidak ditemukan", sc.ClaimID, step.AsAccount)
					}
					tok = e2eLoginAccount(t, client, ts.URL, acc, seedPass, appCfg, totpOffsets)
					cachedTokens[step.AsAccount] = tok
				}
			}

			reqPath := expandString(step.Path)
			var reqBody io.Reader
			contentType := ""

			if step.MultipartPNG {
				var buf bytes.Buffer
				mw := multipart.NewWriter(&buf)
				ff := step.FileField
				if ff == "" {
					ff = "file"
				}
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="kyc-uji.png"`, ff))
				h.Set("Content-Type", "image/png")
				pw, err := mw.CreatePart(h)
				if err != nil {
					t.Fatalf("[%s] create multipart png part: %v", sc.ClaimID, err)
				}
				_, _ = pw.Write(tinyPNG)
				_ = mw.Close()
				reqBody = &buf
				contentType = mw.FormDataContentType()
			} else if step.MultipartCSV != "" {
				var buf bytes.Buffer
				mw := multipart.NewWriter(&buf)
				ff := step.FileField
				if ff == "" {
					ff = "file"
				}
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="mutasi-uji.csv"`, ff))
				h.Set("Content-Type", "text/csv")
				pw, err := mw.CreatePart(h)
				if err != nil {
					t.Fatalf("[%s] create multipart csv part: %v", sc.ClaimID, err)
				}
				_, _ = pw.Write([]byte(expandString(step.MultipartCSV)))
				_ = mw.Close()
				reqBody = &buf
				contentType = mw.FormDataContentType()
			} else if step.RawBody != nil {
				reqBody = strings.NewReader(expandString(*step.RawBody))
			} else if step.Body != nil {
				expanded := expandAny(step.Body)
				rawB, err := json.Marshal(expanded)
				if err != nil {
					t.Fatalf("[%s] marshal step body: %v", sc.ClaimID, err)
				}
				reqBody = bytes.NewReader(rawB)
				contentType = "application/json"
			}

			req, err := http.NewRequestWithContext(ctx, step.Method, ts.URL+reqPath, reqBody)
			if err != nil {
				t.Fatalf("[%s] build HTTP req: %v", sc.ClaimID, err)
			}
			for hk, hv := range step.Headers {
				req.Header.Set(hk, expandString(hv))
			}
			if contentType != "" && req.Header.Get("Content-Type") == "" {
				req.Header.Set("Content-Type", contentType)
			}
			if tok != "" && req.Header.Get("Authorization") == "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("[%s step %d %s] HTTP error: %v", sc.ClaimID, idx, step.Name, err)
			}
			respBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			respText := string(respBytes)
			checkBodyNoLeaks(sc.ClaimID+"/"+step.Name, respText)

			if len(step.ExpectStatus) > 0 {
				matched := false
				for _, want := range step.ExpectStatus {
					if resp.StatusCode == want {
						matched = true
						break
					}
				}
				if !matched {
					t.Fatalf("[%s step %d %s] %s %s expected status %v, got %d: %s",
						sc.ClaimID, idx, step.Name, step.Method, reqPath, step.ExpectStatus, resp.StatusCode, respText)
				}
			}

			for _, hdr := range step.ExpectHeaderPresent {
				if resp.Header.Get(hdr) == "" {
					t.Fatalf("[%s step %d %s] expected header %q present on %s %s",
						sc.ClaimID, idx, step.Name, hdr, step.Method, reqPath)
				}
			}

			for _, needle := range step.ExpectJSONContains {
				wantSub := expandString(needle)
				if !strings.Contains(respText, wantSub) {
					t.Fatalf("[%s step %d %s] expected response to contain %q, got %s",
						sc.ClaimID, idx, step.Name, wantSub, respText)
				}
			}

			if len(step.SaveJSON) > 0 {
				var parsed interface{}
				if err := json.Unmarshal(respBytes, &parsed); err != nil {
					t.Fatalf("[%s step %d %s] invalid JSON for save_json: %v body=%s", sc.ClaimID, idx, step.Name, err, respText)
				}
				for varName, jsonPath := range step.SaveJSON {
					val, ok := e2eExtractJSONPath(parsed, jsonPath)
					if !ok {
						t.Fatalf("[%s step %d %s] save_json path %q tidak ditemukan dalam %s", sc.ClaimID, idx, step.Name, jsonPath, respText)
					}
					ctxVars[varName] = val
				}
			}
		}
	}

	// Scan ulang seluruh respons yang terekam terhadap seluruh NIK dinamis yang baru dibuat di skenario
	for idx, body := range rawResponses {
		for _, leak := range forbiddenLeaks {
			if leak != "" && strings.Contains(body, leak) {
				t.Fatalf("FINAL PII SCAN FAIL pada respons #%d: ditemukan rahasia/NIK mentah %q", idx, leak)
			}
		}
	}

	t.Logf("V5 LIVE E2E PASSED: %d operasi x 8 subjek + %d cross-tenant = %d pemeriksaan matriks; %d skenario (%d langkah); CORS/413/Zero-PII tervalidasi (%d respons dipindai).",
		len(matrix.Endpoints), crossTenantCount, matrixCellCount+crossTenantCount, len(scFile.Scenarios), scenarioStepsCount, len(rawResponses))
}
