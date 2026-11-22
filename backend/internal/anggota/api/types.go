package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound menandakan data anggota tidak ditemukan.
var ErrNotFound = errors.New("anggota tidak ditemukan")

// MemberInfo berisi identitas dasar anggota terhubung ke user.
type MemberInfo struct {
	ID               uuid.UUID
	UserID           *uuid.UUID
	NomorAnggota     string
	NamaLengkap      string
	Status           string
	TanggalBergabung time.Time
}

// MemberSummary berisi ringkasan non-PII anggota untuk pengayaan daftar lintas modul.
type MemberSummary struct {
	ID           uuid.UUID
	UserID       *uuid.UUID
	NomorAnggota string
	NamaLengkap  string
	Status       string
}

// MemberStatusCount merepresentasikan rekap jumlah anggota per status.
type MemberStatusCount struct {
	Status string `json:"status"`
	Jumlah int64  `json:"jumlah"`
}

// MemberReader menyediakan lookup anggota read-only bagi auth, iuran, katalog, pesanan, dan pembayaran.
type MemberReader interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (MemberInfo, error)
	LookupByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]MemberSummary, error)
	GetSummariesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]MemberSummary, error)
	ListActiveMemberIDs(ctx context.Context) ([]uuid.UUID, error)
}

// ReportReader menyediakan laporan ringkasan keanggotaan bagi modul laporan.
type ReportReader interface {
	GetMemberSummary(ctx context.Context) ([]MemberStatusCount, error)
}
