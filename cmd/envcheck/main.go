package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/config"
)

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("envcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)

	envFile := fs.String("env-file", ".env.test", "Berkas environment yang diperiksa")
	strict := fs.Bool("strict", false, "Validasi ketat kelengkapan kunci, tipe, panjang rahasia, dan konsistensi DSN")
	compare := fs.Bool("compare", false, "Bandingkan dua berkas env: envcheck --compare FILE_A FILE_B")
	ports := fs.Bool("ports", false, "Periksa konflik antar-port di config dan keterpakaian port host via TCP bind")
	noListen := fs.Bool("no-listen", false, "Lewati uji bind TCP langsung (hanya cek konflik internal konfigurasi)")
	genDoc := fs.String("gen-doc", "", "Generate dokumentasi Markdown otomatis ke path target (mis. docs/ENV_REFERENCE.md)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *genDoc != "" {
		if err := config.WriteEnvReferenceFile(*genDoc); err != nil {
			fmt.Fprintf(stderr, "ERROR gagal menulis %s: %v\n", *genDoc, err)
			return 1
		}
		fmt.Fprintf(stdout, "Sukses generate %s dari struct AppConfig.\n", *genDoc)
		return 0
	}

	if *compare {
		rest := fs.Args()
		if len(rest) != 2 {
			fmt.Fprintln(stderr, "Penggunaan: envcheck --compare <FILE_A> <FILE_B>")
			return 2
		}
		return runCompare(rest[0], rest[1], stdout, stderr)
	}

	m, err := config.ParseEnvFile(*envFile)
	if err != nil {
		fmt.Fprintf(stderr, "ERROR membaca %s: %v\n", *envFile, err)
		return 1
	}

	cfg, err := config.Load(func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	})
	if err != nil {
		fmt.Fprintf(stderr, "ERROR parsing konfigurasi %s: %v\n", *envFile, err)
		return 1
	}

	if *ports {
		if code := runPortAudit(cfg, !*noListen, stdout, stderr); code != 0 {
			return code
		}
	}

	if *strict || !*ports {
		if code := runStrictAudit(cfg, m, stdout, stderr); code != 0 {
			return code
		}
	}

	return 0
}

func runStrictAudit(cfg config.AppConfig, raw map[string]string, stdout, stderr io.Writer) int {
	var issues []string
	for _, spec := range config.Catalog() {
		val, exists := raw[spec.Name]
		if !exists {
			issues = append(issues, fmt.Sprintf("KUNCI HILANG: %s tidak ada di berkas env", spec.Name))
			continue
		}
		if (spec.RequiredIn == "all" || (spec.RequiredIn == "prod" && strings.EqualFold(cfg.AppEnv, "production"))) && strings.TrimSpace(val) == "" {
			issues = append(issues, fmt.Sprintf("NILAI KOSONG: %s wajib diisi", spec.Name))
		}
	}

	if _, err := cfg.BuildDatabaseDSN(); err != nil {
		issues = append(issues, fmt.Sprintf("DSN DB TIDAK VALID: %v", err))
	}

	srvCfg := api.ConfigFromAppConfig(cfg)
	if err := srvCfg.Validate(); err != nil {
		issues = append(issues, fmt.Sprintf("VALIDASI KEAMANAN GAGAL: %v", err))
	}

	if strings.EqualFold(cfg.AppEnv, "production") {
		if err := cfg.ValidateProduction(); err != nil {
			issues = append(issues, fmt.Sprintf("GUARD PRODUKSI GAGAL: %v", err))
		}
		if err := api.ValidateProductionDatabasePassword(cfg.AppEnv, cfg.DBPassword, cfg.KopdesAppPassword); err != nil {
			issues = append(issues, fmt.Sprintf("PASSWORD DB PRODUKSI: %v", err))
		}
	}

	if len(issues) > 0 {
		for _, iss := range issues {
			fmt.Fprintln(stderr, iss)
		}
		return 1
	}

	fmt.Fprintf(stdout, "envcheck STRICT PASS: %d variabel terverifikasi, APP_ENV=%s, DB_NAME=%s, API_PORT=%d, METRICS_PORT=%d\n",
		len(config.Catalog()), cfg.AppEnv, cfg.DBName, cfg.APIPort, cfg.MetricsPort)
	return 0
}

