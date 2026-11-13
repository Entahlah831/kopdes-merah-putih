package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/security"
)

func resolveRepoPath(rel string) string {
	candidates := []string{
		filepath.Join("..", "..", rel),
		rel,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return candidates[0]
}

// TestDoc_OpenAPI_RouteParity memastikan seluruh rute di internal/api/server.go
// terdokumentasi 100% di docs/openapi.yaml dan docs/kopdes.postman_collection.json.
func TestDoc_OpenAPI_RouteParity(t *testing.T) {
	serverBytes, err := os.ReadFile(resolveRepoPath("internal/api/server.go"))
	if err != nil {
		t.Fatalf("Gagal membaca internal/api/server.go: %v", err)
	}
	openAPIBytes, err := os.ReadFile(resolveRepoPath("docs/openapi.yaml"))
	if err != nil {
		t.Fatalf("Gagal membaca docs/openapi.yaml: %v", err)
	}
	postmanBytes, err := os.ReadFile(resolveRepoPath("docs/kopdes.postman_collection.json"))
	if err != nil {
		t.Fatalf("Gagal membaca docs/kopdes.postman_collection.json: %v", err)
	}
	tsClientBytes, err := os.ReadFile(resolveRepoPath("docs/kopdesClient.ts"))
	if err != nil {
		t.Fatalf("Gagal membaca docs/kopdesClient.ts: %v", err)
	}

	openAPIStr := string(openAPIBytes)
	if !strings.Contains(openAPIStr, "openapi: 3.") {
		t.Fatalf("docs/openapi.yaml harus berformat OpenAPI 3.x")
	}
	if strings.Contains(openAPIStr, "localhost:8080") {
		t.Fatalf("docs/openapi.yaml dilarang hardcode localhost:8080")
	}
	if !strings.Contains(openAPIStr, "http://{host}:{port}") {
		t.Fatalf("docs/openapi.yaml harus memakai URL server berparameter http://{host}:{port}")
	}

	// Ekstrak seluruh "METHOD /path" dari server.go
	routeRe := regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE)\s+(/[^"]+)"`)
	serverRoutes := map[string]bool{}
	for _, m := range routeRe.FindAllStringSubmatch(string(serverBytes), -1) {
		key := m[1] + " " + m[2]
		serverRoutes[key] = true
	}
	if len(serverRoutes) == 0 {
		t.Fatalf("Tidak ada rute HTTP yang terekstrak dari internal/api/server.go")
	}

	// Ekstrak paths dan method dari docs/openapi.yaml
	openAPIRoutes := map[string]bool{}
	lines := strings.Split(openAPIStr, "\n")
	inPaths := false
	currentPath := ""
	pathLineRe := regexp.MustCompile(`^  (/[^:]+):\s*$`)
	methodLineRe := regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	for _, line := range lines {
		trimmedRight := strings.TrimRight(line, "\r")
		if trimmedRight == "paths:" {
			inPaths = true
			currentPath = ""
			continue
		}
		if inPaths && len(trimmedRight) > 0 && trimmedRight[0] != ' ' && trimmedRight[0] != '#' {
			inPaths = false
			currentPath = ""
			continue
		}
		if !inPaths {
			continue
		}
		if pm := pathLineRe.FindStringSubmatch(trimmedRight); pm != nil {
			currentPath = pm[1]
			continue
		}
		if mm := methodLineRe.FindStringSubmatch(trimmedRight); mm != nil && currentPath != "" {
			openAPIRoutes[strings.ToUpper(mm[1])+" "+currentPath] = true
		}
	}

	for r := range serverRoutes {
		if !openAPIRoutes[r] {
			t.Errorf("Rute di server.go belum terdokumentasi di docs/openapi.yaml: %s", r)
		}
	}
	for r := range openAPIRoutes {
		if !serverRoutes[r] {
			t.Errorf("Rute di docs/openapi.yaml tidak terdaftar di internal/api/server.go: %s", r)
		}
	}

	// Validasi struktur Postman Collection JSON
	var pmDoc struct {
		Variable []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"variable"`
		Item []json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(postmanBytes, &pmDoc); err != nil {
		t.Fatalf("docs/kopdes.postman_collection.json bukan JSON valid: %v", err)
	}
	if len(pmDoc.Item) == 0 {
		t.Fatalf("Postman collection kosong")
	}
	postmanStr := string(postmanBytes)
	if strings.Contains(postmanStr, "localhost:8080") {
		t.Fatalf("Postman collection masih memuat hardcode localhost:8080")
	}
	if !strings.Contains(string(tsClientBytes), "createKopdesEndpoints") {
		t.Fatalf("docs/kopdesClient.ts tidak memuat createKopdesEndpoints")
	}
}

func githubHeadingSlug(raw string) string {
	// Bersihkan penanda inline code & tautan sederhana lalu konversi sesuai aturan GitHub slug
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "`", "")
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func collectMarkdownAnchors(content string) map[string]bool {
	anchors := map[string]bool{}
	seenCount := map[string]int{}
	inCode := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		headingText := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if headingText == "" {
			continue
		}
		base := githubHeadingSlug(headingText)
		if count := seenCount[base]; count > 0 {
			anchors[fmt.Sprintf("%s-%d", base, count)] = true
		} else {
			anchors[base] = true
		}
		seenCount[base]++
	}
	return anchors
}

// TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims memeriksa seluruh tautan relatif,
// target #anchor heading, ketiadaan localhost:8080, serta larangan klaim mutlak di docs/*.md.
func TestDoc_MarkdownLinks_Anchors_And_NoForbiddenClaims(t *testing.T) {
	docsDir := resolveRepoPath("docs")
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Fatalf("Gagal membaca direktori docs/: %v", err)
	}

	linkRe := regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	fenceRe := regexp.MustCompile("(?s)```.*?```")
	inlineCodeRe := regexp.MustCompile("`[^`]+`")

	// Berkas dokumentasi aktif yang dilarang keras mengandung kata "enterprise" / "memenuhi UU PDP"
	strictClaimFiles := map[string]bool{
		"README.md":                           true,
		"BACKEND_GUIDE.md":                    true,
		"FRONTEND_GUIDE.md":                   true,
		"backend_integration_guide.md":        true,
		"AUDIT_REPORT.md":                     true,
		"ENV_REFERENCE.md":                    true,
		"HASH_CHAIN_ADVISORY_LOCKS.md":        true,
		"LAPORAN_REMEDIASI_DAN_PERUBAHAN_KODE.md": true,
		"QRIS_UJI_LAPANGAN.md":                true,
		"SCOPE_RECONCILIATION.md":             true,
		"TUGAS_MANUAL_PEMILIK.md":             true,
		"erd.md":                              true,
	}

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

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		filePath := filepath.Join(docsDir, entry.Name())
		rawBytes, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Gagal membaca %s: %v", filePath, err)
		}
		rawText := string(rawBytes)
		lowerText := strings.ToLower(rawText)

		if strings.Contains(lowerText, "localhost:8080") {
			t.Errorf("%s masih mengandung hardcode localhost:8080", entry.Name())
		}
		if strictClaimFiles[entry.Name()] {
			if strings.Contains(lowerText, "enterprise") {
				t.Errorf("%s mengandung kata terlarang 'enterprise'", entry.Name())
			}
			if strings.Contains(lowerText, "memenuhi uu pdp") || strings.Contains(lowerText, "memenuhi standar uu pdp") {
				t.Errorf("%s mengandung klaim terlarang 'memenuhi UU PDP'", entry.Name())
			}
		}

		// Validasi tautan Markdown relatif & #anchor (di luar code block)
		cleaned := fenceRe.ReplaceAllString(rawText, "")
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
				targetAbs = filepath.Clean(filepath.Join(docsDir, filepath.FromSlash(relFile)))
				if _, err := os.Stat(targetAbs); err != nil {
					t.Errorf("%s memiliki tautan rusak ke %q (%v)", entry.Name(), target, err)
					continue
				}
			}

			if anchor != "" && strings.HasSuffix(targetAbs, ".md") {
				anchors, err := loadAnchors(targetAbs)
				if err != nil {
					t.Errorf("Gagal memuat anchor dari %s: %v", targetAbs, err)
					continue
				}
				if !anchors[anchor] {
					t.Errorf("%s memiliki anchor rusak #%s pada target %s", entry.Name(), anchor, target)
				}
			}
		}
	}

	// Pastikan frasa kunci pengakuan batasan & keputusan manusia ada di panduan utama
	backendDoc, err := os.ReadFile(filepath.Join(docsDir, "BACKEND_GUIDE.md"))
	if err != nil {
		t.Fatalf("Gagal membaca BACKEND_GUIDE.md: %v", err)
	}
	if !strings.Contains(string(backendDoc), "PERLU VERIFIKASI HUKUM") {
		t.Errorf("BACKEND_GUIDE.md wajib mencantumkan catatan PERLU VERIFIKASI HUKUM")
	}
	if !strings.Contains(string(backendDoc), "KEPUTUSAN AKUNTAN") {
		t.Errorf("BACKEND_GUIDE.md wajib mencantumkan catatan KEPUTUSAN AKUNTAN")
	}
}

