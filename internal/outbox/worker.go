package outbox

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Notifier mendefinisikan antarmuka pengiriman notifikasi (SMS/WA/Email/Push)
type Notifier interface {
	SendNotification(ctx context.Context, koperasiID uuid.UUID, recipient, subject, body string) error
}

// LogNotifier adalah implementasi default Notifier untuk pencatatan log
type LogNotifier struct{}

func (n *LogNotifier) SendNotification(ctx context.Context, koperasiID uuid.UUID, recipient, subject, body string) error {
	// Di lingkungan dev/staging, notifikasi dicatat ke log/sinkronisasi
	return nil
}

// Worker mengelola pemrosesan berkala antrean event transactional outbox
type Worker struct {
	pool     *pgxpool.Pool
	notifier Notifier
}

func NewWorker(pool *pgxpool.Pool, notifier Notifier) *Worker {
	if notifier == nil {
		notifier = &LogNotifier{}
	}
	return &Worker{
		pool:     pool,
		notifier: notifier,
	}
}

type EventRecord struct {
	ID              uuid.UUID
	KoperasiID      uuid.UUID
	Topik           string
	AgregatTipe     string
	AgregatID       uuid.UUID
	Payload         json.RawMessage
	JumlahPercobaan int
}

// ProcessBatch memproses batch antrean event outbox yang tersedia secara aman konkurensi (FOR UPDATE SKIP LOCKED)
func (w *Worker) ProcessBatch(ctx context.Context, batchSize int) (int, int, error) {
	if batchSize <= 0 {
		batchSize = 20
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	// Worker berjalan sebagai platform worker untuk polling antrean outbox multi-tenant
	if _, err := tx.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'true', true)"); err != nil {
		return 0, 0, err
	}

	rows, err := tx.Query(ctx, `
		SELECT id, koperasi_id, topik, agregat_tipe, agregat_id, payload, jumlah_percobaan
		FROM outbox_event
		WHERE status IN ('menunggu')
		  AND tersedia_pada <= NOW()
		ORDER BY tersedia_pada ASC, created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED;
	`, batchSize)
	if err != nil {
		return 0, 0, err
	}

	var events []EventRecord
	for rows.Next() {
		var ev EventRecord
		if err := rows.Scan(&ev.ID, &ev.KoperasiID, &ev.Topik, &ev.AgregatTipe, &ev.AgregatID, &ev.Payload, &ev.JumlahPercobaan); err != nil {
			rows.Close()
			return 0, 0, err
		}
		events = append(events, ev)
	}
	rows.Close()

	if len(events) == 0 {
		return 0, 0, nil
	}

	processed := 0
	failed := 0

	for _, ev := range events {
		// Kirim notifikasi via Notifier interface
		notifErr := w.notifier.SendNotification(ctx, ev.KoperasiID, ev.AgregatID.String(), ev.Topik, string(ev.Payload))
		if notifErr == nil {
			_, err = tx.Exec(ctx, `
				UPDATE outbox_event
				SET status = 'selesai',
				    diproses_pada = NOW()
				WHERE id = $1;
			`, ev.ID)
			if err != nil {
				return processed, failed, err
			}
			processed++
		} else {
			nextRetry := time.Now().Add(time.Duration(ev.JumlahPercobaan+1) * 2 * time.Minute)
			newStatus := "menunggu"
			if ev.JumlahPercobaan+1 >= 5 {
				newStatus = "gagal"
			}
			_, err = tx.Exec(ctx, `
				UPDATE outbox_event
				SET status = $1,
				    jumlah_percobaan = jumlah_percobaan + 1,
				    tersedia_pada = $2,
				    diproses_pada = NOW()
				WHERE id = $3;
			`, newStatus, nextRetry, ev.ID)
			if err != nil {
				return processed, failed, err
			}
			failed++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}

	return processed, failed, nil
}
