package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/security"
)

func getTestPool(t testing.TB) *pgxpool.Pool {
	candidates := []string{}
	if envDSN := os.Getenv("DATABASE_URL"); envDSN != "" {
		candidates = append(candidates, envDSN)
	}
	if envDSN := os.Getenv("POSTGRES_DSN"); envDSN != "" {
		candidates = append(candidates, envDSN)
	}
	candidates = append(candidates,
		"postgres://kopdes_app:kopdes_dev_password@127.0.0.1:5433/kopdes_db?sslmode=disable",
		"postgres://postgres:kopdes_dev_password@127.0.0.1:5433/kopdes_db?sslmode=disable",
		"postgres://kopdes_app:kopdes_dev_password@127.0.0.1:5432/kopdes_db?sslmode=disable",
	)

	for _, dsn := range candidates {
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			continue
		}
		cfg.MaxConns = 20
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET ROLE kopdes_app;")
			return err
		}

		p, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			continue
		}

		if err := p.Ping(context.Background()); err == nil {
			return p
		}
		p.Close()
	}

	t.Fatalf("gagal terkoneksi ke PostgreSQL database pengujian")
	return nil
}

func hashSHA256(val string) string {
	sum := sha256.Sum256([]byte(val))
	return hex.EncodeToString(sum[:])
}

type TestEnv struct {
	Server        *api.Server
	Pool          *pgxpool.Pool
	Config        api.Config
	KoperasiID    uuid.UUID
	AdminUserID   uuid.UUID
	AnggotaUserID uuid.UUID
	AnggotaID     uuid.UUID
	RekeningID    uuid.UUID
	MetodeID      uuid.UUID
	AdminToken    string
	AnggotaToken  string
}

