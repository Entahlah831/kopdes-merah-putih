package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrIdempotencyConflict terjadi saat Idempotency-Key sama dikirimkan dengan payload berbeda
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request payload")
)

type IdempotencyRecord struct {
	ID           uuid.UUID
	KoperasiID   uuid.UUID
	UserID       uuid.UUID
	Kunci        string
	RequestHash  string
	ResponseCode int
	ResponseBody json.RawMessage
	ExpiresAt    time.Time
}

// HashPayload menghitung hash SHA-256 dari byte body request
func HashPayload(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// CheckIdempotency memeriksa apakah kunci idempotensi sudah ada di tabel database:
// - Mengembalikan (nil, nil) jika belum pernah digunakan atau masa berlakunya habis.
// - Mengembalikan (record, nil) jika sudah pernah digunakan dengan request payload persis sama.
// - Mengembalikan (nil, ErrIdempotencyConflict) jika kunci sama dikirimkan dengan body berbeda (HTTP 422).
func CheckIdempotency(ctx context.Context, pool *pgxpool.Pool, koperasiID, userID uuid.UUID, key, reqHash string) (*IdempotencyRecord, error) {
	if key == "" {
		return nil, nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Pastikan sesi RLS PostgreSQL terikat ke tenant yang sesuai
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true);", koperasiID.String()); err != nil {
		return nil, err
	}

	var rec IdempotencyRecord
	var bodyBytes []byte
	err = tx.QueryRow(ctx, `
		SELECT id, koperasi_id, user_id, kunci, request_hash, response_code, response_body, expires_at
		FROM idempotency_keys
		WHERE koperasi_id = $1 AND user_id = $2 AND kunci = $3 AND expires_at > NOW();
	`, koperasiID, userID, key).Scan(
		&rec.ID, &rec.KoperasiID, &rec.UserID, &rec.Kunci,
		&rec.RequestHash, &rec.ResponseCode, &bodyBytes, &rec.ExpiresAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if rec.RequestHash != reqHash {
		return nil, ErrIdempotencyConflict
	}

	rec.ResponseBody = bodyBytes
	return &rec, nil
}

// SaveIdempotency menyimpan atau memperbarui entri idempotensi di database
func SaveIdempotency(ctx context.Context, pool *pgxpool.Pool, koperasiID, userID uuid.UUID, key, reqHash string, code int, respBody []byte, ttl time.Duration) error {
	if key == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	expiresAt := time.Now().Add(ttl)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true);", koperasiID.String()); err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_keys (
			id, koperasi_id, user_id, kunci, request_hash, response_code, response_body, expires_at, created_at
		) VALUES (
			uuid_generate_v7(), $1, $2, $3, $4, $5, $6, $7, NOW()
		)
		ON CONFLICT (koperasi_id, user_id, kunci) DO UPDATE SET
			request_hash = EXCLUDED.request_hash,
			response_code = EXCLUDED.response_code,
			response_body = EXCLUDED.response_body,
			expires_at = EXCLUDED.expires_at,
			created_at = NOW();
	`, koperasiID, userID, key, reqHash, code, respBody, expiresAt)

	if err != nil {
		log.Printf("[IDEMPOTENCY] Error saving idempotency key %s: %v", key, err)
		return err
	}

	return tx.Commit(ctx)
}
