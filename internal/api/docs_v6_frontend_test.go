package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/middleware"
)

type errorCatalogFile struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
	Errors    map[string]struct {
		Code           string `json:"code"`
		HTTPStatus     int    `json:"http_status"`
		TitleID        string `json:"title_id"`
		MessageID      string `json:"message_id"`
		UserActionID   string `json:"user_action_id"`
		Retryable      bool   `json:"retryable"`
		RetryStrategy  string `json:"retry_strategy"`
		UIPattern      string `json:"ui_pattern"`
		RelatedClaimID string `json:"related_claim_id"`
	} `json:"errors"`
}

func TestDocV6_ErrorCatalogParity(t *testing.T) {
	catPath := resolveRepoPath(filepath.Join("docs", "frontend", "pesan-error-id.json"))
	rawCat, err := os.ReadFile(catPath)
	if err != nil {
		t.Fatalf("gagal baca %s: %v", catPath, err)
	}
	var cat errorCatalogFile
	if err := json.Unmarshal(rawCat, &cat); err != nil {
		t.Fatalf("pesan-error-id.json bukan JSON valid: %v", err)
	}
	if len(cat.Errors) == 0 {
		t.Fatalf("pesan-error-id.json tidak memiliki entri error")
	}

	// Kumpulkan seluruh error.code nyata dari kode Go non-test di internal/api
	apiRoot := resolveRepoPath(filepath.Join("internal", "api"))
	customErrRe := regexp.MustCompile(`response\.Error\([^,]+,\s*[^,]+,\s*"([A-Z0-9_]+)"`)
	rawCodeRe := regexp.MustCompile(`"code":\s*"([A-Z0-9_]+)"`)
	writeBlockedRe := regexp.MustCompile(`writeBlocked\(\s*w\s*,\s*"([A-Z0-9_]+)"`)
	helperMap := map[string]string{
		"response.BadRequest(":    "BAD_REQUEST",
		"response.Unauthorized(":  "UNAUTHORIZED",
		"response.Forbidden(":     "FORBIDDEN",
		"response.NotFound(":      "NOT_FOUND",
		"response.Conflict(":            "CONFLICT",
		"response.InternalServerError(": "INTERNAL_SERVER_ERROR",
	}

	backendCodes := make(map[string]bool)
	err = filepath.WalkDir(apiRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(b)
		for _, m := range customErrRe.FindAllStringSubmatch(text, -1) {
			backendCodes[m[1]] = true
		}
		for _, m := range rawCodeRe.FindAllStringSubmatch(text, -1) {
			backendCodes[m[1]] = true
		}
		for _, m := range writeBlockedRe.FindAllStringSubmatch(text, -1) {
			backendCodes[m[1]] = true
		}
		for callPrefix, code := range helperMap {
			if strings.Contains(text, callPrefix) {
				backendCodes[code] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("gagal memindai internal/api untuk error.code: %v", err)
	}

	for code := range backendCodes {
		if _, ok := cat.Errors[code]; !ok {
			t.Errorf("kode error backend %s belum terdokumentasi di docs/frontend/pesan-error-id.json", code)
		}
	}

	// Muat scenarios.json dan frontend-reference/src/errors.ts
	scBytes, err := os.ReadFile(resolveRepoPath(filepath.Join("frontend-tester", "scenarios.json")))
	if err != nil {
		t.Fatalf("gagal baca scenarios.json: %v", err)
	}
	var sf struct {
		Scenarios []struct {
			ClaimID string `json:"claim_id"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(scBytes, &sf); err != nil {
		t.Fatalf("scenarios.json tidak valid: %v", err)
	}
	knownClaims := make(map[string]bool)
	for _, s := range sf.Scenarios {
		knownClaims[s.ClaimID] = true
	}

	sdkErrBytes, err := os.ReadFile(resolveRepoPath(filepath.Join("frontend-reference", "src", "errors.ts")))
	if err != nil {
		t.Fatalf("gagal baca frontend-reference/src/errors.ts: %v", err)
	}
	sdkErrText := string(sdkErrBytes)

	for key, item := range cat.Errors {
		if key != item.Code {
			t.Errorf("kunci error %s tidak sama dengan field code=%s di pesan-error-id.json", key, item.Code)
		}
		if !backendCodes[key] {
			t.Errorf("kode error %s di pesan-error-id.json tidak ditemukan dipancarkan oleh backend internal/api", key)
		}
		if item.HTTPStatus < 400 || item.HTTPStatus >= 600 {
			t.Errorf("http_status tidak valid untuk %s: %d", key, item.HTTPStatus)
		}
		if item.TitleID == "" || item.MessageID == "" || item.UserActionID == "" || item.RetryStrategy == "" || item.UIPattern == "" {
			t.Errorf("metadata pesan error %s tidak boleh memiliki field kosong", key)
		}
		if !knownClaims[item.RelatedClaimID] {
			t.Errorf("related_claim_id %q pada error %s tidak ditemukan di scenarios.json", item.RelatedClaimID, key)
		}
		if !strings.Contains(sdkErrText, `"`+key+`"`) && !strings.Contains(sdkErrText, key+":") {
			t.Errorf("frontend-reference/src/errors.ts belum memetakan error.code %s", key)
		}
	}
}

func TestDocV6_ConfigSchemaAndExampleParity(t *testing.T) {
	schemaPath := resolveRepoPath(filepath.Join("docs", "frontend", "frontend.config.schema.json"))
	examplePath := resolveRepoPath(filepath.Join("docs", "frontend", "config.example.json"))

	rawSchema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("gagal baca schema: %v", err)
	}
	rawExample, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("gagal baca config.example.json: %v", err)
	}

	var schemaMap map[string]interface{}
	if err := json.Unmarshal(rawSchema, &schemaMap); err != nil {
		t.Fatalf("frontend.config.schema.json bukan JSON valid: %v", err)
	}
	var exampleMap map[string]interface{}
	if err := json.Unmarshal(rawExample, &exampleMap); err != nil {
		t.Fatalf("config.example.json bukan JSON valid: %v", err)
	}

	baseURL, _ := exampleMap["api_base_url"].(string)
	if !strings.HasPrefix(baseURL, "https://") || !strings.Contains(baseURL, ".example") {
		t.Errorf("api_base_url di config.example.json harus sintetis (.example), dapat: %q", baseURL)
	}

	validateObjectAgainstSchema(t, "config.example.json", exampleMap, schemaMap)
}

func validateObjectAgainstSchema(t *testing.T, label string, data map[string]interface{}, schema map[string]interface{}) {
	t.Helper()
	props, _ := schema["properties"].(map[string]interface{})
	if reqRaw, ok := schema["required"].([]interface{}); ok {
		for _, r := range reqRaw {
			reqKey, _ := r.(string)
			if _, exists := data[reqKey]; !exists {
				t.Errorf("%s kehilangan field wajib schema %q", label, reqKey)
			}
		}
	}
	if addProps, ok := schema["additionalProperties"].(bool); ok && !addProps {
		for k := range data {
			if _, defined := props[k]; !defined {
				t.Errorf("%s memiliki field %q yang tidak diizinkan oleh additionalProperties=false", label, k)
			}
		}
	}

	for k, val := range data {
		propSchema, ok := props[k].(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := propSchema["type"].(string)
		switch typ {
		case "string":
			s, ok := val.(string)
			if !ok {
				t.Errorf("%s.%s harus bertipe string, dapat %T", label, k, val)
				continue
			}
			if cVal, hasConst := propSchema["const"].(string); hasConst && s != cVal {
				t.Errorf("%s.%s harus %q, dapat %q", label, k, cVal, s)
			}
			if enumRaw, hasEnum := propSchema["enum"].([]interface{}); hasEnum && !inJSONEnum(s, enumRaw) {
				t.Errorf("%s.%s = %q berada di luar enum schema", label, k, s)
			}
			if minLen, hasMin := propSchema["minLength"].(float64); hasMin && float64(len(s)) < minLen {
				t.Errorf("%s.%s panjang %d < minLength %.0f", label, k, len(s), minLen)
			}
			if pat, hasPat := propSchema["pattern"].(string); hasPat {
				re, err := regexp.Compile(pat)
				if err == nil && !re.MatchString(s) {
					t.Errorf("%s.%s = %q tidak cocok pattern %q", label, k, s, pat)
				}
			}
		case "integer":
			num, ok := val.(float64)
			if !ok {
				t.Errorf("%s.%s harus bertipe integer, dapat %T", label, k, val)
				continue
			}
			if minVal, hasMin := propSchema["minimum"].(float64); hasMin && num < minVal {
				t.Errorf("%s.%s = %.0f < minimum %.0f", label, k, num, minVal)
			}
			if maxVal, hasMax := propSchema["maximum"].(float64); hasMax && num > maxVal {
				t.Errorf("%s.%s = %.0f > maximum %.0f", label, k, num, maxVal)
			}
		case "boolean":
			if _, ok := val.(bool); !ok {
				t.Errorf("%s.%s harus bertipe boolean, dapat %T", label, k, val)
			}
		case "object":
			sub, ok := val.(map[string]interface{})
			if !ok {
				t.Errorf("%s.%s harus bertipe object, dapat %T", label, k, val)
				continue
			}
			validateObjectAgainstSchema(t, label+"."+k, sub, propSchema)
		case "array":
			arr, ok := val.([]interface{})
			if !ok {
				t.Errorf("%s.%s harus bertipe array, dapat %T", label, k, val)
				continue
			}
			if minItems, hasMin := propSchema["minItems"].(float64); hasMin && float64(len(arr)) < minItems {
				t.Errorf("%s.%s panjang array %d < minItems %.0f", label, k, len(arr), minItems)
			}
			if itemSchema, hasItem := propSchema["items"].(map[string]interface{}); hasItem {
				if enumRaw, hasEnum := itemSchema["enum"].([]interface{}); hasEnum {
					for i, item := range arr {
						s, _ := item.(string)
						if !inJSONEnum(s, enumRaw) {
							t.Errorf("%s.%s[%d] = %q di luar enum schema", label, k, i, s)
						}
					}
				}
			}
		}
	}
}

func inJSONEnum(val string, enumVals []interface{}) bool {
	for _, ev := range enumVals {
		if s, ok := ev.(string); ok && s == val {
			return true
		}
	}
	return false
}

func TestDocV6_FrontendChaptersStructureAndNoHardcodedSecretsOrPorts(t *testing.T) {
	frontendDocsDir := resolveRepoPath(filepath.Join("docs", "frontend"))
	entries, err := os.ReadDir(frontendDocsDir)
	if err != nil {
		t.Fatalf("gagal baca docs/frontend: %v", err)
	}

	mdCount := 0
	linkRe := regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	fenceRe := regexp.MustCompile("(?s)```.*?```")
	inlineCodeRe := regexp.MustCompile("`[^`]+`")

	anchorCache := map[string]map[string]bool{}
	loadAnchors := func(absPath string) (map[string]bool, error) {
		if m, ok := anchorCache[absPath]; ok {
			return m, nil
		}
		raw, err := os.ReadFile(absPath)
		if err != nil {
			return nil, err
		}
		m := collectMarkdownAnchors(string(raw))
		anchorCache[absPath] = m
		return m, nil
	}

	featureChapters := map[string]bool{
		"02-autentikasi-mfa-sesi.md":      true,
		"04-pendaftaran-kyc.md":           true,
		"05-profil-anggota-pengguna.md":   true,
		"06-iuran-simpanan.md":            true,
		"07-katalog-promo-keranjang.md":   true,
		"08-toko-checkout-pesanan.md":     true,
		"09-pembayaran-qris-transfer.md":  true,
		"10-rekonsiliasi-mutasi-kas.md":   true,
		"11-laporan-dan-pengawas.md":      true,
	}

	legalMustAppear := map[string]bool{
		"04-pendaftaran-kyc.md":                true,
		"14-unggahan-berkas-keamanan-klien.md": true,
		"99-glosarium-koperasi.md":             true,
	}
	akuntanMustAppear := map[string]bool{
		"06-iuran-simpanan.md":     true,
		"11-laporan-dan-pengawas.md": true,
		"99-glosarium-koperasi.md": true,
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		mdCount++
		filePath := filepath.Join(frontendDocsDir, entry.Name())
		rawBytes, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("gagal baca %s: %v", filePath, err)
		}
		text := string(rawBytes)

		if featureChapters[entry.Name()] {
			for sec := 1; sec <= 13; sec++ {
				prefix := fmt.Sprintf("\n## %d.", sec)
				if !strings.Contains("\n"+text, prefix) {
					t.Errorf("bab fitur %s belum memiliki sub-bagian templat wajib %q", entry.Name(), strings.TrimSpace(prefix))
				}
			}
		}

		if legalMustAppear[entry.Name()] && !strings.Contains(text, "PERLU VERIFIKASI HUKUM") {
			t.Errorf("%s wajib mencantumkan label PERLU VERIFIKASI HUKUM", entry.Name())
		}
		if akuntanMustAppear[entry.Name()] && !strings.Contains(text, "KEPUTUSAN AKUNTAN") {
			t.Errorf("%s wajib mencantumkan label KEPUTUSAN AKUNTAN", entry.Name())
		}

		// Cek tautan relatif & anchor di luar code block/inline code
		cleaned := fenceRe.ReplaceAllString(text, "")
		cleaned = inlineCodeRe.ReplaceAllString(cleaned, "")
		for _, match := range linkRe.FindAllStringSubmatch(cleaned, -1) {
			target := strings.TrimSpace(match[1])
			if target == "" ||
				strings.HasPrefix(target, "http://") ||
				strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") ||
				strings.HasPrefix(target, "file://") {
				continue
			}
			parts := strings.SplitN(target, "#", 2)
			relFile := parts[0]
			anchor := ""
			if len(parts) == 2 {
				anchor = parts[1]
			}
			targetAbs := filePath
			if relFile != "" {
				targetAbs = filepath.Clean(filepath.Join(frontendDocsDir, filepath.FromSlash(relFile)))
				if _, err := os.Stat(targetAbs); err != nil {
					t.Errorf("docs/frontend/%s memiliki tautan rusak ke %q (%v)", entry.Name(), target, err)
					continue
				}
			}
			if anchor != "" && strings.HasSuffix(targetAbs, ".md") {
				anchors, err := loadAnchors(targetAbs)
				if err != nil {
					t.Errorf("gagal memuat anchor %s: %v", targetAbs, err)
					continue
				}
				if !anchors[anchor] {
					t.Errorf("docs/frontend/%s memiliki anchor rusak #%s pada target %s", entry.Name(), anchor, target)
				}
			}
		}
	}
	if mdCount < 22 {
		t.Errorf("jumlah dokumen Markdown di docs/frontend/ terlalu sedikit (%d, minimal 22)", mdCount)
	}

	// Scan lintas aset frontend (docs/frontend, frontend-reference/src+test, frontend-tester) untuk hardcode port & secret
	forbiddenLiterals := []string{
		"127.0.0.1:8080",
		"127.0.0.1:18080",
		"localhost:8080",
		"localhost:18080",
		"127.0.0.1:8081",
		"localhost:8081",
		"127.0.0.1:5433",
		"127.0.0.1:15433",
		"kopdes_test_setA",
	}
	scanRoots := []string{
		resolveRepoPath(filepath.Join("docs", "frontend")),
		resolveRepoPath(filepath.Join("frontend-reference", "src")),
		resolveRepoPath(filepath.Join("frontend-reference", "test")),
		resolveRepoPath("frontend-tester"),
	}
	for _, rootDir := range scanRoots {
		err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			switch ext {
			case ".md", ".json", ".ts", ".tsx", ".js", ".mjs", ".html":
			default:
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			txt := string(b)
			lowerTxt := strings.ToLower(txt)
			for _, lit := range forbiddenLiterals {
				if strings.Contains(lowerTxt, strings.ToLower(lit)) {
					t.Errorf("%s mengandung literal port/URL/secret terlarang %q", path, lit)
				}
			}
			if rootDir == resolveRepoPath(filepath.Join("docs", "frontend")) && ext == ".md" {
				if strings.Contains(lowerTxt, "enterprise") {
					t.Errorf("%s mengandung kata terlarang 'enterprise'", path)
				}
				if strings.Contains(lowerTxt, "memenuhi uu pdp") || strings.Contains(lowerTxt, "memenuhi standar uu pdp") {
					t.Errorf("%s mengandung klaim terlarang 'memenuhi UU PDP'", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("gagal memindai %s: %v", rootDir, err)
		}
	}
}

type docResponseExample struct {
	File     string
	ID       string
	Method   string
	Path     string
	Auth     string
	BodySpec string
	Status   int
	DocJSON  interface{}
}

func TestDocV6_ExecutedResponseExamplesMatchLiveAPI(t *testing.T) {
	frontendDocsDir := resolveRepoPath(filepath.Join("docs", "frontend"))
	mdFiles, err := filepath.Glob(filepath.Join(frontendDocsDir, "*.md"))
	if err != nil {
		t.Fatalf("gagal glob docs/frontend/*.md: %v", err)
	}

	markerRe := regexp.MustCompile(`<!--\s*doc-test-response:\s*([^>]+?)\s*-->\s*\r?\n\x60\x60\x60json\r?\n([\s\S]*?)\r?\n\x60\x60\x60`)
	var examples []docResponseExample

	for _, mdFile := range mdFiles {
		raw, err := os.ReadFile(mdFile)
		if err != nil {
			t.Fatalf("gagal baca %s: %v", mdFile, err)
		}
		matches := markerRe.FindAllStringSubmatch(string(raw), -1)
		for _, m := range matches {
			meta := parseMarkerKV(m[1])
			statusCode, convErr := strconv.Atoi(meta["status"])
			if convErr != nil {
				t.Fatalf("status tidak valid pada %s (%s): %v", mdFile, m[1], convErr)
			}
			var decoded interface{}
			if err := json.Unmarshal([]byte(m[2]), &decoded); err != nil {
				t.Fatalf("contoh JSON tidak valid pada %s id=%s: %v", mdFile, meta["id"], err)
			}
			examples = append(examples, docResponseExample{
				File:     filepath.Base(mdFile),
				ID:       meta["id"],
				Method:   strings.ToUpper(meta["method"]),
				Path:     meta["path"],
				Auth:     meta["auth"],
				BodySpec: meta["body"],
				Status:   statusCode,
				DocJSON:  decoded,
			})
		}
	}

	if len(examples) < 10 {
		t.Fatalf("hanya menemukan %d marker doc-test-response di docs/frontend/*.md (minimal 10)", len(examples))
	}

	env := setupAPITestEnv(t)
	ts := httptest.NewServer(env.Server.Router())
	defer ts.Close()

	_, anggotaEmail, anggotaPass, _ := env.createAndLoginUserFull(t, "anggota")
	// Login ulang agar login anggota_a segar untuk Bearer token
	anggotaToken, _ := env.createAndLoginUser(t, "anggota")
	newUserToken, _ := env.createAndLoginUser(t, "anggota")

	// Ambil wilayah_desa_id milik Koperasi test menggunakan middleware.WithTenantTx
	var wilayahDesaID uuid.UUID
	ctx := context.Background()
	err = middleware.WithTenantTx(ctx, env.Pool, env.KoperasiID, false, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT wilayah_desa_id FROM koperasi WHERE id = $1", env.KoperasiID).Scan(&wilayahDesaID)
	})
	if err != nil || wilayahDesaID == uuid.Nil {
		t.Fatalf("gagal mengambil wilayah_desa_id koperasi test: %v", err)
	}

	// Seed 1 produk katalog lewat HTTP admin agar GET /api/v1/katalog/produk tidak kosong
	prodBody, _ := json.Marshal(handlers.CreateProdukRequest{
		Nama:         "Beras Doc Test 5kg",
		SKU:          "DOC-" + uuid.New().String()[:8],
		Deskripsi:    "Produk uji verifikasi dokumentasi",
		SatuanDasar:  "sak",
		HargaUmum:    72000,
		HargaAnggota: 68000,
		StokAwal:     25,
	})
	prodReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/katalog/produk", bytes.NewReader(prodBody))
	prodReq.Header.Set("Authorization", "Bearer "+env.AdminToken)
	prodReq.Header.Set("Content-Type", "application/json")
	prodResp, err := ts.Client().Do(prodReq)
	if err != nil {
		t.Fatalf("gagal seed produk katalog: %v", err)
	}
	_ = prodResp.Body.Close()
	if prodResp.StatusCode != http.StatusCreated {
		t.Fatalf("seed produk katalog gagal status=%d", prodResp.StatusCode)
	}

	// Muat schema konfigurasi untuk validasi ekstra /public/config
	rawSchema, err := os.ReadFile(resolveRepoPath(filepath.Join("docs", "frontend", "frontend.config.schema.json")))
	if err != nil {
		t.Fatalf("gagal baca schema: %v", err)
	}
	var configSchema map[string]interface{}
	if err := json.Unmarshal(rawSchema, &configSchema); err != nil {
		t.Fatalf("schema JSON invalid: %v", err)
	}

	for _, ex := range examples {
		ex := ex
		t.Run(ex.ID, func(t *testing.T) {
			var reqBody []byte
			if ex.BodySpec != "" {
				reqBody = []byte(ex.BodySpec)
			}

			switch ex.Auth {
			case "login_anggota_a":
				reqBody, _ = json.Marshal(handlers.LoginRequest{
					Email:    anggotaEmail,
					Password: anggotaPass,
				})
			case "anggota_new":
				if ex.Path == "/api/v1/pendaftaran" && ex.Method == "POST" {
					reqBody, _ = json.Marshal(handlers.AjukanPendaftaranRequest{
						NIK:           fmt.Sprintf("3201%012d", time.Now().UnixNano()%1000000000000),
						NamaLengkap:   "Siti Aminah DocTest",
						TempatLahir:   "Bogor",
						TanggalLahir:  "1990-01-01",
						JenisKelamin:  "P",
						Pekerjaan:     "Petani",
						AlamatKTP:     "Jl. Desa Makmur No. 10",
						WilayahDesaID: wilayahDesaID,
					})
				}
			}

			var bodyReader *bytes.Reader
			if len(reqBody) > 0 {
				bodyReader = bytes.NewReader(reqBody)
			} else {
				bodyReader = bytes.NewReader(nil)
			}
			req, err := http.NewRequest(ex.Method, ts.URL+ex.Path, bodyReader)
			if err != nil {
				t.Fatalf("gagal buat HTTP request %s %s: %v", ex.Method, ex.Path, err)
			}
			if len(reqBody) > 0 {
				req.Header.Set("Content-Type", "application/json")
			}

			switch ex.Auth {
			case "public", "login_anggota_a":
			case "anggota_a":
				req.Header.Set("Authorization", "Bearer "+anggotaToken)
			case "admin_a":
				req.Header.Set("Authorization", "Bearer "+env.AdminToken)
			case "anggota_new":
				req.Header.Set("Authorization", "Bearer "+newUserToken)
			default:
				t.Fatalf("auth spec tidak dikenal pada %s (%s): %q", ex.ID, ex.File, ex.Auth)
			}

			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("eksekusi %s %s gagal: %v", ex.Method, ex.Path, err)
			}
			defer resp.Body.Close()

			var liveJSON interface{}
			if err := json.NewDecoder(resp.Body).Decode(&liveJSON); err != nil {
				t.Fatalf("respons %s %s bukan JSON valid: %v", ex.Method, ex.Path, err)
			}

			if resp.StatusCode != ex.Status {
				t.Fatalf("%s (%s): status HTTP mismatch! doc=%d live=%d body=%v",
					ex.ID, ex.File, ex.Status, resp.StatusCode, liveJSON)
			}

			strictKeys := ex.Path == "/healthz" ||
				ex.Path == "/api/v1/public/config" ||
				ex.Path == "/api/v1/auth/me" ||
				ex.Path == "/api/v1/auth/login" ||
				ex.Path == "/api/v1/pendaftaran" ||
				ex.Path == "/api/v1/laporan/keuangan"

			compareDocAndLiveJSON(t, ex.ID+"("+ex.File+")", ex.DocJSON, liveJSON, strictKeys)

			if ex.Path == "/api/v1/public/config" {
				if rootObj, ok := liveJSON.(map[string]interface{}); ok {
					if dataObj, ok := rootObj["data"].(map[string]interface{}); ok {
						validateObjectAgainstSchema(t, "live:/api/v1/public/config", dataObj, configSchema)
					}
				}
			}
		})
	}
}

func parseMarkerKV(raw string) map[string]string {
	out := make(map[string]string)
	for _, tok := range strings.Fields(raw) {
		kv := strings.SplitN(tok, "=", 2)
		if len(kv) == 2 {
			out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return out
}

func compareDocAndLiveJSON(t *testing.T, path string, docVal, liveVal interface{}, strictObjectKeys bool) {
	t.Helper()
	switch d := docVal.(type) {
	case map[string]interface{}:
		l, ok := liveVal.(map[string]interface{})
		if !ok {
			t.Errorf("%s: doc bertipe object, live bertipe %T", path, liveVal)
			return
		}
		for k, dv := range d {
			lv, exists := l[k]
			if !exists {
				t.Errorf("%s.%s: ada di contoh dokumentasi tetapi HILANG dari respons API live", path, k)
				continue
			}
			// Khusus error.code & success boolean harus persis nilainya
			if k == "success" {
				if dv != lv {
					t.Errorf("%s.success mismatch: doc=%v live=%v", path, dv, lv)
				}
				continue
			}
			if k == "code" && strings.HasSuffix(path, ".error") {
				if dv != lv {
					t.Errorf("%s.code mismatch: doc=%v live=%v", path, dv, lv)
				}
				continue
			}
			// Jangan paksa strict exact key match pada map dinamis seperti rincian_status
			subStrict := strictObjectKeys && k != "rincian_status"
			compareDocAndLiveJSON(t, path+"."+k, dv, lv, subStrict)
		}
		if strictObjectKeys {
			for k := range l {
				if _, exists := d[k]; !exists {
					t.Errorf("%s.%s: dikembalikan oleh API live tetapi BELUM didokumentasikan di contoh JSON", path, k)
				}
			}
		}
	case []interface{}:
		l, ok := liveVal.([]interface{})
		if !ok {
			t.Errorf("%s: doc bertipe array, live bertipe %T", path, liveVal)
			return
		}
		if len(d) > 0 && len(l) > 0 {
			compareDocAndLiveJSON(t, path+"[0]", d[0], l[0], false)
		}
	case string:
		if _, ok := liveVal.(string); !ok {
			t.Errorf("%s: doc bertipe string, live bertipe %T", path, liveVal)
		}
	case float64:
		if _, ok := liveVal.(float64); !ok {
			t.Errorf("%s: doc bertipe number, live bertipe %T", path, liveVal)
		}
	case bool:
		if _, ok := liveVal.(bool); !ok {
			t.Errorf("%s: doc bertipe bool, live bertipe %T", path, liveVal)
		}
	}
}

func TestDocV6_TypeScriptSDKTypecheckAndLiveTests(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm tidak ditemukan di PATH; melewati verifikasi SDK TypeScript")
	}
	sdkDir := resolveRepoPath("frontend-reference")
	if _, err := os.Stat(filepath.Join(sdkDir, "node_modules")); err != nil {
		t.Fatalf("frontend-reference/node_modules belum terpasang (jalankan npm --prefix frontend-reference ci): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmdType := exec.CommandContext(ctx, "npm", "--prefix", sdkDir, "run", "typecheck")
	if out, err := cmdType.CombinedOutput(); err != nil {
		t.Fatalf("TypeScript SDK typecheck gagal: %v\n%s", err, string(out))
	}

	env := setupAPITestEnv(t)
	ts := httptest.NewServer(env.Server.Router())
	defer ts.Close()

	cmdTest := exec.CommandContext(ctx, "npm", "--prefix", sdkDir, "test")
	cmdTest.Env = append(os.Environ(), "API_PUBLIC_URL="+ts.URL)
	out, err := cmdTest.CombinedOutput()
	if err != nil {
		t.Fatalf("frontend-reference SDK test gagal terhadap live server: %v\n%s", err, string(out))
	}
	outText := string(out)
	if !strings.Contains(outText, "fail 0") || !strings.Contains(outText, "skipped 0") {
		t.Fatalf("SDK live test tidak boleh memiliki kegagalan atau tes live yang ter-skip:\n%s", outText)
	}
}
