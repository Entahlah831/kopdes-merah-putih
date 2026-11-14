package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/kopdes/merah-putih/internal/api"
)

// TestQRISSlotExhaustion_1500ConcurrentSameNominal mensimulasikan 1.500 permintaan
// pembuatan QRIS secara bersamaan pada nominal dasar yang persis sama dengan kode unik
// 3 digit (kapasitas 999 slot: 001..999). Maksimal 999 request berhasil (201), sisanya
// menerima respons ramah HTTP 409 Conflict, dan 0 duplikasi slot aktif di database.
func TestQRISSlotExhaustion_1500ConcurrentSameNominal(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	cfg := env.Config
	cfg.RateLimitRPS = 50000
	cfg.RateLimitBurst = 50000
	srv := api.NewServer(env.Pool, cfg)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}

	_, _ = conn.Exec(ctx, "RESET ROLE;")
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())

	jenisID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO jenis_iuran (id, koperasi_id, kode, nama, nominal, berlaku_mulai, created_by)
		VALUES ($1, $2, $3, 'Iuran Uji Slot 1500', 50000, CURRENT_DATE, $4);
	`, jenisID, env.KoperasiID, "SLOT-"+uuid.New().String()[:6], env.AdminUserID)
	if err != nil {
		conn.Release()
		t.Fatalf("seed jenis_iuran failed: %v", err)
	}

	const totalRequests = 1500
	const concurrency = 100

	tagihanIDs := make([]uuid.UUID, totalRequests)
	batch := &pgx.Batch{}
	for i := 0; i < totalRequests; i++ {
		tid := uuid.New()
		tagihanIDs[i] = tid
		periode := fmt.Sprintf("%04d-%02d", 2000+(i/12), (i%12)+1)
		batch.Queue(`
			INSERT INTO tagihan_iuran (id, koperasi_id, anggota_id, jenis_iuran_id, periode, jatuh_tempo, nominal_pokok, status, created_by)
			VALUES ($1, $2, $3, $4, $5, CURRENT_DATE + 30, 50000, 'belum_bayar', $6);
		`, tid, env.KoperasiID, env.AnggotaID, jenisID, periode, env.AdminUserID)
	}
	br := conn.SendBatch(ctx, batch)
	for i := 0; i < totalRequests; i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			conn.Release()
			t.Fatalf("seed tagihan %d failed: %v", i, err)
		}
	}
	_ = br.Close()
	conn.Release()

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        concurrency * 2,
			MaxIdleConnsPerHost: concurrency * 2,
		},
	}

	var (
		created201  atomic.Int64
		conflict409 atomic.Int64
		serverErr5  atomic.Int64
		otherStatus atomic.Int64
	)

	jobs := make(chan int, totalRequests)
	for i := 0; i < totalRequests; i++ {
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
				bodyBytes, _ := json.Marshal(map[string]interface{}{
					"tagihan_iuran_id": tagihanIDs[idx],
					"rekening_qris_id": env.RekeningID,
				})
				req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/pembayaran/qris", bytes.NewReader(bodyBytes))
				if err != nil {
					otherStatus.Add(1)
					continue
				}
				req.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
				req.Header.Set("Content-Type", "application/json")

				resp, err := client.Do(req)
				if err != nil {
					otherStatus.Add(1)
					continue
				}
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
					otherStatus.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	dur := time.Since(start)

	t.Logf("QRIS Slot Exhaustion (1500 concurrent requests, %d workers, %.2fs): 201_Created=%d, 409_Conflict=%d, 5xx=%d, other=%d",
		concurrency, dur.Seconds(), created201.Load(), conflict409.Load(), serverErr5.Load(), otherStatus.Load())

	if serverErr5.Load() != 0 {
		t.Fatalf("Expected 0 server 5xx errors, got %d", serverErr5.Load())
	}
	if otherStatus.Load() != 0 {
		t.Fatalf("Expected 0 unexpected status errors, got %d", otherStatus.Load())
	}
	if created201.Load() != 999 {
		t.Fatalf("Expected exactly 999 successful 201 allocations on fresh rekening, got %d", created201.Load())
	}
	if conflict409.Load() != 501 {
		t.Fatalf("Expected exactly 501 friendly 409 Conflict responses, got %d", conflict409.Load())
	}

	// Verify DB has 0 duplicate active (rekening_qris_id, nominal_total)
	conn2, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire verify conn: %v", err)
	}
	defer conn2.Release()
	_, _ = conn2.Exec(ctx, "RESET ROLE;")
	_, _ = conn2.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())

	var activeRows, distinctNominalTotal, duplicatePairs int
	err = conn2.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(DISTINCT nominal_total)::int,
			(SELECT COUNT(*) FROM (
				SELECT rekening_qris_id, nominal_total
				FROM pembayaran
				WHERE koperasi_id = $1 AND rekening_qris_id = $2 AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi') AND deleted_at IS NULL
				GROUP BY rekening_qris_id, nominal_total
				HAVING COUNT(*) > 1
			) d)::int
		FROM pembayaran
		WHERE koperasi_id = $1 AND rekening_qris_id = $2 AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi') AND deleted_at IS NULL;
	`, env.KoperasiID, env.RekeningID).Scan(&activeRows, &distinctNominalTotal, &duplicatePairs)
	if err != nil {
		t.Fatalf("verify DB slot uniqueness failed: %v", err)
	}

	t.Logf("DB Verification: activeRows=%d, distinctNominalTotal=%d, duplicatePairs=%d", activeRows, distinctNominalTotal, duplicatePairs)
	if activeRows != 999 || distinctNominalTotal != 999 || duplicatePairs != 0 {
		t.Fatalf("DB uniqueness mismatch: activeRows=%d distinct=%d dup=%d", activeRows, distinctNominalTotal, duplicatePairs)
	}
}