func setupAPITestEnv(t testing.TB) *TestEnv {
	pool := getTestPool(t)
	cfg := api.DefaultConfig()
	cfg.JWTSecret = "test-jwt-secret-key-32-bytes-minimum-ok!"
	cfg.BlindIndexSecret = "test-blind-index-secret-32-bytes!"
	cfg.QRISSecret = "test-qris-linear-probing-secret-32b!"

	server := api.NewServer(pool, cfg)

	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")

	wilayahID := uuid.New()
	koperasiID := uuid.New()
	adminUserID := uuid.New()
	anggotaUserID := uuid.New()
	anggotaID := uuid.New()
	penyediaID := uuid.New()
	metodeID := uuid.New()
	rekeningID := uuid.New()

	kodeWil := fmt.Sprintf("%d", time.Now().UnixNano())
	if len(kodeWil) > 20 {
		kodeWil = kodeWil[:20]
	}
	_, err = conn.Exec(ctx, `
		INSERT INTO wilayah (id, kode_kemendagri, kode_bps, nama, tingkat)
		VALUES ($1, $2, $2, 'Desa API Test', 'desa_kelurahan')
		ON CONFLICT (kode_kemendagri) DO NOTHING;
	`, wilayahID, kodeWil)
	if err != nil {
		t.Fatalf("seed wilayah error: %v", err)
	}

	kodeKop := fmt.Sprintf("KOP-API-%s", uuid.New().String()[:18])
	_, err = conn.Exec(ctx, `
		INSERT INTO koperasi (id, kode_koperasi, nama, jenis_kopdes, wilayah_desa_id, alamat)
		VALUES ($1, $2, 'Koperasi API Test', 'desa', $3, 'Jl. API Test');
	`, koperasiID, kodeKop, wilayahID)
	if err != nil {
		t.Fatalf("seed koperasi error: %v", err)
	}

	adminEmail := fmt.Sprintf("admin_%s@kopdes.id", uuid.New().String()[:8])
	adminHash := security.HashBlindIndex(adminEmail, []byte(cfg.BlindIndexSecret))
	adminPwdHash, _ := security.HashPassword("AdminPass123!")

	anggotaEmail := fmt.Sprintf("anggota_%s@kopdes.id", uuid.New().String()[:8])
	anggotaHash := security.HashBlindIndex(anggotaEmail, []byte(cfg.BlindIndexSecret))
	anggotaPwdHash, _ := security.HashPassword("AnggotaPass123!")

	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES
		  ($1, $2, $3, $2, $3, $4, 'Admin Test'),
		  ($5, $2, $6, $2, $6, $7, 'Anggota Test');
	`, adminUserID, []byte("enkripsi"), adminHash, adminPwdHash,
		anggotaUserID, anggotaHash, anggotaPwdHash)
	if err != nil {
		t.Fatalf("seed users error: %v", err)
	}

	// Pastikan roles dasar tersedia
	_, err = conn.Exec(ctx, `
		INSERT INTO roles (id, kode, nama, deskripsi)
		VALUES
		  ('00000000-0000-0000-0000-000000000010', 'admin', 'admin', 'Administrator'),
		  ('00000000-0000-0000-0000-000000000011', 'pengurus', 'pengurus', 'Pengurus'),
		  ('00000000-0000-0000-0000-000000000012', 'operator', 'operator', 'Operator'),
		  ('00000000-0000-0000-0000-000000000013', 'anggota', 'anggota', 'Anggota')
		ON CONFLICT (kode) DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("seed roles error: %v", err)
	}

	var roleAdminID, roleAnggotaID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'admin'").Scan(&roleAdminID)
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'anggota'").Scan(&roleAnggotaID)

	if roleAdminID != uuid.Nil && roleAnggotaID != uuid.Nil {
		_, _ = conn.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id)
			VALUES ($1, $2, $3), ($4, $5, $3)
			ON CONFLICT (koperasi_id, user_id, role_id) DO NOTHING;
		`, adminUserID, roleAdminID, koperasiID, anggotaUserID, roleAnggotaID)
	}

	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", koperasiID.String())

	// Anggota
	nik := fmt.Sprintf("3201%012d", time.Now().UnixNano()%1000000000000)
	_, err = conn.Exec(ctx, `
		INSERT INTO anggota (id, koperasi_id, user_id, no_anggota, nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir, tanggal_lahir_enkripsi, jenis_kelamin, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'Anggota Test', 'Bogor', $5, 'L', 'aktif', $7);
	`, anggotaID, koperasiID, anggotaUserID, "ANG-"+uuid.New().String()[:8], []byte("enkripsi"), hashSHA256(nik), adminUserID)
	if err != nil {
		t.Fatalf("seed anggota error: %v", err)
	}

	// Penyedia, Metode, Rekening QRIS
	kodePenyedia := fmt.Sprintf("ASPI_%s", uuid.New().String()[:8])
	_, _ = conn.Exec(ctx, `
		INSERT INTO penyedia_pembayaran (id, kode, nama, tipe_integrasi)
		VALUES ($1, $2, 'PJP QRIS', 'qris_statis_manual')
		ON CONFLICT (kode) DO NOTHING;
	`, penyediaID, kodePenyedia)

	_, err = conn.Exec(ctx, `
		INSERT INTO metode_pembayaran (id, koperasi_id, penyedia_id, kode, nama, butuh_kode_unik, butuh_verifikasi_manual)
		VALUES ($1, $2, $3, $4, 'QRIS Test', true, true);
	`, metodeID, koperasiID, penyediaID, "QRIS-"+uuid.New().String()[:6])
	if err != nil {
		t.Fatalf("seed metode error: %v", err)
	}

	nmid := fmt.Sprintf("ID%013d", time.Now().UnixNano()%10000000000000)
	_, err = conn.Exec(ctx, `
		INSERT INTO rekening_qris_koperasi (
			id, koperasi_id, penyedia_id, nama_merchant, nmid,
			nama_bank_pjp, no_rekening_settlement, nama_pemilik_rekening,
			is_atas_nama_badan_hukum, qris_payload_statis, created_by
		) VALUES (
			$1, $2, $3, 'Merchant API Test', $4,
			'Bank Mandiri', '1112223334', 'Koperasi API',
			true, '00020101021126570011ID.CO.QRIS.WWW...', $5
		);
	`, rekeningID, koperasiID, penyediaID, nmid, adminUserID)
	if err != nil {
		t.Fatalf("seed rekening QRIS error: %v", err)
	}

	_, _ = conn.Exec(ctx, "SET ROLE kopdes_app;")

	// Generate JWT tokens
	adminTok, _ := server.Router().(interface{
		// Token manager is internal to server, but we can do login via HTTP to test!
	})
	_ = adminTok

	return &TestEnv{
		Server:        server,
		Pool:          pool,
		Config:        cfg,
		KoperasiID:    koperasiID,
		AdminUserID:   adminUserID,
		AnggotaUserID: anggotaUserID,
		AnggotaID:     anggotaID,
		RekeningID:    rekeningID,
		MetodeID:      metodeID,
	}
}

func TestAPI_HealthAndSecurityHeaders(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// 1. Test /healthz
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for /healthz, got %d", rec.Code)
	}

	// 2. Check Security Headers
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Missing X-Content-Type-Options: nosniff header")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("Missing X-Frame-Options: DENY header")
	}

	// 3. Test /readyz
	reqReady := httptest.NewRequest("GET", "/readyz", nil)
	recReady := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recReady, reqReady)

	if recReady.Code != http.StatusOK {
		t.Errorf("Expected 200 for /readyz, got %d", recReady.Code)
	}
}

func TestAPI_CORS_Preflight(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	req := httptest.NewRequest("OPTIONS", "/api/v1/pembayaran/qris", nil)
	req.Header.Set("Origin", "https://frontend.kopdes.id")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")

	rec := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 No Content for CORS preflight, got %d", rec.Code)
	}

	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Errorf("Missing Access-Control-Allow-Origin header in preflight response")
	}
}

func TestAPI_Auth_LoginAndProfile(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// Seed satu user dengan password known
	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")

	email := fmt.Sprintf("test_auth_%s@kopdes.id", uuid.New().String()[:8])
	emailHash := security.HashBlindIndex(email, []byte(env.Config.BlindIndexSecret))
	pwdPlain := "SangatRahasia123!"
	pwdHash, _ := security.HashPassword(pwdPlain)
	testUserID := uuid.New()

	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'Budi Utomo');
	`, testUserID, []byte("enkripsi"), emailHash, pwdHash)
	if err != nil {
		t.Fatalf("seed user error: %v", err)
	}

	_, _ = conn.Exec(ctx, "SET ROLE kopdes_app;")

	// 1. Test Login dengan password salah -> 401
	bodyWrong, _ := json.Marshal(handlers.LoginRequest{
		Email:    email,
		Password: "PasswordSalahBro!",
	})
	reqWrong := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyWrong))
	recWrong := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recWrong, reqWrong)

	if recWrong.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for wrong password, got %d", recWrong.Code)
	}

	// 2. Test Login sukses -> 200 OK & ambil token
	bodyOk, _ := json.Marshal(handlers.LoginRequest{
		Email:    email,
		Password: pwdPlain,
	})
	reqOk := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyOk))
	recOk := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recOk, reqOk)

	if recOk.Code != http.StatusOK {
		t.Fatalf("Expected 200 for successful login, got %d (body: %s)", recOk.Code, recOk.Body.String())
	}

	var res struct {
		Success bool                   `json:"success"`
		Data    handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recOk.Body.Bytes(), &res)

	if res.Data.AccessToken == "" {
		t.Fatalf("Access token tidak ditemukan dalam respons login")
	}

	// 3. Test /api/v1/auth/me dengan Bearer token
	reqMe := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+res.Data.AccessToken)
	recMe := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recMe, reqMe)

	if recMe.Code != http.StatusOK {
		t.Errorf("Expected 200 for /auth/me, got %d", recMe.Code)
	}
}

