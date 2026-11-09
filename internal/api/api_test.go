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
	PenyediaID    uuid.UUID
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

	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", koperasiID.String())

	var roleAdminID, roleAnggotaID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'admin'").Scan(&roleAdminID)
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'anggota'").Scan(&roleAnggotaID)

	if roleAdminID != uuid.Nil && roleAnggotaID != uuid.Nil {
		_, err = conn.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id)
			VALUES ($1, $2, $3), ($4, $5, $3)
			ON CONFLICT (koperasi_id, user_id, role_id) DO NOTHING;
		`, adminUserID, roleAdminID, koperasiID, anggotaUserID, roleAnggotaID)
		if err != nil {
			t.Fatalf("seed user_roles error: %v", err)
		}
	}

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

	loginHelper := func(email, pwd string) string {
		body, _ := json.Marshal(handlers.LoginRequest{Email: email, Password: pwd})
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		server.Router().ServeHTTP(rec, req)
		var res struct {
			Data handlers.LoginResponse `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		return res.Data.AccessToken
	}
	adminTok := loginHelper(adminEmail, "AdminPass123!")
	anggotaTok := loginHelper(anggotaEmail, "AnggotaPass123!")

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
		PenyediaID:    penyediaID,
		AdminToken:    adminTok,
		AnggotaToken:  anggotaTok,
	}
}

func (env *TestEnv) createAndLoginUser(t testing.TB, roleName string) (string, uuid.UUID) {
	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")

	email := fmt.Sprintf("user_%s@kopdes.id", uuid.New().String()[:8])
	emailHash := security.HashBlindIndex(email, []byte(env.Config.BlindIndexSecret))
	pwdPlain := "UserPass123!"
	pwdHash, _ := security.HashPassword(pwdPlain)
	userID := uuid.New()

	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'User Test');
	`, userID, []byte("enkripsi"), emailHash, pwdHash)
	if err != nil {
		t.Fatalf("seed user error: %v", err)
	}

	var roleID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE nama = $1", roleName).Scan(&roleID)
	if roleID != uuid.Nil {
		_, _ = conn.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id)
			VALUES ($1, $2, $3);
		`, userID, roleID, env.KoperasiID)
	}

	_, _ = conn.Exec(ctx, "SET ROLE kopdes_app;")

	bodyLogin, _ := json.Marshal(handlers.LoginRequest{Email: email, Password: pwdPlain})
	reqLogin := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin))
	recLogin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin, reqLogin)

	var loginRes struct {
		Data handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recLogin.Body.Bytes(), &loginRes)
	if loginRes.Data.AccessToken == "" {
		t.Fatalf("Login gagal untuk user %s (status %d): %s", email, recLogin.Code, recLogin.Body.String())
	}
	return loginRes.Data.AccessToken, userID
}

