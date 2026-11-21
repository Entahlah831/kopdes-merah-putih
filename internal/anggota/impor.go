package anggota

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/auth"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/security"
)

var nikImportRe = regexp.MustCompile(`^[0-9]{16}$`)

type parsedCSVRow struct {
	Line         int
	NamaLengkap  string
	NIK          string
	NoHP         string
	TanggalLahir string
	Alamat       string
	Email        string
}

func parseCSVRows(csvData string) ([]parsedCSVRow, []string) {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(csvData)))
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return nil, []string{"Header CSV wajib tersedia (nama_lengkap,nik,no_hp,alamat)"}
	}
	colIdx := map[string]int{}
	for i, h := range header {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, req := range []string{"nama_lengkap", "nik", "no_hp", "alamat"} {
		if _, ok := colIdx[req]; !ok {
			return nil, []string{"Kolom CSV wajib hilang: " + req}
		}
	}
	return readAndValidateCSVBody(r, colIdx)
}

func readAndValidateCSVBody(r *csv.Reader, colIdx map[string]int) ([]parsedCSVRow, []string) {
	var valid []parsedCSVRow
	var errs []string
	seenNIK := map[string]bool{}
	line := 1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			errs = append(errs, fmt.Sprintf("baris %d: format CSV rusak", line))
			continue
		}
		row, rowErr := parseSingleCSVRow(rec, colIdx, line, seenNIK)
		if rowErr != "" {
			errs = append(errs, rowErr)
			continue
		}
		seenNIK[row.NIK] = true
		valid = append(valid, row)
		if len(valid) > 500 {
			errs = append(errs, "maksimal 500 baris per batch impor")
			break
		}
	}
	return valid, errs
}

func parseSingleCSVRow(rec []string, colIdx map[string]int, line int, seenNIK map[string]bool) (parsedCSVRow, string) {
	get := func(k string) string {
		if idx, ok := colIdx[k]; ok && idx < len(rec) {
			return strings.TrimSpace(rec[idx])
		}
		return ""
	}
	nik := get("nik")
	hp := security.NormalizePhone(get("no_hp"))
	nama := get("nama_lengkap")
	dob := get("tanggal_lahir")
	alamat := get("alamat")
	if nama == "" || !nikImportRe.MatchString(nik) || len(hp) < 9 || alamat == "" {
		return parsedCSVRow{}, fmt.Sprintf("baris %d: nama/NIK(16 digit)/no_hp/alamat tidak valid", line)
	}
	if dob != "" {
		if _, err := time.Parse("2006-01-02", dob); err != nil {
			return parsedCSVRow{}, fmt.Sprintf("baris %d: tanggal_lahir harus YYYY-MM-DD", line)
		}
	}
	if seenNIK[nik] {
		return parsedCSVRow{}, fmt.Sprintf("baris %d: NIK duplikat di dalam CSV", line)
	}
	return parsedCSVRow{
		Line: line, NamaLengkap: nama, NIK: nik, NoHP: hp,
		TanggalLahir: dob, Alamat: alamat, Email: strings.ToLower(get("email")),
	}, ""
}

func (s *Service) ImportCSV(ctx context.Context, claims *middleware.SessionClaims, csvData string, dryRun bool, ip string) (map[string]interface{}, error) {
	if s.cfg.KeyProvider == nil {
		return nil, response.NewDomainError(http.StatusInternalServerError, "KEY_PROVIDER_UNAVAILABLE", "Penyedia enkripsi tidak siap")
	}
	rows, parseErrs := parseCSVRows(csvData)
	q := db.New(s.pool)
	toCreate, skipped := s.filterExistingCSVRows(ctx, q, rows)

	if dryRun || len(parseErrs) > 0 {
		return map[string]interface{}{
			"dry_run": dryRun, "valid_new_rows": len(toCreate),
			"skipped_existing": skipped, "errors": parseErrs,
		}, nil
	}

	created, impErr := s.executeImportTx(ctx, claims, toCreate, ip)
	if impErr != nil {
		return nil, impErr
	}
	return map[string]interface{}{
		"dry_run": false, "imported": len(created), "skipped_existing": skipped,
		"errors": parseErrs, "members": created,
	}, nil
}

func (s *Service) filterExistingCSVRows(ctx context.Context, q *db.Queries, rows []parsedCSVRow) ([]parsedCSVRow, int) {
	var toCreate []parsedCSVRow
	skipped := 0
	for _, row := range rows {
		nikHash := security.HashBlindIndex(row.NIK, s.cfg.BlindIndexSecret)
		if _, err := q.GetAnggotaByNIKHash(ctx, nikHash); err == nil {
			skipped++
			continue
		}
		toCreate = append(toCreate, row)
	}
	return toCreate, skipped
}

