package db

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
)

// StartPDPPurgeWorker menjalankan scheduled job pembersihan berkas pendaftaran ditolak secara berkala
func StartPDPPurgeWorker(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	go func() {
		log.Printf("[PDP-PURGE-WORKER] Memulai runner pembersihan PDP berkala (interval: %v)", interval)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		runPurge := func() {
			runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()

			// Ambil daftar koperasi aktif
			rows, err := pool.Query(runCtx, "SELECT id FROM koperasi WHERE status_aktif = true AND deleted_at IS NULL;")
			if err != nil {
				log.Printf("[PDP-PURGE-WORKER] Gagal mengambil daftar koperasi: %v", err)
				return
			}
			defer rows.Close()

			var koperasiIDs []uuid.UUID
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err == nil {
					koperasiIDs = append(koperasiIDs, id)
				}
			}

			for _, kopID := range koperasiIDs {
				var purged int
				err := middleware.WithTenantTx(runCtx, pool, kopID, false, func(tx pgx.Tx) error {
					return tx.QueryRow(runCtx, "SELECT fn_purge_rejected_pendaftaran_pdp($1, NULL);", kopID).Scan(&purged)
				})
				if err != nil {
					log.Printf("[PDP-PURGE-WORKER] Error pembersihan PDP tenant %s: %v", kopID, err)
				} else if purged > 0 {
					log.Printf("[PDP-PURGE-WORKER] Tenant %s: berhasil membersihkan %d berkas pendaftaran ditolak.", kopID, purged)
				}
			}
		}

		// Jalankan sekali saat startup
		runPurge()

		for {
			select {
			case <-ctx.Done():
				log.Println("[PDP-PURGE-WORKER] Menghentikan runner pembersihan PDP.")
				return
			case <-ticker.C:
				runPurge()
			}
		}
	}()
}
