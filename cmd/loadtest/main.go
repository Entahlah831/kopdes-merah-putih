package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:18080", "Base URL API target")
	members := flag.Int("members", 2000, "Jumlah simulasi permintaan anggota")
	concurrency := flag.Int("concurrency", 100, "Jumlah worker konkuren")
	bearerToken := flag.String("token", "", "Opsional Bearer token anggota untuk endpoint terautentikasi")
	maxP95Ms := flag.Float64("max-p95-ms", 1500, "Batas ambang gagal p95 dalam milidetik")
	maxErrorPct := flag.Float64("max-error-pct", 1.0, "Batas ambang persentase error")
	flag.Parse()

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        *concurrency * 2,
			MaxIdleConnsPerHost: *concurrency * 2,
			IdleConnTimeout:     30 * time.Second,
		},
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

	totalReqs := int64(*members)
	errPct := float64(errCount.Load()) * 100.0 / float64(totalReqs)
	rps := float64(totalReqs) / totalDur.Seconds()
	p50 := p(0.50)
	p95 := p(0.95)
	p99 := p(0.99)

	report := map[string]interface{}{
		"target":        *baseURL + targetPath,
		"members":       *members,
		"concurrency":   *concurrency,
		"duration_sec":  math.Round(totalDur.Seconds()*1000) / 1000,
		"rps":           math.Round(rps*100) / 100,
		"success_count": okCount.Load(),
		"error_count":   errCount.Load(),
		"error_rate_pct": math.Round(errPct*1000) / 1000,
		"latency_ms": map[string]float64{
			"p50": math.Round(p50*100) / 100,
			"p95": math.Round(p95*100) / 100,
			"p99": math.Round(p99*100) / 100,
		},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	fmt.Print(buf.String())

	if errPct > *maxErrorPct {
		fmt.Fprintf(os.Stderr, "GAGAL: error_rate_pct %.3f%% > max %.3f%%\n", errPct, *maxErrorPct)
		os.Exit(1)
	}
	if p95 > *maxP95Ms {
		fmt.Fprintf(os.Stderr, "GAGAL: p95 %.2fms > max %.2fms\n", p95, *maxP95Ms)
		os.Exit(1)
	}
}
