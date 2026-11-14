package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:18080", "Base URL API target")
	mode := flag.String("mode", "default", "Mode loadtest: default (health/produk) atau qris-slot-exhaustion")
	members := flag.Int("members", 2000, "Jumlah simulasi permintaan anggota")
	requests := flag.Int("requests", 1500, "Jumlah permintaan untuk mode qris-slot-exhaustion")
	concurrency := flag.Int("concurrency", 100, "Jumlah worker konkuren")
	bearerToken := flag.String("token", "", "Opsional Bearer token untuk endpoint terautentikasi")
	tagihanIDsFile := flag.String("tagihan-ids-file", "", "Berkas daftar UUID tagihan (1 per baris) untuk mode qris-slot-exhaustion")
	rekeningQRISID := flag.String("rekening-qris-id", "", "Opsional UUID rekening QRIS target untuk mode qris-slot-exhaustion")
	maxP95Ms := flag.Float64("max-p95-ms", 1500, "Batas ambang gagal p95 dalam milidetik")
	maxErrorPct := flag.Float64("max-error-pct", 1.0, "Batas ambang persentase error (mode default)")
	flag.Parse()

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        *concurrency * 2,
			MaxIdleConnsPerHost: *concurrency * 2,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	if *mode == "qris-slot-exhaustion" {
		runQRISSlotExhaustion(client, *baseURL, *bearerToken, *tagihanIDsFile, *rekeningQRISID, *requests, *concurrency)
		return
	}

	targetPath := "/healthz"
	if *bearerToken != "" {
		targetPath = "/api/v1/katalog/produk"
	}

	latencies := make([]time.Duration, 0, *members)
	var latMu sync.Mutex
	var errCount atomic.Int64
	var okCount atomic.Int64

	jobs := make(chan int, *members)
	for i := 0; i < *members; i++ {
		jobs <- i
	}
	close(jobs)

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				t0 := time.Now()
				var bodyReader io.Reader
				method := http.MethodGet
				path := targetPath
				if *bearerToken == "" && idx%5 == 0 {
					path = "/readyz"
				}
				req, err := http.NewRequest(method, *baseURL+path, bodyReader)
				if err != nil {
					errCount.Add(1)
					continue
				}
				if *bearerToken != "" {
					req.Header.Set("Authorization", "Bearer "+*bearerToken)
				}
				// Simulasikan 2.000 IP anggota berbeda melalui trusted proxy header
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.20.%d.%d", (idx/250)%250, (idx%250)+1))
				resp, err := client.Do(req)
				lat := time.Since(t0)
				if err != nil {
					errCount.Add(1)
				} else {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 400 {
						okCount.Add(1)
					} else {
						errCount.Add(1)
					}
				}
				latMu.Lock()
				latencies = append(latencies, lat)
				latMu.Unlock()
			}
		}()
	}
	wg.Wait()
	totalDur := time.Since(start)

	p50, p95, p99 := computePercentiles(latencies)

	totalReqs := int64(*members)
	errPct := float64(errCount.Load()) * 100.0 / float64(totalReqs)
	rps := float64(totalReqs) / totalDur.Seconds()

	report := map[string]interface{}{
		"mode":           *mode,
		"target":         *baseURL + targetPath,
		"members":        *members,
		"concurrency":    *concurrency,
		"duration_sec":   math.Round(totalDur.Seconds()*1000) / 1000,
		"rps":            math.Round(rps*100) / 100,
		"success_count":  okCount.Load(),
		"error_count":    errCount.Load(),
		"error_rate_pct": math.Round(errPct*1000) / 1000,
		"latency_ms": map[string]float64{
			"p50": math.Round(p50*100) / 100,
			"p95": math.Round(p95*100) / 100,
			"p99": math.Round(p99*100) / 100,
		},
	}
	printJSON(report)

	if errPct > *maxErrorPct {
		fmt.Fprintf(os.Stderr, "GAGAL: error_rate_pct %.3f%% > max %.3f%%\n", errPct, *maxErrorPct)
		os.Exit(1)
	}
	if p95 > *maxP95Ms {
		fmt.Fprintf(os.Stderr, "GAGAL: p95 %.2fms > max %.2fms\n", p95, *maxP95Ms)
		os.Exit(1)
	}
}

