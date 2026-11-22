package api

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Settings berisi konfigurasi koperasi satu baris yang dikelola modul pengaturan.
type Settings struct {
	NamaKoperasi         string
	AlamatKoperasi       string
	TeleponKoperasi      string
	QRISUniqueDigits     int32
	OngkirTetap          int64
	KebijakanTeks        string
	CatatanAkuntansi     string
	ActiveRekeningQRISID *uuid.UUID
	UpdatedAt            time.Time
}

// SettingsReader menyediakan pembacaan pengaturan koperasi bagi modul lain.
type SettingsReader interface {
	GetSettings(ctx context.Context) (Settings, error)
	GetSettingsTx(ctx context.Context, tx pgx.Tx) (Settings, error)
}

// AuditEntry mendeskripsikan satu baris catatan audit non-PII lintas modul.
type AuditEntry struct {
	ActorUserID *uuid.UUID
	ActorPeran  string
	Aksi        string
	Entitas     string
	EntitasID   string
	Ringkasan   string
	IPAddress   string
}

// AuditWriter menuliskan audit_log atas nama modul pemanggil baik autocommit maupun dalam tx yang sama.
type AuditWriter interface {
	Log(ctx context.Context, entry AuditEntry) error
	LogTx(ctx context.Context, tx pgx.Tx, entry AuditEntry) error
}