func runPortAudit(cfg config.AppConfig, checkBind bool, stdout, stderr io.Writer) int {
	var issues []string

	// 1. Cek bentrokan port host publik
	hostPorts := map[int][]string{}
	addHostPort := func(port int, name string) {
		if port <= 0 {
			return
		}
		hostPorts[port] = append(hostPorts[port], name)
	}
	addHostPort(cfg.APIHostPort, "API_HOST_PORT")
	addHostPort(cfg.DBHostPort, "DB_HOST_PORT")
	addHostPort(cfg.MetricsHostPort, "METRICS_HOST_PORT")
	addHostPort(cfg.PrometheusHostPort, "PROMETHEUS_HOST_PORT")

	for port, owners := range hostPorts {
		if len(owners) > 1 {
			issues = append(issues, fmt.Sprintf("KONFLIK PORT HOST %d digunakan bersama oleh: %s", port, strings.Join(owners, ", ")))
		}
	}

	// 2. Uji ketersediaan bind TCP lokal bila diminta
	if checkBind {
		var sortedPorts []int
		for p := range hostPorts {
			sortedPorts = append(sortedPorts, p)
		}
		sort.Ints(sortedPorts)
		bindHost := cfg.DBBindHost
		if bindHost == "" {
			bindHost = "127.0.0.1"
		}
		for _, p := range sortedPorts {
			addr := fmt.Sprintf("%s:%d", bindHost, p)
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				issues = append(issues, fmt.Sprintf("PORT SEDANG DIPAKAI (%s): %s (%v)", strings.Join(hostPorts[p], ","), addr, err))
				continue
			}
			_ = ln.Close()
		}
	}

	if len(issues) > 0 {
		for _, iss := range issues {
			fmt.Fprintln(stderr, iss)
		}
		return 1
	}
	fmt.Fprintf(stdout, "envcheck PORTS PASS: tidak ada konflik port pada konfigurasi.\n")
	return 0
}

func runCompare(pathA, pathB string, stdout, stderr io.Writer) int {
	mapA, err := config.ParseEnvFile(pathA)
	if err != nil {
		fmt.Fprintf(stderr, "ERROR membaca %s: %v\n", pathA, err)
		return 1
	}
	mapB, err := config.ParseEnvFile(pathB)
	if err != nil {
		fmt.Fprintf(stderr, "ERROR membaca %s: %v\n", pathB, err)
		return 1
	}

	secretSet := map[string]bool{}
	for _, s := range config.Catalog() {
		if s.Secret {
			secretSet[s.Name] = true
		}
	}

	allKeysMap := map[string]bool{}
	for k := range mapA {
		allKeysMap[k] = true
	}
	for k := range mapB {
		allKeysMap[k] = true
	}
	var keys []string
	for k := range allKeysMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var onlyA, onlyB []string
	for _, k := range keys {
		va, inA := mapA[k]
		vb, inB := mapB[k]
		if inA && !inB {
			onlyA = append(onlyA, k)
			continue
		}
		if !inA && inB {
			onlyB = append(onlyB, k)
			continue
		}
		dispA, dispB := va, vb
		if secretSet[k] {
			if va != "" {
				dispA = "***REDACTED***"
			}
			if vb != "" {
				dispB = "***REDACTED***"
			}
		}
		if va != vb {
			fmt.Fprintf(stdout, "DIFF  %-32s A=%-20s B=%s\n", k, dispA, dispB)
		}
	}

	if len(onlyA) > 0 {
		fmt.Fprintf(stderr, "HANYA DI %s: %s\n", pathA, strings.Join(onlyA, ", "))
	}
	if len(onlyB) > 0 {
		fmt.Fprintf(stderr, "HANYA DI %s: %s\n", pathB, strings.Join(onlyB, ", "))
	}
	if len(onlyA) > 0 || len(onlyB) > 0 {
		return 1
	}
	fmt.Fprintf(stdout, "envcheck COMPARE PASS: skema kunci 100%% identik (%d kunci).\n", len(keys))
	return 0
}