func runQRISSlotExhaustion(client *http.Client, baseURL, token, tagihanFile, rekeningID string, totalReqs, concurrency int) {
	if strings.TrimSpace(token) == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --token wajib diisi pada mode qris-slot-exhaustion")
		os.Exit(2)
	}
	tagihanIDs, err := readIDsFile(tagihanFile)
	if err != nil || len(tagihanIDs) == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: --tagihan-ids-file gagal dibaca atau kosong (%v)\n", err)
		os.Exit(2)
	}
	if totalReqs > len(tagihanIDs) {
		totalReqs = len(tagihanIDs)
	}

	var (
		created201  atomic.Int64
		conflict409 atomic.Int64
		serverErr5  atomic.Int64
		otherErr    atomic.Int64
		latMu       sync.Mutex
	)
	latencies := make([]time.Duration, 0, totalReqs)

	jobs := make(chan int, totalReqs)
	for i := 0; i < totalReqs; i++ {
		jobs <- i
	}
	close(jobs)

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				payloadObj := map[string]interface{}{
					"tagihan_iuran_id": tagihanIDs[idx],
				}
				if strings.TrimSpace(rekeningID) != "" {
					payloadObj["rekening_qris_id"] = strings.TrimSpace(rekeningID)
				}
				bodyBytes, _ := json.Marshal(payloadObj)
				req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/pembayaran/qris", bytes.NewReader(bodyBytes))
				if err != nil {
					otherErr.Add(1)
					continue
				}
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.21.%d.%d", (idx/250)%250, (idx%250)+1))

				t0 := time.Now()
				resp, err := client.Do(req)
				lat := time.Since(t0)
				if err != nil {
					otherErr.Add(1)
				} else {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					switch {
					case resp.StatusCode == http.StatusCreated:
						created201.Add(1)
					case resp.StatusCode == http.StatusConflict:
						conflict409.Add(1)
					case resp.StatusCode >= 500:
						serverErr5.Add(1)
					default:
						otherErr.Add(1)
					}
				}
				latMu.Lock()
				latencies = append(latencies, lat)
				latMu.Unlock()
			}
		}()
	}
	wg.Wait()
	totalDur := time.Since(start)
	p50, p95, p99 := computePercentiles(latencies)

	report := map[string]interface{}{
		"mode":            "qris-slot-exhaustion",
		"target":          baseURL + "/api/v1/pembayaran/qris",
		"total_requests":  totalReqs,
		"concurrency":     concurrency,
		"duration_sec":    math.Round(totalDur.Seconds()*1000) / 1000,
		"created_201":     created201.Load(),
		"conflict_409":    conflict409.Load(),
		"server_error_5x": serverErr5.Load(),
		"other_status":    otherErr.Load(),
		"latency_ms": map[string]float64{
			"p50": math.Round(p50*100) / 100,
			"p95": math.Round(p95*100) / 100,
			"p99": math.Round(p99*100) / 100,
		},
	}
	printJSON(report)

	if serverErr5.Load() > 0 || otherErr.Load() > 0 || created201.Load() > 999 {
		os.Exit(1)
	}
}

func readIDsFile(rawPath string) ([]string, error) {
	cleaned := filepath.Clean(rawPath)
	// #nosec G304 -- path eksplisit dari argumen CLI loadtest
	f, err := os.Open(cleaned)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var ids []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			ids = append(ids, line)
		}
	}
	return ids, sc.Err()
}

func computePercentiles(latencies []time.Duration) (float64, float64, float64) {
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p := func(q float64) float64 {
		if len(latencies) == 0 {
			return 0
		}
		idx := int(math.Ceil(q*float64(len(latencies)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(latencies) {
			idx = len(latencies) - 1
		}
		return float64(latencies[idx].Microseconds()) / 1000.0
	}
	return p(0.50), p(0.95), p(0.99)
}

func printJSON(v interface{}) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	fmt.Print(buf.String())
}
