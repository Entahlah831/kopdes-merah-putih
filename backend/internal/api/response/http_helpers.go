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
	"github.com/jackc/pgx/v5/pgconn"
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
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && writePGError(w, pgErr) {
		return
	}
	InternalServerError(w, "Terjadi kesalahan pada server")
}

func writePGError(w http.ResponseWriter, pgErr *pgconn.PgError) bool {
	switch pgErr.Code {
	case "23505":
		Conflict(w, "Data sudah terdaftar atau bentrok dengan entri lain")
		return true
	case "22001", "22003", "22007", "22008", "2201W", "2201X", "22021", "22P02", "23502", "23503", "23514":
		BadRequest(w, "Masukan tidak memenuhi format atau batasan skema")
		return true
	default:
		return false
	}
}

// DecodeJSON mendekode body JSON dengan batas ukuran, melarang field tak dikenal (anti-mass-assignment),
// serta menolak payload trailing di luar satu objek JSON.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return NewDomainError(http.StatusBadRequest, "INVALID_JSON", "Format JSON tidak valid atau memuat field tidak dikenal")
	}
	var trailing interface{}
	if err := dec.Decode(&trailing); err != io.EOF {
		return NewDomainError(http.StatusBadRequest, "INVALID_JSON", "Body JSON hanya boleh berisi satu objek tunggal")
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

const maxPaginationOffset int64 = 1_000_000

// ParsePagination membaca query limit dan offset dengan batas aman anti-overflow int32.
func ParsePagination(r *http.Request, defLim, maxLim int) (int32, int32) {
	if defLim <= 0 || defLim > 1000 {
		defLim = 20
	}
	if maxLim < defLim || maxLim > 1000 {
		maxLim = max(defLim, 100)
	}
	lim := int64(defLim)
	if v, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("limit")), 10, 64); err == nil && v > 0 {
		lim = min(v, int64(maxLim))
	}
	var off int64
	if v, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("offset")), 10, 64); err == nil && v >= 0 {
		off = min(v, maxPaginationOffset)
	}
	return int32(lim), int32(off) // #nosec G115 -- dibatasi eksplisit 1<=lim<=1000 dan 0<=off<=1_000_000
}
