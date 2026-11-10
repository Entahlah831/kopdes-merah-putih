package simkopdes

import (
	"context"
	"errors"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSIMKOPDESDeScoped menandakan pelaporan nasional SIMKOPDES ditangguhkan untuk implementasi cabang tunggal lokal
var ErrSIMKOPDESDeScoped = errors.New("integrasi SIMKOPDES ditangguhkan: sistem difokuskan pada satu cabang lokal koperasi (adapter stub PERLU VERIFIKASI)")

// SyncAdapter mendefinisikan kontrak adapter sinkronisasi ke SIMKOPDES Kemenkop
type SyncAdapter interface {
	EnqueueReport(ctx context.Context, koperasiID uuid.UUID, jenisPayload string, payload map[string]interface{}) (uuid.UUID, error)
	ProcessQueue(ctx context.Context) error
}

// StubAdapter adalah implementasi stub yang aman dan memenuhi kontrak arsitektur
type StubAdapter struct {
	pool *pgxpool.Pool
}

func NewStubAdapter(pool *pgxpool.Pool) *StubAdapter {
	return &StubAdapter{pool: pool}
}

func (s *StubAdapter) EnqueueReport(ctx context.Context, koperasiID uuid.UUID, jenisPayload string, payload map[string]interface{}) (uuid.UUID, error) {
	log.Printf("[SIMKOPDES-STUB] Pelaporan jenis %s untuk tenant %s dicatat pada stub adapter (de-scoped).", jenisPayload, koperasiID)
	return uuid.Nil, ErrSIMKOPDESDeScoped
}

func (s *StubAdapter) ProcessQueue(ctx context.Context) error {
	// Stub tidak mengirim payload ke server nasional SIMKOPDES
	return nil
}
