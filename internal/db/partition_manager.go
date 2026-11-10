package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var PartitionedTables = []string{
	"audit_log",
	"stok_mutasi",
	"kas_mutasi",
	"notifikasi",
}

// PartitionMetrics memuat data pemantauan kesehatan partisi tabel
type PartitionMetrics struct {
	MonthsRemaining       map[string]int
	MinMonthsRemaining    int
	AuditLogDefaultRows   int64
	KasMutasiDefaultRows  int64
	StokMutasiDefaultRows int64
	NotifikasiDefaultRows int64
	AlertActive           bool
}

// EnsurePartitionsAhead memeriksa dan secara idempoten membuat partisi bulanan ke depan
// dengan memanggil fungsi SECURITY DEFINER fn_ensure_future_partitions di database.
func EnsurePartitionsAhead(ctx context.Context, pool *pgxpool.Pool, monthsAhead int) error {
	if monthsAhead < 1 {
		monthsAhead = 3
	}

	// 1. Coba panggil fungsi SQL idempoten tersentralisasi di PostgreSQL
	var resultJSON string
	err := pool.QueryRow(ctx, "SELECT fn_ensure_future_partitions($1)::text;", monthsAhead).Scan(&resultJSON)
	if err == nil {
		log.Printf("[PARTITION] Sukses memastikan partisi %d bulan ke depan: %s", monthsAhead, resultJSON)
		return nil
	}

	// 2. Fallback: Buat partisi langsung per tabel jika fungsi SQL belum termigrasi
	log.Printf("[PARTITION] Notice: fn_ensure_future_partitions gagal/belum ada (%v), menjalankan fallback DDL.", err)
	now := time.Now().UTC()
	for _, table := range PartitionedTables {
		for i := 0; i <= monthsAhead; i++ {
			targetMonth := now.AddDate(0, i, 0)
			year := targetMonth.Year()
			month := int(targetMonth.Month())

			startRange := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
			endRange := startRange.AddDate(0, 1, 0)

			partitionName := fmt.Sprintf("%s_y%04dm%02d", table, year, month)
			startStr := startRange.Format("2006-01-02 15:04:05-07")
			endStr := endRange.Format("2006-01-02 15:04:05-07")

			query := fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s');",
				partitionName, table, startStr, endStr,
			)

			if _, execErr := pool.Exec(ctx, query); execErr != nil {
				log.Printf("[PARTITION] Notice: Fallback tidak dapat membuat partisi %s: %v", partitionName, execErr)
			}
		}
	}

	return nil
}

// GetPartitionMetrics mengambil data statistik partisi dari katalog PostgreSQL
func GetPartitionMetrics(ctx context.Context, pool *pgxpool.Pool) (PartitionMetrics, error) {
	metrics := PartitionMetrics{
		MonthsRemaining:    make(map[string]int),
		MinMonthsRemaining: 9999,
	}

	// 1. Hitung jumlah baris di partisi DEFAULT (harus 0)
	err := pool.QueryRow(ctx, `
		SELECT 
			(SELECT COUNT(*) FROM audit_log_default),
			(SELECT COUNT(*) FROM kas_mutasi_default),
			(SELECT COUNT(*) FROM stok_mutasi_default),
			(SELECT COUNT(*) FROM notifikasi_default);
	`).Scan(
		&metrics.AuditLogDefaultRows,
		&metrics.KasMutasiDefaultRows,
		&metrics.StokMutasiDefaultRows,
		&metrics.NotifikasiDefaultRows,
	)
	if err != nil {
		return metrics, fmt.Errorf("gagal menghitung baris partisi default: %w", err)
	}

	// 2. Hitung bulan tersisa untuk setiap tabel berpartisi
	rows, err := pool.Query(ctx, `
		SELECT 
			p.inhparent::regclass::text as parent_table, 
			(EXTRACT(YEAR FROM age(to_date(MAX(substring(c.relname from '_y([0-9]{4}m[0-9]{2})$')), 'YYYYmMM') + interval '1 month', clock_timestamp())) * 12 + 
			 EXTRACT(MONTH FROM age(to_date(MAX(substring(c.relname from '_y([0-9]{4}m[0-9]{2})$')), 'YYYYmMM') + interval '1 month', clock_timestamp())))::int as bulan_tersisa 
		FROM pg_class c 
		JOIN pg_inherits p ON p.inhrelid = c.oid 
		WHERE p.inhparent IN ('audit_log'::regclass, 'kas_mutasi'::regclass, 'stok_mutasi'::regclass, 'notifikasi'::regclass) 
		  AND c.relname ~ '_y[0-9]{4}m[0-9]{2}$' 
		GROUP BY p.inhparent;
	`)
	if err != nil {
		return metrics, fmt.Errorf("gagal menghitung bulan tersisa partisi: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tbl string
		var remaining int
		if err := rows.Scan(&tbl, &remaining); err == nil {
			metrics.MonthsRemaining[tbl] = remaining
			if remaining < metrics.MinMonthsRemaining {
				metrics.MinMonthsRemaining = remaining
			}
		}
	}

	if metrics.MinMonthsRemaining == 9999 {
		metrics.MinMonthsRemaining = 0
	}

	// Peringatan aktif jika partisi default terisi data (> 0) atau sisa partisi kurang dari 2 bulan
	totalDefaultRows := metrics.AuditLogDefaultRows + metrics.KasMutasiDefaultRows +
		metrics.StokMutasiDefaultRows + metrics.NotifikasiDefaultRows
	if totalDefaultRows > 0 || metrics.MinMonthsRemaining < 2 {
		metrics.AlertActive = true
	}

	return metrics, nil
}

// StartPartitionMaintenanceJob memulai background worker terjadwal untuk menjaga partisi selalu siap
func StartPartitionMaintenanceJob(ctx context.Context, pool *pgxpool.Pool, interval time.Duration, monthsAhead int) {
	go func() {
		log.Printf("[PARTITION-WORKER] Memulai runner pemeliharaan partisi (interval: %v, target: %d bulan ke depan)", interval, monthsAhead)

		// Jalankan sekali saat startup
		runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_ = EnsurePartitionsAhead(runCtx, pool, monthsAhead)
		cancel()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("[PARTITION-WORKER] Menghentikan runner pemeliharaan partisi.")
				return
			case <-ticker.C:
				mCtx, mCancel := context.WithTimeout(ctx, 30*time.Second)
				if err := EnsurePartitionsAhead(mCtx, pool, monthsAhead); err != nil {
					log.Printf("[PARTITION-WORKER] Gagal dalam pemeliharaan partisi terjadwal: %v", err)
				}
				mCancel()
			}
		}
	}()
}
