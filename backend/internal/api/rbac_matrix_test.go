package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/testseed"
)

type rolesMatrixDoc struct {
	Roles        []string            `json:"roles"`
	RouteCatalog []roleCatalogEntry  `json:"route_catalog"`
	Endpoints    []rolesMatrixTarget `json:"endpoints"`
}

type roleCatalogEntry struct {
	Pattern      string   `json:"pattern"`
	AllowedRoles []string `json:"allowed_roles"`
	Note         string   `json:"note"`
}

type rolesMatrixTarget struct {
	ID             string                 `json:"id"`
	Method         string                 `json:"method"`
	Path           string                 `json:"path"`
	Note           string                 `json:"note"`
	Body           map[string]interface{} `json:"body"`
	ExpectedStatus map[string]interface{} `json:"expected_status"`
}

func extractLiveRoutePatterns(t *testing.T, root string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "internal", "api", "routes.go"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`mux\.Handle(?:Func)?\("([^"]+)"`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

func matchesExpectedCode(got int, rawExp interface{}) bool {
	switch v := rawExp.(type) {
	case float64:
		return got == int(v)
	case []interface{}:
		for _, item := range v {
			if got == int(item.(float64)) {
				return true
			}
		}
	}
	return false
}

func TestRBAC_MatrixMatchesLiveRouterAndEnforcesBoundaries(t *testing.T) {
	root := repoRootDir(t)
	bRoot := backendRootDir(t)
	docBytes, err := os.ReadFile(filepath.Join(root, "docs", "roles_matrix.json"))
	if err != nil {
		t.Fatalf("read docs/roles_matrix.json: %v", err)
	}
	testerBytes, err := os.ReadFile(filepath.Join(root, "frontend", "tester", "roles_matrix.json"))
	if err != nil {
		t.Fatalf("read frontend/tester/roles_matrix.json: %v", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(docBytes, []byte("\r\n"), []byte("\n")), bytes.ReplaceAll(testerBytes, []byte("\r\n"), []byte("\n"))) {
		t.Fatal("docs/roles_matrix.json and frontend/tester/roles_matrix.json are out of sync")
	}

	var doc rolesMatrixDoc
	if err := json.Unmarshal(docBytes, &doc); err != nil {
		t.Fatalf("parse roles_matrix.json: %v", err)
	}
	liveRoutes := extractLiveRoutePatterns(t, bRoot)
	catSet := make(map[string]bool, len(doc.RouteCatalog))
	for _, rc := range doc.RouteCatalog {
		catSet[rc.Pattern] = true
	}
	for _, lr := range liveRoutes {
		if !catSet[lr] {
			t.Fatalf("registered route %q missing from docs/roles_matrix.json route_catalog", lr)
		}
	}
	liveSet := make(map[string]bool, len(liveRoutes))
	for _, lr := range liveRoutes {
		liveSet[lr] = true
	}
	for _, rc := range doc.RouteCatalog {
		if !liveSet[rc.Pattern] {
			t.Fatalf("stale route %q in route_catalog not found in routes.go", rc.Pattern)
		}
	}

	e := setupE2EEnv(t, ".env.test")
	// Gunakan kapasitas burst lebih besar untuk sapuan 100+ permintaan matriks berurutan dari localhost
	rbacCfg := ConfigFromAppConfig(e.appCfg)
	rbacCfg.StorageKYCDir = e.kycDir
	rbacCfg.StorageBuktiDir = e.buktiDir
	rbacCfg.RateLimitRPS = 1000
	rbacCfg.RateLimitBurst = 1000
	rbacSrv := httptest.NewServer(NewServer(e.pool, rbacCfg).Router())
	defer rbacSrv.Close()

	tokens := map[string]string{
		"unauthenticated": "",
		"anggota":         loginSession(t, e, "anggota1@kopdes-test.local", e.seedPwd, ""),
		"petugas_toko":    loginSession(t, e, "petugas@kopdes-test.local", e.seedPwd, ""),
		"bendahara":       loginSession(t, e, "bendahara@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "bendahara@kopdes-test.local")),
		"admin":           loginSession(t, e, "admin@kopdes-test.local", e.seedPwd, testseed.DeterministicTOTPSecret(e.appCfg, "admin@kopdes-test.local")),
	}

	for _, ep := range doc.Endpoints {
		for _, role := range doc.Roles {
			var body interface{}
			if ep.Body != nil {
				body = ep.Body
			}
			st, _, raw := doJSONReq(t, ep.Method, rbacSrv.URL+ep.Path, tokens[role], body, nil)
			if !matchesExpectedCode(st, ep.ExpectedStatus[role]) {
				t.Fatalf("%s (%s %s) role=%s got=%d want=%v body=%s", ep.ID, ep.Method, ep.Path, role, st, ep.ExpectedStatus[role], string(raw))
			}
		}
	}

	// Eksplisit pastikan petugas_toko ditolak 403 pada POST /api/v1/pembayaran tunai dan bendahara ditolak 403 pada PATCH status pesanan
	stPetugasCash, _, rawPC := doJSONReq(t, http.MethodPost, rbacSrv.URL+"/api/v1/pembayaran", tokens["petugas_toko"], map[string]interface{}{
		"jenis_transaksi": "iuran", "metode": "tunai",
	}, nil)
	if stPetugasCash != http.StatusForbidden {
		t.Fatalf("petugas_toko must receive 403 on cash payment, got %d %s", stPetugasCash, string(rawPC))
	}
	stBendOrder, _, rawBO := doJSONReq(t, http.MethodPatch, rbacSrv.URL+"/api/v1/pesanan/00000000-0000-0000-0000-000000000000/status", tokens["bendahara"], map[string]string{
		"status": "diproses",
	}, nil)
	if stBendOrder != http.StatusForbidden {
		t.Fatalf("bendahara must receive 403 on order status update, got %d %s", stBendOrder, string(rawBO))
	}

	// Pastikan pendaftaran tanpa tanggal_lahir (minimisasi data D10) lolos hingga persetujuan & unmask anggota
	nik := "3208" + randomDigits12()
	hp := "0819" + randomDigits12()[:8]
	rSt, rBody, rRaw := doJSONReq(t, http.MethodPost, rbacSrv.URL+"/api/v1/public/pendaftaran", "", map[string]string{
		"nama_lengkap": "Warga Minimal PII", "email": "min-" + uuid.NewString()[:8] + "@kopdes-test.local",
		"nik": nik, "no_hp": hp, "alamat": "Dusun Harapan RT 01", "pekerjaan": "Petani",
	}, nil)
	if rSt != http.StatusCreated {
		t.Fatalf("registration without tanggal_lahir failed: %d %s", rSt, string(rRaw))
	}
	regData := rBody["data"].(map[string]interface{})
	regID := regData["id"].(string)
	uSt, _, uRaw := doMultipartReq(t, rbacSrv.URL+"/api/v1/public/pendaftaran/"+regID+"/dokumen", "", "file", "ktp.png", makeUniquePNG(t),
		map[string]string{"tipe_dokumen": "ktp"}, map[string]string{"X-Upload-Token": regData["upload_token"].(string)})
	if uSt != http.StatusCreated {
		t.Fatalf("upload KTP for minimal PII failed: %d %s", uSt, string(uRaw))
	}
	vSt, vBody, vRaw := doJSONReq(t, http.MethodPost, rbacSrv.URL+"/api/v1/pendaftaran/"+regID+"/verifikasi", tokens["admin"], map[string]string{
		"keputusan": "disetujui", "catatan": "Lolos minimisasi data",
	}, nil)
	if vSt != http.StatusOK {
		t.Fatalf("approve minimal PII failed: %d %s", vSt, string(vRaw))
	}
	angID := vBody["data"].(map[string]interface{})["anggota_id"].(string)
	gSt, gBody, gRaw := doJSONReq(t, http.MethodGet, rbacSrv.URL+"/api/v1/anggota/"+angID+"?unmask=true", tokens["admin"], nil, nil)
	if gSt != http.StatusOK {
		t.Fatalf("get unmasked member without tanggal_lahir failed: %d %s", gSt, string(gRaw))
	}
	if dob, _ := gBody["data"].(map[string]interface{})["tanggal_lahir"].(string); dob != "" {
		t.Fatalf("expected empty tanggal_lahir on minimized member, got %q", dob)
	}
}