func TestAPI_QRIS_CreateAndCheckStatus(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// Login sebagai user anggota
	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")

	email := fmt.Sprintf("qris_user_%s@kopdes.id", uuid.New().String()[:8])
	emailHash := security.HashBlindIndex(email, []byte(env.Config.BlindIndexSecret))
	pwdPlain := "PasswordQRIS123!"
	pwdHash, _ := security.HashPassword(pwdPlain)
	testUserID := uuid.New()

	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'Penyetor QRIS');
	`, testUserID, []byte("enkripsi"), emailHash, pwdHash)
	if err != nil {
		t.Fatalf("seed user error: %v", err)
	}

	// Kaitkan user ke Koperasi
	var roleAnggotaID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE nama = 'anggota'").Scan(&roleAnggotaID)
	if roleAnggotaID != uuid.Nil {
		_, _ = conn.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id)
			VALUES ($1, $2, $3);
		`, testUserID, roleAnggotaID, env.KoperasiID)
	}

	_, _ = conn.Exec(ctx, "SET ROLE kopdes_app;")

	// Login via HTTP
	bodyLogin, _ := json.Marshal(handlers.LoginRequest{Email: email, Password: pwdPlain})
	reqLogin := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin))
	recLogin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin, reqLogin)

	var loginRes struct {
		Data handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recLogin.Body.Bytes(), &loginRes)
	token := loginRes.Data.AccessToken

	// 1. POST /api/v1/pembayaran/qris
	qrisReqBody, _ := json.Marshal(handlers.BuatQRISRequest{
		NominalDasarRp: 50000,
		JenisTransaksi: "iuran_bulanan",
		AnggotaID:      env.AnggotaID,
		RekeningQRISID: env.RekeningID,
	})

	reqQRIS := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(qrisReqBody))
	reqQRIS.Header.Set("Authorization", "Bearer "+token)
	reqQRIS.Header.Set("Content-Type", "application/json")
	recQRIS := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recQRIS, reqQRIS)

	if recQRIS.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for QRIS creation, got %d (body: %s)", recQRIS.Code, recQRIS.Body.String())
	}

	var qrisRes struct {
		Success bool                      `json:"success"`
		Data    handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recQRIS.Body.Bytes(), &qrisRes)

	if qrisRes.Data.KodeUnik < 1 || qrisRes.Data.KodeUnik > 999 {
		t.Errorf("Kode unik di luar rentang (1..999): %d", qrisRes.Data.KodeUnik)
	}
	if qrisRes.Data.NominalTotalRp != 50000+int64(qrisRes.Data.KodeUnik) {
		t.Errorf("Nominal total salah: %d", qrisRes.Data.NominalTotalRp)
	}

	// 2. GET /api/v1/pembayaran/{id}
	reqStatus := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/%s", qrisRes.Data.PembayaranID), nil)
	reqStatus.Header.Set("Authorization", "Bearer "+token)
	recStatus := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recStatus, reqStatus)

	if recStatus.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for payment status, got %d", recStatus.Code)
	}
}

