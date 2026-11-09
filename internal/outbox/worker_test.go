package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/outbox"
)

type MockNotifier struct {
	ShouldFail bool
	CallCount  int
}

func (m *MockNotifier) SendNotification(ctx context.Context, koperasiID uuid.UUID, recipient, subject, body string) error {
	m.CallCount++
	if m.ShouldFail {
		return errors.New("simulated notification gateway failure")
	}
	return nil
}

func getTestPool(t testing.TB) *pgxpool.Pool {
	candidates := []string{
		"postgres://kopdes_app:kopdes_dev_password@127.0.0.1:5433/kopdes_db?sslmode=disable",
		"postgres://postgres:kopdes_dev_password@127.0.0.1:5433/kopdes_db?sslmode=disable",
	}
	if envDSN := os.Getenv("DATABASE_URL"); envDSN != "" {
		candidates = append([]string{envDSN}, candidates...)
	}

	for _, dsn := range candidates {
		pool, err := pgxpool.New(context.Background(), dsn)
		if err == nil && pool.Ping(context.Background()) == nil {
			return pool
		}
		if pool != nil {
			pool.Close()
		}
	}
	t.Fatalf("Database test tidak dapat diakses")
	return nil
}

func TestOutboxWorker_ProcessBatch(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// 1. Buat data uji koperasi & event outbox
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")

	wilayahID := uuid.New()
	koperasiID := uuid.New()
	userID := uuid.New()
	eventID := uuid.New()

	kodeWil := fmt.Sprintf("%d", time.Now().UnixNano())[:18]
	_, err = conn.Exec(ctx, `
		INSERT INTO wilayah (id, kode_kemendagri, kode_bps, nama, tingkat)
		VALUES ($1, $2, $2, 'Desa Outbox', 'desa_kelurahan')
		ON CONFLICT (kode_kemendagri) DO NOTHING;
	`, wilayahID, kodeWil)
	if err != nil {
		t.Fatalf("Seed wilayah failed: %v", err)
	}

	kodeKop := fmt.Sprintf("KOP-OBX-%s", uuid.New().String()[:15])
	_, err = conn.Exec(ctx, `
		INSERT INTO koperasi (id, kode_koperasi, nama, jenis_kopdes, wilayah_desa_id, alamat)
		VALUES ($1, $2, 'Koperasi Outbox Test', 'desa', $3, 'Jl. Outbox');
	`, koperasiID, kodeKop, wilayahID)
	if err != nil {
		t.Fatalf("Seed koperasi failed: %v", err)
	}

	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", koperasiID.String())

	_, err = conn.Exec(ctx, `
		INSERT INTO outbox_event (id, koperasi_id, topik, agregat_tipe, agregat_id, payload, status)
		VALUES ($1, $2, 'pembayaran.disetujui', 'pembayaran', $3, '{"nominal": 50000}', 'menunggu');
	`, eventID, koperasiID, userID)
	if err != nil {
		t.Fatalf("Seed outbox_event failed: %v", err)
	}

	// 2. Uji keberhasilan notifikasi
	mockSuccess := &MockNotifier{ShouldFail: false}
	workerSuccess := outbox.NewWorker(pool, mockSuccess)

	proc, failed, err := workerSuccess.ProcessBatch(ctx, 50)
	if err != nil {
		t.Fatalf("ProcessBatch failed: %v", err)
	}
	if proc < 1 {
		t.Errorf("Expected at least 1 processed event, got %d", proc)
	}
	if failed != 0 {
		t.Errorf("Expected 0 failed events, got %d", failed)
	}
	if mockSuccess.CallCount < 1 {
		t.Errorf("Expected Notifier to be called at least once")
	}

	// Verifikasi di DB: status telah menjadi 'selesai'
	var dbStatus string
	_ = conn.QueryRow(ctx, "SELECT status FROM outbox_event WHERE id = $1", eventID).Scan(&dbStatus)
	if dbStatus != "selesai" {
		t.Errorf("Expected DB status 'selesai', got '%s'", dbStatus)
	}
}
