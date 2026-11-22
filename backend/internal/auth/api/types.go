package api

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateMemberUserInput mendefinisikan data akun baru untuk anggota terverifikasi/impor.
type CreateMemberUserInput struct {
	Email       string
	NoHP        string
	NamaLengkap string
	Peran       string
}

// MemberUserCreated berisi ringkasan user baru yang berhasil dibuat dalam transaksi.
type MemberUserCreated struct {
	UserID      uuid.UUID
	NamaLengkap string
}

// ActivationCodeResult berisi kode aktivasi sekali pakai dan tautan WhatsApp siap kirim.
type ActivationCodeResult struct {
	Code         string
	WhatsAppLink string
}

// UserManager menyediakan operasi siklus akun anggota dan kode aktivasi lintas modul via tx yang sama.
type UserManager interface {
	CreateMemberUserTx(ctx context.Context, tx pgx.Tx, in CreateMemberUserInput) (MemberUserCreated, error)
	CreateActivationOrResetCodeTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, actorUserID *uuid.UUID, jenis, noHP string, ttl time.Duration) (ActivationCodeResult, error)
	SetUserActiveAndRevokeTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, active bool) error
	GetUserRoleByID(ctx context.Context, userID uuid.UUID) (string, error)
}

// BreakglassTOTPVerifier memverifikasi kode TOTP admin dengan anti-replay dalam tx yang sama.
type BreakglassTOTPVerifier interface {
	VerifyBreakglassTOTP(ctx context.Context, tx pgx.Tx, userID uuid.UUID, code string) error
}

// MaintenanceCleaner membersihkan rate limit dan sesi kedaluwarsa secara berkala oleh worker.
type MaintenanceCleaner interface {
	CleanupExpiredRateLimits(ctx context.Context) (int64, error)
	CleanupExpiredSessions(ctx context.Context) (int64, error)
}