func (env *TestEnv) CreateBill(t testing.TB, anggotaID uuid.UUID, nominal int64) uuid.UUID {
	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())

	var jenisID uuid.UUID
	err = conn.QueryRow(ctx, "SELECT id FROM jenis_iuran WHERE koperasi_id = $1 LIMIT 1", env.KoperasiID).Scan(&jenisID)
	if err != nil || jenisID == uuid.Nil {
		jenisID = uuid.New()
		_, err = conn.Exec(ctx, `
			INSERT INTO jenis_iuran (id, koperasi_id, kode, nama, nominal, hari_jatuh_tempo, berlaku_mulai, created_by)
			VALUES ($1, $2, $3, 'Iuran Test', $4, 10, '2026-01-01', $5);
		`, jenisID, env.KoperasiID, "JI-"+uuid.New().String()[:8], nominal, env.AdminUserID)
		if err != nil {
			t.Fatalf("create jenis_iuran error: %v", err)
		}
	}

	billID := uuid.New()
	periode := fmt.Sprintf("2026-%02d", (time.Now().UnixNano()%12)+1)
	_, err = conn.Exec(ctx, `
		INSERT INTO tagihan_iuran (id, koperasi_id, anggota_id, jenis_iuran_id, periode, jatuh_tempo, nominal_pokok, status, created_by)
		VALUES ($1, $2, $3, $4, $5, '2026-11-10', $6, 'belum_bayar', $7);
	`, billID, env.KoperasiID, anggotaID, jenisID, periode, nominal, env.AdminUserID)
	if err != nil {
		t.Fatalf("create bill error: %v", err)
	}

	_, _ = conn.Exec(ctx, "SET ROLE kopdes_app;")
	return billID
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
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())
	var roleAnggotaID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'anggota'").Scan(&roleAnggotaID)
	if roleAnggotaID != uuid.Nil {
		_, _ = conn.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id)
			VALUES ($1, $2, $3);
		`, testUserID, roleAnggotaID, env.KoperasiID)
	}

	testAnggotaID := uuid.New()
	nik := fmt.Sprintf("3203%012d", time.Now().UnixNano()%1000000000000)
	_, _ = conn.Exec(ctx, `
		INSERT INTO anggota (id, koperasi_id, user_id, no_anggota, nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir, tanggal_lahir_enkripsi, jenis_kelamin, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'Penyetor QRIS', 'Bogor', $5, 'L', 'aktif', $7);
	`, testAnggotaID, env.KoperasiID, testUserID, "ANG-"+uuid.New().String()[:8], []byte("enkripsi"), hashSHA256(nik), env.AdminUserID)

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

	billID := env.CreateBill(t, testAnggotaID, 50000)

	// 1. POST /api/v1/pembayaran/qris
	qrisReqBody, _ := json.Marshal(handlers.BuatQRISRequest{
		TagihanIuranIDs: []uuid.UUID{billID},
		JenisTransaksi:  "iuran_bulanan",
		AnggotaID:       testAnggotaID,
		RekeningQRISID:  env.RekeningID,
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

	billID := env.CreateBill(t, env.AnggotaID, 100000)

	// Admin membuat pembayaran atas namanya sendiri
	bodyQRIS, _ := json.Marshal(handlers.BuatQRISRequest{
		TagihanIuranIDs: []uuid.UUID{billID},
		JenisTransaksi:  "iuran_bulanan",
		AnggotaID:       env.AnggotaID,
		RekeningQRISID:  env.RekeningID,
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

func TestK3_ApprovalSuccessFlow_EndToEnd(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	tokenAdminA, _ := env.createAndLoginUser(t, "admin")
	tokenAdminB, _ := env.createAndLoginUser(t, "admin")

	billID := env.CreateBill(t, env.AnggotaID, 50000)

	// 1. Admin A membuat pembayaran
	bodyQRIS, _ := json.Marshal(handlers.BuatQRISRequest{
		TagihanIuranIDs: []uuid.UUID{billID},
		JenisTransaksi:  "iuran_bulanan",
		AnggotaID:       env.AnggotaID,
		RekeningQRISID:  env.RekeningID,
	})
	reqQRIS := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyQRIS))
	reqQRIS.Header.Set("Authorization", "Bearer "+tokenAdminA)
	reqQRIS.Header.Set("Content-Type", "application/json")
	recQRIS := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recQRIS, reqQRIS)

	if recQRIS.Code != http.StatusCreated {
		t.Fatalf("Gagal buat QRIS: %d", recQRIS.Code)
	}

	var qrisRes struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recQRIS.Body.Bytes(), &qrisRes)
	pembayaranID := qrisRes.Data.PembayaranID

	// 2. Pembayar mengajukan bukti pembayaran (transisi ke menunggu_verifikasi)
	bodyBukti, _ := json.Marshal(handlers.KlaimBuktiRequest{
		ReferensiRRN: "123456789012",
		Catatan:      "Sudah transfer via BCA mobile",
	})
	reqBukti := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/bukti", pembayaranID), bytes.NewReader(bodyBukti))
	reqBukti.Header.Set("Authorization", "Bearer "+tokenAdminA)
	reqBukti.Header.Set("Content-Type", "application/json")
	recBukti := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recBukti, reqBukti)

	if recBukti.Code != http.StatusOK {
		t.Fatalf("Gagal klaim bukti pembayaran: %d (body: %s)", recBukti.Code, recBukti.Body.String())
	}

	// 3. Admin B memverifikasi pembayaran (Sukses)
	bodyVerif, _ := json.Marshal(handlers.VerifikasiRequest{
		Disetujui:    true,
		ReferensiRRN: "123456789012",
	})
	reqVerif := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", pembayaranID), bytes.NewReader(bodyVerif))
	reqVerif.Header.Set("Authorization", "Bearer "+tokenAdminB)
	reqVerif.Header.Set("Content-Type", "application/json")
	recVerif := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerif, reqVerif)

	if recVerif.Code != http.StatusOK {
		t.Fatalf("Gagal verifikasi pembayaran: %d (body: %s)", recVerif.Code, recVerif.Body.String())
	}

	// 4. Verifikasi mutasi kas dan log tercatat di basis data
	ctx := context.Background()
	var mutasiCount int
	_ = env.Pool.QueryRow(ctx, "SELECT count(*) FROM kas_mutasi WHERE pembayaran_id = $1;", pembayaranID).Scan(&mutasiCount)
	if mutasiCount < 1 {
		t.Errorf("Kas mutasi tidak tercatat di database untuk pembayaran %s", pembayaranID)
	}

	var logCount int
	_ = env.Pool.QueryRow(ctx, "SELECT count(*) FROM pembayaran_verifikasi_log WHERE pembayaran_id = $1;", pembayaranID).Scan(&logCount)
	if logCount < 2 {
		t.Errorf("Log verifikasi tidak lengkap (harus ada unggah_bukti dan setujui), dapat: %d", logCount)
	}

	// 5. Coba verifikasi ulang (harus ditolak karena status sudah disetujui)
	recDouble := httptest.NewRecorder()
	reqDouble := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", pembayaranID), bytes.NewReader(bodyVerif))
	reqDouble.Header.Set("Authorization", "Bearer "+tokenAdminB)
	env.Server.Router().ServeHTTP(recDouble, reqDouble)

	if recDouble.Code != http.StatusBadRequest {
		t.Errorf("Verifikasi ganda harusnya 400 Bad Request, dapat: %d", recDouble.Code)
	}
}

func TestAPI_Admin_RekeningQRIS_Management(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	tokenAdmin, _ := env.createAndLoginUser(t, "admin")
	tokenAnggota, _ := env.createAndLoginUser(t, "anggota")

	// Payload EMVCo Statis valid
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	validStaticQR := fmt.Sprintf("000201010211%s5204541153033605802ID5913KOPDES MAKMUR6007BANDUNG63041234", tag26)

	// 1. Pengujian Akses Non-Admin (Wajib Ditolak 403)
	bodyNonAdmin, _ := json.Marshal(handlers.AdminDaftarRekeningQRISRequest{
		NamaMerchant:         "KOPDES MAKMUR",
		NMID:                 "ID1020304050607",
		NamaBankPJP:          "Bank Mandiri",
		NoRekeningSettlement: "1300099887766",
		NamaPemilikRekening:  "Koperasi Makmur Sejahtera",
		IsAtasNamaBadanHukum: true,
		QRISPayloadStatis:    validStaticQR,
	})
	reqNonAdmin := httptest.NewRequest("POST", "/api/v1/koperasi/rekening-qris", bytes.NewReader(bodyNonAdmin))
	reqNonAdmin.Header.Set("Authorization", "Bearer "+tokenAnggota)
	reqNonAdmin.Header.Set("Content-Type", "application/json")
	recNonAdmin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recNonAdmin, reqNonAdmin)

	if recNonAdmin.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for non-admin QRIS registration, got %d", recNonAdmin.Code)
	}

	// 2. Pengujian Validasi UU & Peraturan BI: Wajib Atas Nama Badan Hukum Koperasi (is_atas_nama_badan_hukum = false ditolak 400)
	bodyBukanBadanHukum, _ := json.Marshal(handlers.AdminDaftarRekeningQRISRequest{
		NamaMerchant:         "KOPDES MAKMUR",
		NMID:                 "ID1020304050607",
		NamaBankPJP:          "Bank Mandiri",
		NoRekeningSettlement: "1300099887766",
		NamaPemilikRekening:  "Akun Pribadi Pengurus",
		IsAtasNamaBadanHukum: false,
		QRISPayloadStatis:    validStaticQR,
	})
	reqBadanHukum := httptest.NewRequest("POST", "/api/v1/koperasi/rekening-qris", bytes.NewReader(bodyBukanBadanHukum))
	reqBadanHukum.Header.Set("Authorization", "Bearer "+tokenAdmin)
	reqBadanHukum.Header.Set("Content-Type", "application/json")
	recBadanHukum := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recBadanHukum, reqBadanHukum)

	if recBadanHukum.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for non-legal-entity QRIS, got %d", recBadanHukum.Code)
	}

	// 3. Admin Mendaftarkan QRIS Statis Badan Hukum yang Sah
	bodyValid, _ := json.Marshal(handlers.AdminDaftarRekeningQRISRequest{
		NamaMerchant:         "KOPDES MAKMUR MANDIRI",
		NMID:                 "ID1099887766554",
		NamaBankPJP:          "Bank Mandiri",
		NoRekeningSettlement: "1300099887766",
		NamaPemilikRekening:  "Koperasi Konsumen Makmur Sejahtera",
		IsAtasNamaBadanHukum: true,
		QRISPayloadStatis:    validStaticQR,
	})
	reqDaftar := httptest.NewRequest("POST", "/api/v1/koperasi/rekening-qris", bytes.NewReader(bodyValid))
	reqDaftar.Header.Set("Authorization", "Bearer "+tokenAdmin)
	reqDaftar.Header.Set("Content-Type", "application/json")
	recDaftar := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recDaftar, reqDaftar)

	if recDaftar.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for Admin QRIS registration, got %d (body: %s)", recDaftar.Code, recDaftar.Body.String())
	}

	var daftarRes struct {
		Success bool `json:"success"`
		Data    struct {
			ID   uuid.UUID `json:"id"`
			NMID string    `json:"nmid"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recDaftar.Body.Bytes(), &daftarRes)
	createdQRISID := daftarRes.Data.ID

	if createdQRISID == uuid.Nil {
		t.Fatalf("Gagal mendapatkan ID rekening QRIS yang baru didaftarkan")
	}

	// 4. Admin Mengambil Daftar Rekening QRIS
	reqList := httptest.NewRequest("GET", "/api/v1/koperasi/rekening-qris", nil)
	reqList.Header.Set("Authorization", "Bearer "+tokenAdmin)
	recList := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET rekening-qris, got %d", recList.Code)
	}

	var listRes struct {
		Success bool                        `json:"success"`
		Data    []handlers.RekeningQRISItem `json:"data"`
	}
	_ = json.Unmarshal(recList.Body.Bytes(), &listRes)
	if len(listRes.Data) == 0 {
		t.Errorf("Expected at least 1 registered QRIS account, got 0")
	}

	// 5. Admin Menonaktifkan Rekening QRIS
	reqDelete := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/koperasi/rekening-qris/%s", createdQRISID), nil)
	reqDelete.Header.Set("Authorization", "Bearer "+tokenAdmin)
	recDelete := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recDelete, reqDelete)

	if recDelete.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for DELETE rekening-qris, got %d (body: %s)", recDelete.Code, recDelete.Body.String())
	}
}