func TestAPI_MakerChecker_HTTP_Violation(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// Buat user yang berperan sebagai admin
	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")

	emailAdmin := fmt.Sprintf("mc_admin_%s@kopdes.id", uuid.New().String()[:8])
	emailHash := security.HashBlindIndex(emailAdmin, []byte(env.Config.BlindIndexSecret))
	pwdPlain := "AdminPass123!"
	pwdHash, _ := security.HashPassword(pwdPlain)
	adminID := uuid.New()

	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'Admin Maker Checker');
	`, adminID, []byte("enkripsi"), emailHash, pwdHash)
	if err != nil {
		t.Fatalf("seed user error: %v", err)
	}

	var roleAdminID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE nama = 'admin'").Scan(&roleAdminID)
	if roleAdminID != uuid.Nil {
		_, _ = conn.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id)
			VALUES ($1, $2, $3);
		`, adminID, roleAdminID, env.KoperasiID)
	}

	_, _ = conn.Exec(ctx, "SET ROLE kopdes_app;")

	// Login admin
	bodyLogin, _ := json.Marshal(handlers.LoginRequest{Email: emailAdmin, Password: pwdPlain})
	reqLogin := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin))
	recLogin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin, reqLogin)

	var loginRes struct {
		Data handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recLogin.Body.Bytes(), &loginRes)
	tokenAdmin := loginRes.Data.AccessToken

	// Admin membuat pembayaran atas namanya sendiri
	bodyQRIS, _ := json.Marshal(handlers.BuatQRISRequest{
		NominalDasarRp: 100000,
		JenisTransaksi: "iuran_bulanan",
		AnggotaID:      env.AnggotaID,
		RekeningQRISID: env.RekeningID,
	})
	reqQRIS := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyQRIS))
	reqQRIS.Header.Set("Authorization", "Bearer "+tokenAdmin)
	reqQRIS.Header.Set("Content-Type", "application/json")
	recQRIS := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recQRIS, reqQRIS)

	if recQRIS.Code != http.StatusCreated {
		t.Fatalf("Expected 201 for Admin QRIS create, got %d (body: %s)", recQRIS.Code, recQRIS.Body.String())
	}

	var qrisRes struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recQRIS.Body.Bytes(), &qrisRes)
	pembayaranID := qrisRes.Data.PembayaranID

	// Admin mencoba MEMVERIFIKASI pembayarannya sendiri (Maker-Checker Violation)
	bodyVerif, _ := json.Marshal(handlers.VerifikasiRequest{
		Disetujui:    true,
		ReferensiRRN: "123456789012",
	})
	reqVerif := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", pembayaranID), bytes.NewReader(bodyVerif))
	reqVerif.Header.Set("Authorization", "Bearer "+tokenAdmin)
	recVerif := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerif, reqVerif)

	// Harapannya: 403 Forbidden ditolak oleh aturan maker-checker
	if recVerif.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for Maker-Checker violation, got %d (body: %s)", recVerif.Code, recVerif.Body.String())
	}
	t.Logf("Sukses: Verifikasi diri sendiri ditolak dengan HTTP 403: %s", recVerif.Body.String())
}