// TestDoc_EndToEnd_DocumentedWorkflows mengeksekusi langsung alur endpoint
// yang terdokumentasi di FRONTEND_GUIDE.md dan BACKEND_GUIDE.md ke handler live.
func TestDoc_EndToEnd_DocumentedWorkflows(t *testing.T) {
	env := setupAPITestEnv(t)
	cfg := env.Config
	cfg.StorageKYCDir = t.TempDir()
	srv := api.NewServer(env.Pool, cfg)
	router := srv.Router()
	metricsRouter := srv.MetricsRouter()
	ctx := context.Background()

	// 1. Healthz, Readyz & Metrics (tanpa/dengan token)
	recHealth := httptest.NewRecorder()
	router.ServeHTTP(recHealth, httptest.NewRequest("GET", "/healthz", nil))
	if recHealth.Code != http.StatusOK || !strings.Contains(recHealth.Body.String(), `"UP"`) {
		t.Fatalf("/healthz mismatch: %d %s", recHealth.Code, recHealth.Body.String())
	}

	recReady := httptest.NewRecorder()
	router.ServeHTTP(recReady, httptest.NewRequest("GET", "/readyz", nil))
	if recReady.Code != http.StatusOK || !strings.Contains(recReady.Body.String(), `"READY"`) {
		t.Fatalf("/readyz mismatch: %d %s", recReady.Code, recReady.Body.String())
	}

	recMetricsNoAuth := httptest.NewRecorder()
	metricsRouter.ServeHTTP(recMetricsNoAuth, httptest.NewRequest("GET", "/metrics", nil))
	if recMetricsNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("/metrics tanpa token harus 401, got: %d", recMetricsNoAuth.Code)
	}

	reqMetricsAuth := httptest.NewRequest("GET", "/metrics", nil)
	reqMetricsAuth.Header.Set("Authorization", "Bearer "+cfg.MetricsToken)
	recMetricsAuth := httptest.NewRecorder()
	metricsRouter.ServeHTTP(recMetricsAuth, reqMetricsAuth)
	if recMetricsAuth.Code != http.StatusOK {
		t.Fatalf("/metrics dengan Bearer token harus 200, got: %d %s", recMetricsAuth.Code, recMetricsAuth.Body.String())
	}

	// 2. Alur Auth + MFA Wajib Admin/Pengurus (MFA_MANDATORY_SETUP_REQUIRED -> setup -> enable -> mfa_required -> verify)
	secUserID := uuid.New()
	secEmail := fmt.Sprintf("ketua_doc_%s@kopdes.id", uuid.New().String()[:8])
	secPwd := "KetuaDocPass123!"
	secPwdHash, _ := security.HashPassword(secPwd)
	secEmailHash := security.HashBlindIndex(secEmail, []byte(cfg.BlindIndexSecret))

	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	_, _ = conn.Exec(ctx, "RESET ROLE;")
	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'Ketua Verifier Doc');
	`, secUserID, []byte("enc"), secEmailHash, secPwdHash)
	if err != nil {
		conn.Release()
		t.Fatalf("seed second admin: %v", err)
	}
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())
	var adminRoleID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'admin'").Scan(&adminRoleID)
	_, err = conn.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, koperasi_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (koperasi_id, user_id, role_id) DO NOTHING;
	`, secUserID, adminRoleID, env.KoperasiID)
	conn.Release()
	if err != nil {
		t.Fatalf("seed second admin role: %v", err)
	}

	loginPayload, _ := json.Marshal(handlers.LoginRequest{Email: secEmail, Password: secPwd})
	recEnroll := httptest.NewRecorder()
	reqEnroll := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginPayload))
	reqEnroll.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recEnroll, reqEnroll)
	if recEnroll.Code != http.StatusForbidden || !strings.Contains(recEnroll.Body.String(), "MFA_MANDATORY_SETUP_REQUIRED") {
		t.Fatalf("Login admin tanpa MFA harus 403 MFA_MANDATORY_SETUP_REQUIRED, got: %d %s", recEnroll.Code, recEnroll.Body.String())
	}

	var enrollErrResp struct {
		Data struct {
			MFARequired      bool   `json:"mfa_required"`
			MFASetupRequired bool   `json:"mfa_setup_required"`
			SetupToken       string `json:"setup_token"`
		} `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recEnroll.Body.Bytes(), &enrollErrResp); err != nil || !enrollErrResp.Data.MFASetupRequired || enrollErrResp.Data.SetupToken == "" {
		t.Fatalf("Respons 403 enrollment tidak memuat data.setup_token: %s", recEnroll.Body.String())
	}

	// Setup TOTP
	reqSetup := httptest.NewRequest("POST", "/api/v1/auth/mfa/setup", nil)
	reqSetup.Header.Set("Authorization", "Bearer "+enrollErrResp.Data.SetupToken)
	recSetup := httptest.NewRecorder()
	router.ServeHTTP(recSetup, reqSetup)
	if recSetup.Code != http.StatusOK {
		t.Fatalf("/auth/mfa/setup harus 200, got: %d %s", recSetup.Code, recSetup.Body.String())
	}
	var setupResp struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recSetup.Body.Bytes(), &setupResp)

	// Enable TOTP + terima recovery_codes
	totpCode1, err := security.GenerateTOTPCode(setupResp.Data.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode error: %v", err)
	}
	enableBody, _ := json.Marshal(map[string]string{
		"secret":    setupResp.Data.Secret,
		"totp_code": totpCode1,
	})
	reqEnable := httptest.NewRequest("POST", "/api/v1/auth/mfa/enable", bytes.NewReader(enableBody))
	reqEnable.Header.Set("Authorization", "Bearer "+enrollErrResp.Data.SetupToken)
	reqEnable.Header.Set("Content-Type", "application/json")
	recEnable := httptest.NewRecorder()
	router.ServeHTTP(recEnable, reqEnable)
	if recEnable.Code != http.StatusOK || !strings.Contains(recEnable.Body.String(), "recovery_codes") {
		t.Fatalf("/auth/mfa/enable harus 200 & memuat recovery_codes, got: %d %s", recEnable.Code, recEnable.Body.String())
	}

	// Login ulang -> mfa_required=true -> mfa/verify -> dapat access_token kedua untuk uji verifikasi
	recLogin2 := httptest.NewRecorder()
	reqLogin2 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginPayload))
	reqLogin2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recLogin2, reqLogin2)
	if recLogin2.Code != http.StatusOK {
		t.Fatalf("Login MFA tahap 1 harus 200, got: %d %s", recLogin2.Code, recLogin2.Body.String())
	}
	var login2Resp struct {
		Data struct {
			MFARequired     bool   `json:"mfa_required"`
			MFASessionToken string `json:"mfa_session_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recLogin2.Body.Bytes(), &login2Resp)
	if !login2Resp.Data.MFARequired || login2Resp.Data.MFASessionToken == "" {
		t.Fatalf("Harus mfa_required=true & mfa_session_token ada, got: %s", recLogin2.Body.String())
	}

	// Catatan: gunakan slot waktu berikutnya agar tidak tertolak anti-replay pada slot yang sama
	totpCode2, _ := security.GenerateTOTPCode(setupResp.Data.Secret, time.Now().Add(31*time.Second))
	verifyMFABody, _ := json.Marshal(map[string]string{
		"mfa_session_token": login2Resp.Data.MFASessionToken,
		"totp_code":         totpCode2,
	})
	reqVerifyMFA := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(verifyMFABody))
	reqVerifyMFA.Header.Set("Content-Type", "application/json")
	recVerifyMFA := httptest.NewRecorder()
	router.ServeHTTP(recVerifyMFA, reqVerifyMFA)
	if recVerifyMFA.Code != http.StatusOK {
		t.Fatalf("/auth/mfa/verify harus 200, got: %d %s", recVerifyMFA.Code, recVerifyMFA.Body.String())
	}
	var finalLoginResp struct {
		Data handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recVerifyMFA.Body.Bytes(), &finalLoginResp)
	secondAdminToken := finalLoginResp.Data.AccessToken
	if secondAdminToken == "" || finalLoginResp.Data.RefreshToken == "" {
		t.Fatalf("access_token / refresh_token kosong: %s", recVerifyMFA.Body.String())
	}

	// 3. Pendaftaran + Upload KYC + Masking NIK di GET /pendaftaran/{id}
	var desaID uuid.UUID
	_ = env.Pool.QueryRow(ctx, "SELECT wilayah_desa_id FROM koperasi WHERE id = $1;", env.KoperasiID).Scan(&desaID)

	pendaftaranReq, _ := json.Marshal(handlers.AjukanPendaftaranRequest{
		NIK:           fmt.Sprintf("320101%010d", time.Now().UnixNano()%10000000000),
		NamaLengkap:   "Calon Anggota Doc",
		TempatLahir:   "Bandung",
		TanggalLahir:  "1994-05-12",
		JenisKelamin:  "L",
		Pekerjaan:     "Petani",
		AlamatKTP:     "Jl. Merdeka No. 1",
		WilayahDesaID: desaID,
	})
	reqDaftar := httptest.NewRequest("POST", "/api/v1/pendaftaran", bytes.NewReader(pendaftaranReq))
	reqDaftar.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqDaftar.Header.Set("Content-Type", "application/json")
	recDaftar := httptest.NewRecorder()
	router.ServeHTTP(recDaftar, reqDaftar)
	if recDaftar.Code != http.StatusCreated {
		t.Fatalf("POST /pendaftaran harus 201, got: %d %s", recDaftar.Code, recDaftar.Body.String())
	}
	var daftarResp struct {
		Data struct {
			PendaftaranID string `json:"pendaftaran_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recDaftar.Body.Bytes(), &daftarResp)
	if daftarResp.Data.PendaftaranID == "" {
		t.Fatalf("POST /pendaftaran harus memuat data.pendaftaran_id: %s", recDaftar.Body.String())
	}

	// Upload file PNG valid (89 50 4E 47)
	var mpBuf bytes.Buffer
	mpWriter := multipart.NewWriter(&mpBuf)
	_ = mpWriter.WriteField("jenis_dokumen", "ktp")
	part, _ := mpWriter.CreateFormFile("file", "ktp.png")
	_, _ = part.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D})
	_ = mpWriter.Close()
	reqUp := httptest.NewRequest("POST", "/api/v1/pendaftaran/"+daftarResp.Data.PendaftaranID+"/dokumen", &mpBuf)
	reqUp.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqUp.Header.Set("Content-Type", mpWriter.FormDataContentType())
	recUp := httptest.NewRecorder()
	router.ServeHTTP(recUp, reqUp)
	if recUp.Code != http.StatusCreated {
		t.Fatalf("Upload dokumen KYC harus 201, got: %d %s", recUp.Code, recUp.Body.String())
	}

	reqGetPendaftaran := httptest.NewRequest("GET", "/api/v1/pendaftaran/"+daftarResp.Data.PendaftaranID, nil)
	reqGetPendaftaran.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recGetPendaftaran := httptest.NewRecorder()
	router.ServeHTTP(recGetPendaftaran, reqGetPendaftaran)
	if recGetPendaftaran.Code != http.StatusOK || !strings.Contains(recGetPendaftaran.Body.String(), "****") {
		t.Fatalf("GET /pendaftaran/{id} harus 200 dengan nik_tersamar bermasker ****, got: %d %s", recGetPendaftaran.Code, recGetPendaftaran.Body.String())
	}

	// 4. Iuran Jenis (`KEPUTUSAN AKUNTAN`) + Pembayaran QRIS + Idempotency-Key (201, replay, 422) + Klaim + Verifikasi 403/200
	iuranPayload, _ := json.Marshal(handlers.CreateJenisIuranRequest{
		Kode:           fmt.Sprintf("IUR_%s", uuid.New().String()[:6]),
		Nama:           "Iuran Wajib Doc",
		Tipe:           "wajib",
		NominalStandar: 50000,
	})
	reqIuran := httptest.NewRequest("POST", "/api/v1/iuran/jenis", bytes.NewReader(iuranPayload))
	reqIuran.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqIuran.Header.Set("Content-Type", "application/json")
	recIuran := httptest.NewRecorder()
	router.ServeHTTP(recIuran, reqIuran)
	if recIuran.Code != http.StatusCreated {
		t.Fatalf("POST /iuran/jenis harus 201, got: %d %s", recIuran.Code, recIuran.Body.String())
	}

	reqListIuran := httptest.NewRequest("GET", "/api/v1/iuran/jenis", nil)
	reqListIuran.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recListIuran := httptest.NewRecorder()
	router.ServeHTTP(recListIuran, reqListIuran)
	if recListIuran.Code != http.StatusOK || !strings.Contains(recListIuran.Body.String(), "KEPUTUSAN AKUNTAN") {
		t.Fatalf("GET /iuran/jenis harus 200 dengan label KEPUTUSAN AKUNTAN, got: %d %s", recListIuran.Code, recListIuran.Body.String())
	}

	tagihanID := env.CreateBill(t, env.AnggotaID, 75000)
	idempKey := "doc-idemp-" + uuid.New().String()
	qrisPayload, _ := json.Marshal(handlers.BuatQRISRequest{
		JenisTransaksi:  "iuran_bulanan",
		TagihanIuranIDs: []uuid.UUID{tagihanID},
	})

	reqQris1 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(qrisPayload))
	reqQris1.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqQris1.Header.Set("Content-Type", "application/json")
	reqQris1.Header.Set("Idempotency-Key", idempKey)
	recQris1 := httptest.NewRecorder()
	router.ServeHTTP(recQris1, reqQris1)
	if recQris1.Code != http.StatusCreated {
		t.Fatalf("POST /pembayaran/qris pertama harus 201, got: %d %s", recQris1.Code, recQris1.Body.String())
	}
	var qrisResp1 struct {
		Data struct {
			PembayaranID string `json:"pembayaran_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recQris1.Body.Bytes(), &qrisResp1)
	if qrisResp1.Data.PembayaranID == "" {
		t.Fatalf("POST /pembayaran/qris harus memuat data.pembayaran_id: %s", recQris1.Body.String())
	}

	// Replay identik -> 201 pembayaran_id sama
	reqQrisReplay := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(qrisPayload))
	reqQrisReplay.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqQrisReplay.Header.Set("Content-Type", "application/json")
	reqQrisReplay.Header.Set("Idempotency-Key", idempKey)
	recQrisReplay := httptest.NewRecorder()
	router.ServeHTTP(recQrisReplay, reqQrisReplay)
	var qrisRespReplay struct {
		Data struct {
			PembayaranID string `json:"pembayaran_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recQrisReplay.Body.Bytes(), &qrisRespReplay)
	if recQrisReplay.Code != http.StatusCreated || qrisRespReplay.Data.PembayaranID != qrisResp1.Data.PembayaranID {
		t.Fatalf("Replay Idempotency-Key identik gagal: code=%d id1=%s id2=%s", recQrisReplay.Code, qrisResp1.Data.PembayaranID, qrisRespReplay.Data.PembayaranID)
	}

	// Body berbeda + key sama -> 422 IDEMPOTENCY_CONFLICT
	tagihanLain := env.CreateBill(t, env.AnggotaID, 85000)
	qrisPayloadDiff, _ := json.Marshal(handlers.BuatQRISRequest{
		JenisTransaksi:  "iuran_bulanan",
		TagihanIuranIDs: []uuid.UUID{tagihanLain},
	})
	reqQrisConflict := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(qrisPayloadDiff))
	reqQrisConflict.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqQrisConflict.Header.Set("Content-Type", "application/json")
	reqQrisConflict.Header.Set("Idempotency-Key", idempKey)
	recQrisConflict := httptest.NewRecorder()
	router.ServeHTTP(recQrisConflict, reqQrisConflict)
	if recQrisConflict.Code != http.StatusUnprocessableEntity || !strings.Contains(recQrisConflict.Body.String(), "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("Idempotency body berbeda harus 422 IDEMPOTENCY_CONFLICT, got: %d %s", recQrisConflict.Code, recQrisConflict.Body.String())
	}

	// Klaim bukti oleh anggota -> verifikasi oleh admin kedua -> 200
	buktiBody, _ := json.Marshal(handlers.KlaimBuktiRequest{ReferensiRRN: "RRNDOC123456"})
	reqBukti := httptest.NewRequest("POST", "/api/v1/pembayaran/"+qrisResp1.Data.PembayaranID+"/bukti", bytes.NewReader(buktiBody))
	reqBukti.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqBukti.Header.Set("Content-Type", "application/json")
	recBukti := httptest.NewRecorder()
	router.ServeHTTP(recBukti, reqBukti)
	if recBukti.Code != http.StatusOK {
		t.Fatalf("POST /pembayaran/{id}/bukti harus 200, got: %d %s", recBukti.Code, recBukti.Body.String())
	}

	verifBody, _ := json.Marshal(handlers.VerifikasiRequest{Disetujui: true})
	reqVerif := httptest.NewRequest("POST", "/api/v1/pembayaran/"+qrisResp1.Data.PembayaranID+"/verifikasi", bytes.NewReader(verifBody))
	reqVerif.Header.Set("Authorization", "Bearer "+secondAdminToken)
	reqVerif.Header.Set("Content-Type", "application/json")
	recVerif := httptest.NewRecorder()
	router.ServeHTTP(recVerif, reqVerif)
	if recVerif.Code != http.StatusOK {
		t.Fatalf("POST /pembayaran/{id}/verifikasi oleh admin kedua harus 200, got: %d %s", recVerif.Code, recVerif.Body.String())
	}

	// Suppress unused pgx warning check
	_ = pgx.ErrNoRows
}
