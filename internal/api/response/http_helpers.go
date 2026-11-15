package response

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// DomainError merepresentasikan error bisnis yang aman dikembalikan ke klien HTTP.
type DomainError struct {
	Status  int
	Code    string
	Message string
	Details interface{}
}

func (e *DomainError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewDomainError membuat error bisnis dengan status HTTP dan kode error konsisten.
func NewDomainError(status int, code, message string, details ...interface{}) error {
	var det interface{}
	if len(details) > 0 {
		det = details[0]
	}
	return &DomainError{Status: status, Code: code, Message: message, Details: det}
}

// WriteServiceError memetakan error layanan ke respons JSON HTTP tanpa membocorkan detail internal.
func WriteServiceError(w http.ResponseWriter, err error) {
	var de *DomainError
	if errors.As(err, &de) {
		if de.Details != nil {
			Error(w, de.Status, de.Code, de.Message, de.Details)
			return
		}
		Error(w, de.Status, de.Code, de.Message)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, "Data tidak ditemukan")
		return
	}
	InternalServerError(w, "Terjadi kesalahan pada server")
}

// DecodeJSON mendekode body JSON dengan batas ukuran dan menolak JSON tidak valid.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	if err := dec.Decode(dst); err != nil {
		return NewDomainError(http.StatusBadRequest, "INVALID_JSON", "Format JSON request tidak valid")
	}
	return nil
}

// ToPGUUID mengonversi google/uuid ke pgtype.UUID valid.
func ToPGUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}

// PtrToPGUUID mengonversi pointer google/uuid opsional ke pgtype.UUID.
func PtrToPGUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return ToPGUUID(*id)
}

// FromPGUUID mengonversi pgtype.UUID valid menjadi google/uuid (atau uuid.Nil jika null).
func FromPGUUID(u pgtype.UUID) uuid.UUID {
	if !u.Valid {
		return uuid.Nil
	}
	return uuid.UUID(u.Bytes)
}

// ParsePGUUID mem-parsing string UUID menjadi pgtype.UUID.
func ParsePGUUID(raw string) (pgtype.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return pgtype.UUID{}, NewDomainError(http.StatusBadRequest, "INVALID_UUID", "Format ID UUID tidak valid")
	}
	return ToPGUUID(id), nil
}

func ToPGText(s string) pgtype.Text {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func ToPGInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func ToPGTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func ToPGDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true}
}

// ParsePagination membaca query limit dan offset dengan batas aman.
func ParsePagination(r *http.Request, defLim, maxLim int) (int32, int32) {
	if defLim <= 0 {
		defLim = 20
	}
	if maxLim < defLim {
		maxLim = 100
	}
	lim := defLim
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		lim = v
	}
	if lim > maxLim {
		lim = maxLim
	}
	off := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		off = v
	}
	return int32(lim), int32(off) // #nosec G115 -- dibatasi maxLim <= 100 dan off >= 0
}
