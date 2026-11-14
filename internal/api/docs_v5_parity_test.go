package api_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type docRolesMatrix struct {
	Version   string              `json:"version"`
	Roles     []string            `json:"roles"`
	Endpoints []docRolesMatrixRow `json:"endpoints"`
}

type docRolesMatrixRow struct {
	Method          string                 `json:"method"`
	Path            string                 `json:"path"`
	Group           string                 `json:"group"`
	AuthMode        string                 `json:"auth_mode"`
	SamplePath      string                 `json:"sample_path"`
	SampleBody      map[string]interface{} `json:"sample_body,omitempty"`
	Roles           map[string]interface{} `json:"roles"`
	Unauthenticated interface{}            `json:"unauthenticated"`
	CrossTenant     string                 `json:"cross_tenant"`
	Notes           string                 `json:"notes"`
}

func loadOpenAPIOpsFromYAML(t *testing.T, yamlPath string) (map[string]bool, string) {
	t.Helper()
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("gagal baca %s: %v", yamlPath, err)
	}
	text := string(raw)
	versionRe := regexp.MustCompile(`(?m)^  version:\s*([^\s#]+)`)
	version := ""
	if m := versionRe.FindStringSubmatch(text); len(m) == 2 {
		version = strings.Trim(m[1], `"'`)
	}

	ops := make(map[string]bool)
	lines := strings.Split(text, "\n")
	inPaths := false
	currentPath := ""
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "paths:" {
			inPaths = true
			continue
		}
		if inPaths && len(trimmed) > 0 && trimmed[0] != ' ' && trimmed[0] != '#' {
			break
		}
		if !inPaths {
			continue
		}
		if strings.HasPrefix(trimmed, "  /") && strings.HasSuffix(strings.TrimSpace(trimmed), ":") {
			currentPath = strings.TrimSuffix(strings.TrimSpace(trimmed), ":")
			continue
		}
		if currentPath != "" && strings.HasPrefix(trimmed, "    ") && !strings.HasPrefix(trimmed, "      ") {
			m := strings.TrimSuffix(strings.TrimSpace(trimmed), ":")
			mu := strings.ToUpper(m)
			if mu == "GET" || mu == "POST" || mu == "PUT" || mu == "PATCH" || mu == "DELETE" {
				ops[mu+" "+currentPath] = true
			}
		}
	}
	return ops, version
}

func TestDoc_OpenAPI_JSON_Parity(t *testing.T) {
	root := filepath.Join("..", "..")
	yamlOps, yamlVer := loadOpenAPIOpsFromYAML(t, filepath.Join(root, "docs", "openapi.yaml"))

	jsonPath := filepath.Join(root, "frontend-tester", "openapi.json")
	rawJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("frontend-tester/openapi.json belum ada atau gagal dibaca: %v", err)
	}

	var spec struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]map[string]interface{} `json:"paths"`
	}
	if err := json.Unmarshal(rawJSON, &spec); err != nil {
		t.Fatalf("frontend-tester/openapi.json bukan JSON valid: %v", err)
	}
	if spec.Info.Version != yamlVer {
		t.Fatalf("versi openapi.json (%s) berbeda dari openapi.yaml (%s)", spec.Info.Version, yamlVer)
	}

	jsonOps := make(map[string]bool)
	for p, methods := range spec.Paths {
		for m := range methods {
			mu := strings.ToUpper(m)
			if mu == "GET" || mu == "POST" || mu == "PUT" || mu == "PATCH" || mu == "DELETE" {
				jsonOps[mu+" "+p] = true
			}
		}
	}

	if len(jsonOps) != len(yamlOps) {
		t.Fatalf("jumlah operasi openapi.json (%d) != openapi.yaml (%d)", len(jsonOps), len(yamlOps))
	}
	for k := range yamlOps {
		if !jsonOps[k] {
			t.Errorf("operasi %s ada di openapi.yaml tetapi hilang di frontend-tester/openapi.json", k)
		}
	}
}

func TestDoc_RolesMatrix_Parity(t *testing.T) {
	root := filepath.Join("..", "..")
	yamlOps, _ := loadOpenAPIOpsFromYAML(t, filepath.Join(root, "docs", "openapi.yaml"))

	docsMatrixPath := filepath.Join(root, "docs", "roles_matrix.json")
	uiMatrixPath := filepath.Join(root, "frontend-tester", "roles_matrix.json")

	rawDocs, err := os.ReadFile(docsMatrixPath)
	if err != nil {
		t.Fatalf("docs/roles_matrix.json belum ada: %v", err)
	}
	rawUI, err := os.ReadFile(uiMatrixPath)
	if err != nil {
		t.Fatalf("frontend-tester/roles_matrix.json belum ada: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(rawDocs), bytes.TrimSpace(rawUI)) {
		t.Fatalf("frontend-tester/roles_matrix.json tidak sinkron byte-for-byte dengan docs/roles_matrix.json")
	}

	var matrix docRolesMatrix
	if err := json.Unmarshal(rawDocs, &matrix); err != nil {
		t.Fatalf("docs/roles_matrix.json bukan JSON valid: %v", err)
	}

	requiredRoles := []string{"admin", "pengurus", "bendahara", "ketua", "pengawas", "operator", "anggota"}
	matrixOps := make(map[string]bool)
	for _, ep := range matrix.Endpoints {
		key := strings.ToUpper(ep.Method) + " " + ep.Path
		if matrixOps[key] {
			t.Errorf("duplikasi operasi di roles_matrix.json: %s", key)
		}
		matrixOps[key] = true
		for _, r := range requiredRoles {
			if _, ok := ep.Roles[r]; !ok {
				t.Errorf("operasi %s tidak mendefinisikan role %s", key, r)
			}
		}
		if ep.Unauthenticated == nil {
			t.Errorf("operasi %s tidak mendefinisikan unauthenticated", key)
		}
		if strings.TrimSpace(ep.CrossTenant) == "" {
			t.Errorf("operasi %s tidak mendefinisikan cross_tenant", key)
		}
	}

	for op := range yamlOps {
		if !matrixOps[op] {
			t.Errorf("operasi OpenAPI %s belum tercakup di docs/roles_matrix.json", op)
		}
	}
	for op := range matrixOps {
		if !yamlOps[op] {
			t.Errorf("operasi tambahan %s di docs/roles_matrix.json tidak ada di docs/openapi.yaml", op)
		}
	}

	fgBytes, err := os.ReadFile(filepath.Join(root, "docs", "FRONTEND_GUIDE.md"))
	if err != nil {
		t.Fatalf("gagal baca docs/FRONTEND_GUIDE.md: %v", err)
	}
	fg := string(fgBytes)
	beginIdx := strings.Index(fg, "<!-- BEGIN_ROLES_MATRIX -->")
	endIdx := strings.Index(fg, "<!-- END_ROLES_MATRIX -->")
	if beginIdx < 0 || endIdx <= beginIdx {
		t.Fatalf("docs/FRONTEND_GUIDE.md belum memiliki blok <!-- BEGIN_ROLES_MATRIX --> .. <!-- END_ROLES_MATRIX -->")
	}
	block := fg[beginIdx:endIdx]
	for _, ep := range matrix.Endpoints {
		if !strings.Contains(block, ep.Path) || !strings.Contains(block, strings.ToUpper(ep.Method)) {
			t.Errorf("tabel peran di docs/FRONTEND_GUIDE.md belum mencantumkan %s %s", strings.ToUpper(ep.Method), ep.Path)
		}
	}
}