func TestAPI_QRIS_AutoLockNominal(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	token := env.AnggotaToken
	billID := env.CreateBill(t, env.AnggotaID, 75000)

	// Daftarkan QRIS Statis valid format EMVCo Bank Indonesia
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	validStaticQR := fmt.Sprintf("000201010211%s5204541153033605802ID5913KOPDES MAKMUR6007BANDUNG63041234", tag26)

	var newRekeningID uuid.UUID
	err := env.Pool.QueryRow(context.Background(), `
		INSERT INTO rekening_qris_koperasi (
			koperasi_id, penyedia_id, nama_merchant, nmid,
			nama_bank_pjp, no_rekening_settlement, nama_pemilik_rekening,
			is_atas_nama_badan_hukum, qris_payload_statis, status_aktif, created_by
		) VALUES (
			$1, $2, 'Kopdes Makmur AutoLock', 'ID9988776655443',
			'Bank Mandiri', '1300011223344', 'Koperasi Makmur Sejahtera',
			true, $3, true, $4
		) RETURNING id;
	`, env.KoperasiID, env.PenyediaID, validStaticQR, env.AdminUserID).Scan(&newRekeningID)
	if err != nil {
		t.Fatalf("Gagal insert rekening QRIS valid: %v", err)
	}

	// Anggota meminta pembuatan QRIS pembayaran
	qrisReqBody, _ := json.Marshal(handlers.BuatQRISRequest{
		TagihanIuranIDs: []uuid.UUID{billID},
		JenisTransaksi:  "iuran_bulanan",
		AnggotaID:       env.AnggotaID,
		RekeningQRISID:  newRekeningID,
	})

	req := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(qrisReqBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for AutoLock QRIS, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var res struct {
		Success bool                      `json:"success"`
		Data    handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)

	// Verifikasi fitur Auto-Lock Nominal
	if !res.Data.IsAutoLockNominal {
		t.Errorf("Expected is_autolock_nominal to be true")
	}

	expectedNominalStr := fmt.Sprintf("%d", res.Data.NominalTotalRp)
	if !bytes.Contains([]byte(res.Data.QRISPayload), []byte("54"+fmt.Sprintf("%02d", len(expectedNominalStr))+expectedNominalStr)) {
		t.Errorf("Tag 54 auto-lock nominal tidak ditemukan dalam QRIS payload: %s", res.Data.QRISPayload)
	}

	t.Logf("Sukses Auto-Lock QRIS: Nominal Rp %d terkunci di payload dinamis %s", res.Data.NominalTotalRp, res.Data.QRISPayload)
}

func TestT1_PaymentBindingToBills(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")

	// 1. Seed another anggota for testing unauthorized access to bills
	otherUserID := uuid.New()
	otherEmail := fmt.Sprintf("other_%s@kopdes.id", uuid.New().String()[:8])
	otherEmailHash := security.HashBlindIndex(otherEmail, []byte(env.Config.BlindIndexSecret))
	otherPwdHash, _ := security.HashPassword("OtherPass123!")

	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'Anggota Lain');
	`, otherUserID, []byte("enkripsi"), otherEmailHash, otherPwdHash)
	if err != nil {
		t.Fatalf("seed other user error: %v", err)
	}

	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())

	var roleAnggotaID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'anggota'").Scan(&roleAnggotaID)
	_, err = conn.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, koperasi_id)
		VALUES ($1, $2, $3);
	`, otherUserID, roleAnggotaID, env.KoperasiID)
	if err != nil {
		t.Fatalf("seed other user_roles error: %v", err)
	}

	otherAnggotaID := uuid.New()
	nikOther := fmt.Sprintf("3202%012d", time.Now().UnixNano()%1000000000000)
	_, err = conn.Exec(ctx, `
		INSERT INTO anggota (id, koperasi_id, user_id, no_anggota, nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir, tanggal_lahir_enkripsi, jenis_kelamin, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'Anggota Lain', 'Bandung', $5, 'P', 'aktif', $7);
	`, otherAnggotaID, env.KoperasiID, otherUserID, "ANG-"+uuid.New().String()[:8], []byte("enkripsi"), hashSHA256(nikOther), env.AdminUserID)
	if err != nil {
		t.Fatalf("seed other anggota error: %v", err)
	}

	// Seed jenis_iuran
	jenisIuranID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO jenis_iuran (id, koperasi_id, kode, nama, nominal, hari_jatuh_tempo, berlaku_mulai, created_by)
		VALUES ($1, $2, $3, 'Iuran Wajib Bulanan', 50000, 10, '2026-01-01', $4);
	`, jenisIuranID, env.KoperasiID, "WAJIB-"+uuid.New().String()[:6], env.AdminUserID)
	if err != nil {
		t.Fatalf("seed jenis_iuran error: %v", err)
	}

	// Seed bill 1 for env.AnggotaID (unpaid, 50000)
	bill1ID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO tagihan_iuran (id, koperasi_id, anggota_id, jenis_iuran_id, periode, jatuh_tempo, nominal_pokok, status, created_by)
		VALUES ($1, $2, $3, $4, '2026-11', '2026-11-10', 50000, 'belum_bayar', $5);
	`, bill1ID, env.KoperasiID, env.AnggotaID, jenisIuranID, env.AdminUserID)
	if err != nil {
		t.Fatalf("seed bill1 error: %v", err)
	}

	// Seed bill for other anggota (75000)
	billOtherID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO tagihan_iuran (id, koperasi_id, anggota_id, jenis_iuran_id, periode, jatuh_tempo, nominal_pokok, status, created_by)
		VALUES ($1, $2, $3, $4, '2026-11', '2026-11-10', 75000, 'belum_bayar', $5);
	`, billOtherID, env.KoperasiID, otherAnggotaID, jenisIuranID, env.AdminUserID)
	if err != nil {
		t.Fatalf("seed other bill error: %v", err)
	}

	_, _ = conn.Exec(ctx, "SET ROLE kopdes_app;")

	// Case 1: Member submits request WITHOUT any tagihan_iuran_id or pesanan_id (pure arbitrary nominal)
	// Must be rejected with HTTP 400 Bad Request
	bodyNoBill, _ := json.Marshal(map[string]interface{}{
		"nominal_dasar": 50000,
		"rekening_qris_id": env.RekeningID,
	})
	req1 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyNoBill))
	req1.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusBadRequest {
		t.Fatalf("Case 1 FAILED: Expected 400 Bad Request for payment without bills/orders, got %d (body: %s)", rec1.Code, rec1.Body.String())
	}

	// Case 2: Member tries to pay another member's bill
	// Must be rejected with 400 Bad Request or 403 Forbidden
	bodyOtherBill, _ := json.Marshal(map[string]interface{}{
		"tagihan_iuran_id": []string{billOtherID.String()},
		"rekening_qris_id": env.RekeningID,
	})
	req2 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyOtherBill))
	req2.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusBadRequest && rec2.Code != http.StatusForbidden {
		t.Fatalf("Case 2 FAILED: Expected 400 or 403 for paying another member's bill, got %d (body: %s)", rec2.Code, rec2.Body.String())
	}

	// Case 3: Member pays their own bill successfully
	bodyOwnBill, _ := json.Marshal(map[string]interface{}{
		"tagihan_iuran_id": []string{bill1ID.String()},
		"rekening_qris_id": env.RekeningID,
	})
	req3 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyOwnBill))
	req3.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusCreated {
		t.Fatalf("Case 3 FAILED: Expected 201 Created for valid member bill payment, got %d (body: %s)", rec3.Code, rec3.Body.String())
	}

	var res3 struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(rec3.Body.Bytes(), &res3)

	// Verify server calculated nominal_dasar from the bill
	if res3.Data.NominalDasarRp != 50000 {
		t.Errorf("Expected nominal_dasar 50000 from bill, got %d", res3.Data.NominalDasarRp)
	}

	// Verify pembayaran_alokasi is created in database
	var (
		alokasiCount int
		nominalAlokasi int64
		alokasiTagihanID uuid.UUID
	)
	err = conn.QueryRow(ctx, `
		SELECT count(*), coalesce(sum(nominal_alokasi), 0), (array_agg(tagihan_iuran_id))[1]
		FROM pembayaran_alokasi
		WHERE pembayaran_id = $1
	`, res3.Data.PembayaranID).Scan(&alokasiCount, &nominalAlokasi, &alokasiTagihanID)
	if err != nil {
		t.Fatalf("Query pembayaran_alokasi error: %v", err)
	}

	if alokasiCount != 1 {
		t.Errorf("Expected 1 pembayaran_alokasi record, got %d", alokasiCount)
	}
	if nominalAlokasi != 50000 {
		t.Errorf("Expected nominal_alokasi 50000, got %d", nominalAlokasi)
	}
	if alokasiTagihanID != bill1ID {
		t.Errorf("Expected alokasi tagihan_iuran_id %s, got %s", bill1ID, alokasiTagihanID)
	}

	// Case 4: Member tries to pay the SAME bill while payment is still active/pending
	// Must be rejected
	req4 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyOwnBill))
	req4.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	req4.Header.Set("Content-Type", "application/json")
	rec4 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec4, req4)

	if rec4.Code != http.StatusBadRequest && rec4.Code != http.StatusConflict {
		t.Fatalf("Case 4 FAILED: Expected 400 or 409 for paying a bill with active payment, got %d (body: %s)", rec4.Code, rec4.Body.String())
	}

	// Case 5: Admin pays on behalf of other member with audit trail logged
	bodyAdmin, _ := json.Marshal(map[string]interface{}{
		"tagihan_iuran_id": []string{billOtherID.String()},
		"rekening_qris_id": env.RekeningID,
	})
	req5 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyAdmin))
	req5.Header.Set("Authorization", "Bearer "+env.AdminToken)
	req5.Header.Set("Content-Type", "application/json")
	rec5 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec5, req5)

	if rec5.Code != http.StatusCreated {
		t.Fatalf("Case 5 FAILED: Expected 201 Created for Admin on-behalf payment, got %d (body: %s)", rec5.Code, rec5.Body.String())
	}

	var res5 struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(rec5.Body.Bytes(), &res5)

	// Verify audit_log recorded for admin on-behalf action
	var auditCount int
	err = conn.QueryRow(ctx, `
		SELECT count(*) FROM audit_log
		WHERE record_id = $1 AND nama_tabel = 'pembayaran' AND aksi = 'INSERT' AND user_id = $2
	`, res5.Data.PembayaranID, env.AdminUserID).Scan(&auditCount)
	if err != nil {
		t.Fatalf("Query audit_log error: %v", err)
	}
	if auditCount < 1 {
		t.Errorf("Expected at least 1 audit_log entry for admin on-behalf payment, got %d", auditCount)
	}
}
