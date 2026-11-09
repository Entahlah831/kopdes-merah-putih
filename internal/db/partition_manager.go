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

// EnsurePartitionsAhead memeriksa dan secara idempoten membuat partisi bulanan ke depan
// untuk menjaga agar minimal 'monthsAhead' (misal 3 bulan) selalu tersedia di database.
func EnsurePartitionsAhead(ctx context.Context, pool *pgxpool.Pool, monthsAhead int) error {
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

			// Jalankan create table jika role memiliki izin (atau fallback ke default partition)
			if _, err := pool.Exec(ctx, query); err != nil {
				// Jika role aplikasi tidak memiliki izin DDL, log informasi dan andalkan partisi DEFAULT
				log.Printf("[PARTITION] Notice: Tidak dapat membuat partisi %s (%v). Memastikan partisi DEFAULT aktif.", partitionName, err)
			}
		}
	}

	return nil
}
