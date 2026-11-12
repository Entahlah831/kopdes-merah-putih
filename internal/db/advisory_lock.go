package db

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CanonicalBaseTable menormalisasi nama tabel partisi ke tabel dasar
func CanonicalBaseTable(t string) string {
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "audit_log") {
		return "audit_log"
	}
	if strings.HasPrefix(t, "kas_mutasi") {
		return "kas_mutasi"
	}
	if strings.HasPrefix(t, "stok_mutasi") {
		return "stok_mutasi"
	}
	return t
}

// TableAdvisoryLockQuery mengembalikan query SQL untuk mengambil pg_advisory_xact_lock
// berdasarkan md5(base_table || ':' || koperasi_id) 64-bit bigint, sinkron dengan trigger hash chain.
func TableAdvisoryLockQuery() string {
	return `SELECT pg_advisory_xact_lock(('x' || substr(md5($1 || ':' || $2::text), 1, 16))::bit(64)::bigint);`
}

// AcquireOrderedAdvisoryLocks mengambil seluruh transactional advisory lock untuk tabel-tabel target
// dalam urutan kanonikal (alfabetis terurut dan terdeduplikasi) di awal transaksi pgx.Tx.
// Ini menjamin kebebasan dari deadlock (anti-circular wait) antar transaksi konkuren
// yang memodifikasi tabel ledger append-only (audit_log, kas_mutasi, stok_mutasi, dll).
func AcquireOrderedAdvisoryLocks(ctx context.Context, tx pgx.Tx, koperasiID uuid.UUID, tables ...string) ([]string, error) {
	if len(tables) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	var uniqueTables []string
	for _, t := range tables {
		base := CanonicalBaseTable(t)
		if base != "" && !seen[base] {
			seen[base] = true
			uniqueTables = append(uniqueTables, base)
		}
	}

	// Urutkan secara leksikografis (deterministik kanonikal)
	sort.Strings(uniqueTables)

	lockQuery := TableAdvisoryLockQuery()
	for _, tbl := range uniqueTables {
		if _, err := tx.Exec(ctx, lockQuery, tbl, koperasiID.String()); err != nil {
			return nil, fmt.Errorf("gagal mengunci advisory lock kanonikal untuk tabel %s: %w", tbl, err)
		}
	}

	return uniqueTables, nil
}
