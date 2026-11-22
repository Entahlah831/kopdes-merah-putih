package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseEnvFile membaca pasangan KEY=VALUE dari berkas .env tanpa mengekspansi shell.
func ParseEnvFile(path string) (map[string]string, error) {
	// #nosec G304 -- membaca path berkas env yang dikonfigurasi caller/CLI
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("%s:%d: format baris tidak valid (harapkan KEY=VALUE)", path, lineNo)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		values[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// LoadFromEnvAndFile memuat dari os.LookupEnv dan mengisi nilai kosong dari file fallback bila tersedia.
func LoadFromEnvAndFile(path string) (AppConfig, error) {
	var fileMap map[string]string
	if path != "" {
		if m, err := ParseEnvFile(path); err == nil {
			fileMap = m
		}
	}
	return Load(func(key string) (string, bool) {
		if v, ok := os.LookupEnv(key); ok {
			return v, true
		}
		if fileMap != nil {
			v, ok := fileMap[key]
			return v, ok
		}
		return "", false
	})
}

// WriteEnvReferenceFile menulis docs/ENV_REFERENCE.md yang di-generate dari struct AppConfig.
func WriteEnvReferenceFile(path string) error {
	return os.WriteFile(path, []byte(RenderEnvReferenceMarkdown()), 0o600)
}