func (s *Service) executeImportTx(ctx context.Context, claims *middleware.SessionClaims, rows []parsedCSVRow, ip string) ([]map[string]interface{}, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qt := db.New(tx)
	baseCount, _ := qt.CountTotalAnggota(ctx)
	created := make([]map[string]interface{}, 0, len(rows))
	for i, row := range rows {
		item, err := s.insertImportedMemberTx(ctx, tx, qt, claims, row, baseCount+int32(i+1)) // #nosec G115 -- dibatasi <= 500
		if err != nil {
			return nil, err
		}
		created = append(created, item)
	}
	_ = qt.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorUserID: response.ToPGUUID(claims.UserID), ActorPeran: claims.Role,
		Aksi: "impor_anggota_csv", Entitas: "anggota", EntitasID: "batch",
		Ringkasan: fmt.Sprintf("Impor CSV %d anggota baru", len(created)), IpAddress: ip,
	})
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Service) insertImportedMemberTx(ctx context.Context, tx pgx.Tx, qt *db.Queries, claims *middleware.SessionClaims, row parsedCSVRow, seq int32) (map[string]interface{}, error) {
	uid, _ := uuid.NewV7()
	if uid == uuid.Nil {
		uid = uuid.New()
	}
	var emailEnc []byte
	var emailHash pgtype.Text
	if row.Email != "" {
		emailHash = pgtype.Text{String: security.HashBlindIndex(row.Email, s.cfg.BlindIndexSecret), Valid: true}
		emailEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(row.Email), security.FormatAAD("users", "email", uid.String()))
	}
	uHP, _ := s.cfg.KeyProvider.EncryptWithAAD([]byte(row.NoHP), security.FormatAAD("users", "hp", uid.String()))
	uRow, err := qt.CreateUser(ctx, db.CreateUserParams{
		ID: response.ToPGUUID(uid), EmailEnc: emailEnc, EmailHash: emailHash,
		HpEnc: uHP, HpHash: pgtype.Text{String: security.HashBlindIndex(row.NoHP, s.cfg.BlindIndexSecret), Valid: true},
		NamaLengkap: row.NamaLengkap, PasswordHash: "", Peran: "anggota", StatusAktif: true,
	})
	if err != nil {
		return nil, response.NewDomainError(http.StatusConflict, "IMPORT_USER_CONFLICT", fmt.Sprintf("baris %d: email/no_hp sudah dipakai akun lain", row.Line))
	}

	aid, _ := uuid.NewV7()
	if aid == uuid.Nil {
		aid = uuid.New()
	}
	nomor := fmt.Sprintf("KMP-%s-%04d-%s", time.Now().Format("200601"), seq, strings.ToUpper(aid.String()[:4]))
	nikEnc, _ := s.cfg.KeyProvider.EncryptWithAAD([]byte(row.NIK), security.FormatAAD("anggota", "nik", aid.String()))
	hpEnc, _ := s.cfg.KeyProvider.EncryptWithAAD([]byte(row.NoHP), security.FormatAAD("anggota", "hp", aid.String()))
	var dobEnc []byte
	if row.TanggalLahir != "" {
		dobEnc, _ = s.cfg.KeyProvider.EncryptWithAAD([]byte(row.TanggalLahir), security.FormatAAD("anggota", "tanggal_lahir", aid.String()))
	}
	addrEnc, _ := s.cfg.KeyProvider.EncryptWithAAD([]byte(row.Alamat), security.FormatAAD("anggota", "alamat", aid.String()))

	ang, err := qt.CreateAnggota(ctx, db.CreateAnggotaParams{
		ID: response.ToPGUUID(aid), UserID: uRow.ID, NomorAnggota: nomor, NamaLengkap: row.NamaLengkap,
		NikEnc: nikEnc, NikHash: security.HashBlindIndex(row.NIK, s.cfg.BlindIndexSecret),
		NikKeyVersion: security.BlindIndexKeyVersion(s.cfg.BlindIndexSecret), NikMasked: security.MaskNIK(row.NIK),
		HpEnc: hpEnc, HpMasked: security.MaskPhone(row.NoHP), TanggalLahirEnc: dobEnc, AlamatEnc: addrEnc, Status: "aktif",
	})
	if err != nil {
		return nil, err
	}
	code, _, err := auth.CreateActivationOrResetCodeTx(ctx, tx, uRow.ID, response.ToPGUUID(claims.UserID), "aktivasi_awal", row.NoHP, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": aid, "nomor_anggota": ang.NomorAnggota, "nama_lengkap": ang.NamaLengkap, "activation_code": code}, nil
}
