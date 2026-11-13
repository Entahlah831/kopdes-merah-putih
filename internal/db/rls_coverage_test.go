package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getTestPoolForRLS(t testing.TB) *pgxpool.Pool {
	t.Helper()
	dsn, err := ResolveTestDatabaseDSN("", "")
	if err != nil {
		t.Fatalf("ResolveTestDatabaseDSN gagal: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig gagal: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE kopdes_app;")
		return err
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("gagal terkoneksi ke PostgreSQL test pool: %v", err)
	}
	if err := p.Ping(context.Background()); err != nil {
		p.Close()
		t.Fatalf("gagal ping PostgreSQL test pool: %v", err)
	}
	return p
}

func TestT4_RLS_AuditLog_And_JobLog_Isolation(t *testing.T) {
	pool := getTestPoolForRLS(t)
	defer pool.Close()

	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection error: %v", err)
	}
	defer conn.Release()

	tenantA := uuid.New()
	tenantB := uuid.New()
	wilayahID := uuid.New()
	adminUserID := uuid.New()

	// Seed wilayah dan koperasi untuk kedua tenant (sebagai platform admin)
	_, _ = conn.Exec(ctx, "RESET ROLE;")
	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'true', false);")
	kodeKem := fmt.Sprintf("%d", time.Now().UnixNano())
	if len(kodeKem) > 20 {
		kodeKem = kodeKem[:20]
	}
	_, _ = conn.Exec(ctx, `
		INSERT INTO wilayah (id, kode_kemendagri, kode_bps, nama, tingkat)
		VALUES ($1, $2, $2, 'Desa RLS Test', 'desa_kelurahan')
		ON CONFLICT (kode_kemendagri) DO NOTHING;
	`, wilayahID, kodeKem)

	_, err = conn.Exec(ctx, `
		INSERT INTO koperasi (id, kode_koperasi, nama, jenis_kopdes, wilayah_desa_id, alamat)
		VALUES
		  ($1, $2, 'Koperasi A RLS', 'desa', $3, 'Alamat A'),
		  ($4, $5, 'Koperasi B RLS', 'desa', $3, 'Alamat B');
	`, tenantA, "KOP-RLS-A-"+uuid.New().String()[:10], wilayahID,
		tenantB, "KOP-RLS-B-"+uuid.New().String()[:10])
	if err != nil {
		t.Fatalf("seed koperasi error: %v", err)
	}

	// Matikan platform admin flag untuk memulai pengujian isolasi murni antar tenant
	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false);")

	// Seed user untuk tenant A
	emailHash := fmt.Sprintf("%064x", time.Now().UnixNano())
	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, 'enk', $2, 'enk', $2, 'hash', 'Admin A');
	`, adminUserID, emailHash)
	if err != nil {
		t.Fatalf("seed user error: %v", err)
	}

	// 1. Tenant A membuat data audit_log dan job_log
	_, _ = conn.Exec(ctx, "SET ROLE kopdes_app;")
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", tenantA.String())

	auditRecordID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO audit_log (koperasi_id, user_id, nama_tabel, record_id, aksi, created_by)
		VALUES ($1, $2, 'anggota', $3, 'INSERT', $2);
	`, tenantA, adminUserID, auditRecordID)
	if err != nil {
		t.Fatalf("insert audit_log tenant A error: %v", err)
	}

	jobLogID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO job_log (id, koperasi_id, nama_job, kunci_idempoten, status, created_by)
		VALUES ($1, $2, 'sync_job', $3, 'selesai', $4);
	`, jobLogID, tenantA, "idempotent-key-"+uuid.New().String(), adminUserID)
	if err != nil {
		t.Fatalf("insert job_log tenant A error: %v", err)
	}

	// 2. Sekarang beralih ke Tenant B
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", tenantB.String())

	// Verifikasi audit_log: Tenant B TIDAK BOLEH bisa membaca audit_log milik Tenant A
	var auditCountB int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE record_id = $1", auditRecordID).Scan(&auditCountB)
	if err != nil {
		t.Fatalf("query audit_log tenant B error: %v", err)
	}
	if auditCountB > 0 {
		t.Fatalf("VULNERABILITY REPRODUCED: Tenant B can see audit_log of Tenant A! RLS not enforced on audit_log.")
	}

	// Verifikasi job_log: Tenant B TIDAK BOLEH bisa membaca job_log milik Tenant A
	var jobCountB int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM job_log WHERE id = $1", jobLogID).Scan(&jobCountB)
	if err != nil {
		t.Fatalf("query job_log tenant B error: %v", err)
	}
	if jobCountB > 0 {
		t.Fatalf("VULNERABILITY REPRODUCED: Tenant B can see job_log of Tenant A! RLS not enforced on job_log.")
	}
}
