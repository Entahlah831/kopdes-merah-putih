package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/qris"
	"github.com/kopdes/merah-putih/internal/security"
	"github.com/kopdes/merah-putih/internal/simkopdes"
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
	cfg.AESMasterKey = "test-aes-master-key-32-bytes-long!"

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

	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	qrisPrefix := fmt.Sprintf("000201010211%s5204541153033605802ID5917Merchant API Test6007Jakarta6304", tag26)
	validQRISPayload := qrisPrefix + qris.CalculateCRC16(qrisPrefix)

	nmid := fmt.Sprintf("ID%013d", time.Now().UnixNano()%10000000000000)
	_, err = conn.Exec(ctx, `
		INSERT INTO rekening_qris_koperasi (
			id, koperasi_id, penyedia_id, nama_merchant, nmid,
			nama_bank_pjp, no_rekening_settlement, nama_pemilik_rekening,
			is_atas_nama_badan_hukum, qris_payload_statis, created_by
		) VALUES (
			$1, $2, $3, 'Merchant API Test', $4,
			'Bank Mandiri', '1112223334', 'Koperasi API',
			true, $5, $6
		);
	`, rekeningID, koperasiID, penyediaID, nmid, validQRISPayload, adminUserID)
	if err != nil {
		t.Fatalf("seed rekening QRIS error: %v", err)
	}

	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false); SET ROLE kopdes_app;")

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

func (env *TestEnv) createAndLoginUserFull(t testing.TB, roleName string) (handlers.LoginResponse, string, string, uuid.UUID) {
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
	err = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = $1 OR nama = $1 LIMIT 1", roleName).Scan(&roleID)
	if err != nil || roleID == uuid.Nil {
		roleID = uuid.New()
		_, err = conn.Exec(ctx, "INSERT INTO roles (id, kode, nama, deskripsi) VALUES ($1, $2, $3, $4) ON CONFLICT (kode) DO NOTHING;", roleID, roleName, roleName, roleName)
		if err != nil {
			t.Fatalf("insert role error: %v", err)
		}
		err = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = $1", roleName).Scan(&roleID)
		if err != nil {
			t.Fatalf("scan role error: %v", err)
		}
	}

	if roleID != uuid.Nil {
		_, err = conn.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (koperasi_id, user_id, role_id) DO NOTHING;
		`, userID, roleID, env.KoperasiID)
		if err != nil {
			t.Fatalf("insert user_roles error: %v", err)
		}
	}

	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false); SET ROLE kopdes_app;")

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
	return loginRes.Data, email, pwdPlain, userID
}

func (env *TestEnv) createAndLoginUser(t testing.TB, roleName string) (string, uuid.UUID) {
	loginRes, _, _, userID := env.createAndLoginUserFull(t, roleName)
	return loginRes.AccessToken, userID
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

	jenisID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO jenis_iuran (id, koperasi_id, kode, nama, nominal, hari_jatuh_tempo, berlaku_mulai, created_by)
		VALUES ($1, $2, $3, 'Iuran Test', $4, 10, '2026-01-01', $5);
	`, jenisID, env.KoperasiID, "JI-"+uuid.New().String()[:12], nominal, env.AdminUserID)
	if err != nil {
		t.Fatalf("create jenis_iuran error: %v", err)
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

	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false); SET ROLE kopdes_app;")
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

	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false); SET ROLE kopdes_app;")

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

	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false); SET ROLE kopdes_app;")

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

	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false); SET ROLE kopdes_app;")

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

	// Payload EMVCo Statis valid dengan CRC16 terhitung
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	validStaticQRPrefix := fmt.Sprintf("000201010211%s5204541153033605802ID5913KOPDES MAKMUR6007BANDUNG6304", tag26)
	validStaticQR := validStaticQRPrefix + qris.CalculateCRC16(validStaticQRPrefix)

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

	if recBadanHukum.Code != http.StatusBadRequest && recBadanHukum.Code != http.StatusForbidden {
		t.Errorf("Expected 400 Bad Request or 403 Forbidden for non-legal-entity QRIS, got %d", recBadanHukum.Code)
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

	// Daftarkan QRIS Statis valid format EMVCo Bank Indonesia dengan CRC valid
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	validStaticQRPrefix := fmt.Sprintf("000201010211%s5204541153033605802ID5913KOPDES MAKMUR6007BANDUNG6304", tag26)
	validStaticQR := validStaticQRPrefix + qris.CalculateCRC16(validStaticQRPrefix)

	// Aktifkan server dengan QRISDynamicEnabled = true untuk menguji AutoLock
	cfgDynamic := env.Config
	cfgDynamic.QRISDynamicEnabled = true
	serverDynamic := api.NewServer(env.Pool, cfgDynamic)

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
	serverDynamic.Router().ServeHTTP(rec, req)

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

	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false); SET ROLE kopdes_app;")

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

func TestT5_AuthAndPermissions_Reproduction(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	ctx := context.Background()

	// 1. Pengawas hanya memiliki hak akses read-only (bukan mutatif)
	tokenPengawas, _ := env.createAndLoginUser(t, "pengawas")

	// 1a. Pengawas mencoba verifikasi pembayaran (MUTATIF) -> 403 Forbidden
	dummyPayID := uuid.New()
	bodyVerif, _ := json.Marshal(handlers.VerifikasiRequest{
		Disetujui:    true,
		ReferensiRRN: "112233445566",
	})
	reqVerif := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", dummyPayID), bytes.NewReader(bodyVerif))
	reqVerif.Header.Set("Authorization", "Bearer "+tokenPengawas)
	recVerif := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerif, reqVerif)

	if recVerif.Code != http.StatusForbidden {
		t.Fatalf("T5 FAILED: Role 'pengawas' must get 403 Forbidden on payment verification, got %d", recVerif.Code)
	}

	// 1b. Pengawas membaca antrean pembayaran (READ-ONLY) -> 200 OK
	reqAntrean := httptest.NewRequest("GET", "/api/v1/pembayaran/antrean", nil)
	reqAntrean.Header.Set("Authorization", "Bearer "+tokenPengawas)
	recAntrean := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recAntrean, reqAntrean)
	if recAntrean.Code != http.StatusOK {
		t.Fatalf("T5 FAILED: Role 'pengawas' must be allowed to read antrean, got %d", recAntrean.Code)
	}

	// 2. Operator tidak boleh melakukan mutasi iuran massal
	tokenOperator, _ := env.createAndLoginUser(t, "operator")

	// 2a. Operator mencoba POST /api/v1/iuran/generate (MUTATIF) -> 403 Forbidden
	bodyGen, _ := json.Marshal(map[string]string{
		"periode": "2026-11",
	})
	reqGen := httptest.NewRequest("POST", "/api/v1/iuran/generate", bytes.NewReader(bodyGen))
	reqGen.Header.Set("Authorization", "Bearer "+tokenOperator)
	recGen := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recGen, reqGen)

	if recGen.Code != http.StatusForbidden {
		t.Fatalf("T5 FAILED: Role 'operator' must get 403 Forbidden on generate bill, got %d", recGen.Code)
	}

	// 2b. Operator membaca daftar menunggak (READ-ONLY) -> 200 OK
	reqTunggak := httptest.NewRequest("GET", "/api/v1/iuran/menunggak", nil)
	reqTunggak.Header.Set("Authorization", "Bearer "+tokenOperator)
	recTunggak := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recTunggak, reqTunggak)
	if recTunggak.Code != http.StatusOK {
		t.Fatalf("T5 FAILED: Role 'operator' must be allowed to read menunggak, got %d", recTunggak.Code)
	}

	// 3. Admin / Pengurus tetap boleh mengakses endpoint mutasi (tidak 403 Forbidden)
	reqAdminGen := httptest.NewRequest("POST", "/api/v1/iuran/generate", bytes.NewReader(bodyGen))
	reqAdminGen.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recAdminGen := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recAdminGen, reqAdminGen)
	if recAdminGen.Code == http.StatusForbidden {
		t.Fatalf("T5 FAILED: Role 'admin' should not receive 403 Forbidden, got %d", recAdminGen.Code)
	}

	// 4. Verifikasi masa aktif access token (15 menit = 900 detik) dan keberadaan refresh token
	loginRes1, _, _, _ := env.createAndLoginUserFull(t, "anggota")
	if loginRes1.ExpiresIn != 900 {
		t.Errorf("T5 FAILED: Expected access token ExpiresIn to be 900 seconds (15m), got %d", loginRes1.ExpiresIn)
	}
	if loginRes1.RefreshToken == "" {
		t.Fatalf("T5 FAILED: RefreshToken must not be empty in login response")
	}

	// 5. Rotasi Refresh Token (POST /api/v1/auth/refresh)
	bodyRef1, _ := json.Marshal(handlers.RefreshRequest{
		RefreshToken: loginRes1.RefreshToken,
	})
	reqRef1 := httptest.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewReader(bodyRef1))
	recRef1 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recRef1, reqRef1)

	if recRef1.Code != http.StatusOK {
		t.Fatalf("T5 FAILED: Refresh token rotation failed, got %d (body: %s)", recRef1.Code, recRef1.Body.String())
	}

	var refRes1 struct {
		Data handlers.RefreshResponse `json:"data"`
	}
	_ = json.Unmarshal(recRef1.Body.Bytes(), &refRes1)

	if refRes1.Data.AccessToken == "" || refRes1.Data.RefreshToken == "" {
		t.Fatalf("T5 FAILED: RefreshResponse missing new access or refresh token (body: %s)", recRef1.Body.String())
	}
	if refRes1.Data.AccessToken == loginRes1.AccessToken {
		t.Errorf("T5 FAILED: Expected rotated access token to differ from original")
	}
	if refRes1.Data.RefreshToken == loginRes1.RefreshToken {
		t.Errorf("T5 FAILED: Expected rotated refresh token to differ from original")
	}
	if refRes1.Data.ExpiresIn != 900 {
		t.Errorf("T5 FAILED: Expected new access token ExpiresIn 900, got %d", refRes1.Data.ExpiresIn)
	}

	// Uji access token baru berfungsi untuk endpoint autentikasi
	reqMe := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+refRes1.Data.AccessToken)
	recMe := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recMe, reqMe)
	if recMe.Code != http.StatusOK {
		t.Fatalf("T5 FAILED: Rotated access token cannot access /auth/me, got %d", recMe.Code)
	}

	// 6. Deteksi Token Reuse (Gunakan kembali refresh token lama yang sudah dicabut)
	reqReuse := httptest.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewReader(bodyRef1))
	recReuse := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recReuse, reqReuse)

	if recReuse.Code != http.StatusUnauthorized {
		t.Fatalf("T5 FAILED: Expected 401 Unauthorized for reused refresh token, got %d", recReuse.Code)
	}

	// Karena ada deteksi pelanggaran sesi (reuse), token turunan (refRes1.Data.RefreshToken) pun ikut hangus
	bodyRef2, _ := json.Marshal(handlers.RefreshRequest{
		RefreshToken: refRes1.Data.RefreshToken,
	})
	reqRef2 := httptest.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewReader(bodyRef2))
	recRef2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recRef2, reqRef2)

	if recRef2.Code != http.StatusUnauthorized {
		t.Fatalf("T5 FAILED: Expected 401 on active refresh token after breach detection, got %d", recRef2.Code)
	}

	// 7. Revokasi Eksplisit (Logout via POST /api/v1/auth/revoke)
	loginRes2, _, _, _ := env.createAndLoginUserFull(t, "anggota")
	bodyRev, _ := json.Marshal(handlers.RefreshRequest{
		RefreshToken: loginRes2.RefreshToken,
	})
	reqRev := httptest.NewRequest("POST", "/api/v1/auth/revoke", bytes.NewReader(bodyRev))
	recRev := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recRev, reqRev)

	if recRev.Code != http.StatusOK {
		t.Fatalf("T5 FAILED: Revoke endpoint failed, got %d", recRev.Code)
	}

	// Coba refresh setelah di-revoke -> wajib 401
	reqAfterRev := httptest.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewReader(bodyRev))
	recAfterRev := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recAfterRev, reqAfterRev)
	if recAfterRev.Code != http.StatusUnauthorized {
		t.Fatalf("T5 FAILED: Expected 401 after explicit revocation, got %d", recAfterRev.Code)
	}

	// 8. Pemilihan Koperasi Eksplisit & Hierarki Peran Deterministik
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn error: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE; SELECT set_config('app.is_platform_admin', 'true', false);")

	// Buat Koperasi Kedua
	coop2ID := uuid.New()
	_, err = conn.Exec(ctx, `
		INSERT INTO koperasi (id, kode_koperasi, nama, jenis_kopdes, wilayah_desa_id, alamat)
		VALUES ($1, $2, 'Koperasi Multi Tenant Test', 'desa',
		        (SELECT id FROM wilayah LIMIT 1), 'Jl. Merdeka No. 2');
	`, coop2ID, fmt.Sprintf("KOP-MULTI-%s", uuid.New().String()[:10]))
	if err != nil {
		t.Fatalf("seed coop2 error: %v", err)
	}

	multiUserID := uuid.New()
	multiEmail := fmt.Sprintf("multi_%s@kopdes.id", uuid.New().String()[:8])
	multiEmailHash := security.HashBlindIndex(multiEmail, []byte(env.Config.BlindIndexSecret))
	multiPwdPlain := "MultiPass123!"
	multiPwdHash, _ := security.HashPassword(multiPwdPlain)

	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'Multi Role User');
	`, multiUserID, []byte("enkripsi"), multiEmailHash, multiPwdHash)
	if err != nil {
		t.Fatalf("seed multi user error: %v", err)
	}

	var roleAdminID, roleAnggotaID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'admin'").Scan(&roleAdminID)
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'anggota'").Scan(&roleAnggotaID)

	// Hubungkan: di env.KoperasiID sebagai 'anggota', di coop2ID sebagai 'admin'
	_, err = conn.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, koperasi_id)
		VALUES ($1, $2, $3), ($1, $4, $5);
	`, multiUserID, roleAnggotaID, env.KoperasiID, roleAdminID, coop2ID)
	if err != nil {
		t.Fatalf("seed multi user_roles error: %v", err)
	}

	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false); SET ROLE kopdes_app;")

	// 8a. Login eksplisit memilih env.KoperasiID -> peran harus 'anggota'
	bodyLoginCoop1, _ := json.Marshal(handlers.LoginRequest{
		Email:      multiEmail,
		Password:   multiPwdPlain,
		KoperasiID: &env.KoperasiID,
	})
	reqLoginCoop1 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLoginCoop1))
	recLoginCoop1 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLoginCoop1, reqLoginCoop1)

	var resCoop1 struct {
		Data handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recLoginCoop1.Body.Bytes(), &resCoop1)
	if resCoop1.Data.Role != "anggota" || resCoop1.Data.KoperasiID != env.KoperasiID {
		t.Errorf("T5 FAILED: Expected role 'anggota' for coop1, got '%s', coop: %s", resCoop1.Data.Role, resCoop1.Data.KoperasiID)
	}

	// 8b. Login eksplisit memilih coop2ID -> peran harus 'admin'
	bodyLoginCoop2, _ := json.Marshal(handlers.LoginRequest{
		Email:      multiEmail,
		Password:   multiPwdPlain,
		KoperasiID: &coop2ID,
	})
	reqLoginCoop2 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLoginCoop2))
	recLoginCoop2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLoginCoop2, reqLoginCoop2)

	var resCoop2 struct {
		Data handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recLoginCoop2.Body.Bytes(), &resCoop2)
	if resCoop2.Data.Role != "admin" || resCoop2.Data.KoperasiID != coop2ID {
		t.Errorf("T5 FAILED: Expected role 'admin' for coop2, got '%s', coop: %s", resCoop2.Data.Role, resCoop2.Data.KoperasiID)
	}

	// 8c. Login tanpa memilih koperasi -> urutan hierarki deterministik memprioritaskan 'admin'
	bodyLoginNoCoop, _ := json.Marshal(handlers.LoginRequest{
		Email:    multiEmail,
		Password: multiPwdPlain,
	})
	reqLoginNoCoop := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLoginNoCoop))
	recLoginNoCoop := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLoginNoCoop, reqLoginNoCoop)

	var resNoCoop struct {
		Data handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recLoginNoCoop.Body.Bytes(), &resNoCoop)
	if resNoCoop.Data.Role != "admin" {
		t.Errorf("T5 FAILED: Expected deterministic highest priority role 'admin', got '%s'", resNoCoop.Data.Role)
	}
}

func TestS1_IDOR_PembayaranStatus_Reproduction(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// 1. Buat pembayaran untuk Anggota A
	billID := env.CreateBill(t, env.AnggotaID, 75000)
	bodyPay, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{billID},
	})
	reqPay := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyPay))
	reqPay.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqPay.Header.Set("Content-Type", "application/json")
	recPay := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPay, reqPay)

	if recPay.Code != http.StatusCreated {
		t.Fatalf("Gagal membuat pembayaran untuk Anggota A: %d (body: %s)", recPay.Code, recPay.Body.String())
	}

	var payRes struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recPay.Body.Bytes(), &payRes)
	payIDA := payRes.Data.PembayaranID

	// 2. Anggota B (anggota lain dalam koperasi yang sama) mencoba mengakses pembayaran Anggota A
	tokenAnggotaB, _ := env.createAndLoginUser(t, "anggota")

	reqIDOR := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/%s", payIDA), nil)
	reqIDOR.Header.Set("Authorization", "Bearer "+tokenAnggotaB)
	recIDOR := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recIDOR, reqIDOR)

	// Harapan aman: Anggota B TIDAK boleh melihat pembayaran Anggota A (harus 403 atau 404)
	if recIDOR.Code == http.StatusOK {
		t.Fatalf("VULNERABILITY S1 REPRODUCED: Anggota B dapat membaca pembayaran Anggota A melalui IDOR (HTTP 200 OK)")
	}

	if recIDOR.Code != http.StatusNotFound && recIDOR.Code != http.StatusForbidden {
		t.Errorf("Expected 403 or 404 for IDOR access, got %d", recIDOR.Code)
	}

	// 3. Admin koperasi boleh melihat pembayaran siapa pun di koperasinya
	reqAdmin := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/%s", payIDA), nil)
	reqAdmin.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recAdmin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recAdmin, reqAdmin)
	if recAdmin.Code != http.StatusOK {
		t.Errorf("Expected Admin to have access to pembayaran, got %d", recAdmin.Code)
	}

	// 4. Pengurus koperasi boleh melihat pembayaran siapa pun di koperasinya
	tokenPengurus, _ := env.createAndLoginUser(t, "pengurus")
	reqPengurus := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/%s", payIDA), nil)
	reqPengurus.Header.Set("Authorization", "Bearer "+tokenPengurus)
	recPengurus := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPengurus, reqPengurus)
	if recPengurus.Code != http.StatusOK {
		t.Errorf("Expected Pengurus to have access to pembayaran, got %d", recPengurus.Code)
	}

	// 5. Pemilik aslinya (Anggota A) BOLEH melihat pembayarannya sendiri
	reqOwner := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/%s", payIDA), nil)
	reqOwner.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recOwner := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recOwner, reqOwner)
	if recOwner.Code != http.StatusOK {
		t.Errorf("Expected original payer to have access to pembayaran, got %d", recOwner.Code)
	}
}

func TestS2_QRIS_FeatureFlag_And_Pilot_Validation(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// 1. Uji default feature flag QRISDynamicEnabled == false
	billID := env.CreateBill(t, env.AnggotaID, 60000)
	bodyPay, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{billID},
	})
	reqPay := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyPay))
	reqPay.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqPay.Header.Set("Content-Type", "application/json")
	recPay := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPay, reqPay)

	if recPay.Code != http.StatusCreated {
		t.Fatalf("Buat pembayaran QRIS gagal: %d (body: %s)", recPay.Code, recPay.Body.String())
	}

	var resDefault struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recPay.Body.Bytes(), &resDefault)

	// Karena QRIS_DYNAMIC_ENABLED default false:
	if resDefault.Data.IsAutoLockNominal {
		t.Errorf("Expected IsAutoLockNominal to be false when QRIS_DYNAMIC_ENABLED is false")
	}
	if resDefault.Data.QRISPayloadDinamis != "" {
		t.Errorf("Expected QRISPayloadDinamis to be empty when flag is disabled, got %s", resDefault.Data.QRISPayloadDinamis)
	}

	// 2. Uji ketika QRIS_DYNAMIC_ENABLED diaktifkan
	cfgDynamic := env.Config
	cfgDynamic.QRISDynamicEnabled = true
	serverDynamic := api.NewServer(env.Pool, cfgDynamic)

	billID2 := env.CreateBill(t, env.AnggotaID, 65000)
	bodyPay2, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{billID2},
	})
	reqPay2 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyPay2))
	reqPay2.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqPay2.Header.Set("Content-Type", "application/json")
	recPay2 := httptest.NewRecorder()
	serverDynamic.Router().ServeHTTP(recPay2, reqPay2)

	if recPay2.Code != http.StatusCreated {
		t.Fatalf("Buat pembayaran QRIS dinamis gagal: %d (body: %s)", recPay2.Code, recPay2.Body.String())
	}

	var resDynamic struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recPay2.Body.Bytes(), &resDynamic)

	if !resDynamic.Data.IsAutoLockNominal {
		t.Errorf("Expected IsAutoLockNominal to be true when QRIS_DYNAMIC_ENABLED is true")
	}
	if resDynamic.Data.QRISPayloadDinamis == "" {
		t.Errorf("Expected QRISPayloadDinamis to be generated when flag is enabled")
	}

	// 3. Uji Admin Daftar Rekening QRIS: Validasi Payload EMVCo & CRC Masukan
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	validPrefix := fmt.Sprintf("000201010211%s5204541153033605802ID5912Kopdes Juara6007Bandung6304", tag26)
	validCRC := qris.CalculateCRC16(validPrefix)
	validPayload := validPrefix + validCRC
	corruptPayload := validPrefix + "DEAD"

	// 3a. Tolak jika payload CRC rusak -> 400 Bad Request
	bodyBadCRC, _ := json.Marshal(handlers.AdminDaftarRekeningQRISRequest{
		NamaMerchant:         "Kopdes Juara",
		NMID:                 "ID1099887766554",
		NamaBankPJP:          "Bank Mandiri",
		NoRekeningSettlement: "1234567890",
		NamaPemilikRekening:  "Koperasi Desa Juara",
		IsAtasNamaBadanHukum: true,
		JenisPemilik:         "badan_hukum_koperasi",
		QRISPayloadStatis:    corruptPayload,
	})
	reqBadCRC := httptest.NewRequest("POST", "/api/v1/koperasi/rekening-qris", bytes.NewReader(bodyBadCRC))
	reqBadCRC.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqBadCRC.Header.Set("Content-Type", "application/json")
	recBadCRC := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recBadCRC, reqBadCRC)

	if recBadCRC.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for corrupted CRC, got %d", recBadCRC.Code)
	}

	// 3b. Tolak perorangan_pengurus jika pilot mode non-aktif -> 403 Forbidden
	bodyPersonalNoPilot, _ := json.Marshal(handlers.AdminDaftarRekeningQRISRequest{
		NamaMerchant:         "Toko Pribadi Pengurus",
		NMID:                 "ID1099887766555",
		NamaBankPJP:          "Bank BRI",
		NoRekeningSettlement: "0987654321",
		NamaPemilikRekening:  "Budi Pengurus",
		IsAtasNamaBadanHukum: false,
		JenisPemilik:         "perorangan_pengurus",
		QRISPayloadStatis:    validPayload,
	})
	reqPersonalNoPilot := httptest.NewRequest("POST", "/api/v1/koperasi/rekening-qris", bytes.NewReader(bodyPersonalNoPilot))
	reqPersonalNoPilot.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqPersonalNoPilot.Header.Set("Content-Type", "application/json")
	recPersonalNoPilot := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPersonalNoPilot, reqPersonalNoPilot)

	if recPersonalNoPilot.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for perorangan_pengurus when pilot disabled, got %d", recPersonalNoPilot.Code)
	}

	// 3c. Izinkan perorangan_pengurus jika ALLOW_PERSONAL_PAYMENT_PILOT diaktifkan -> 200/201 OK
	cfgPilot := env.Config
	cfgPilot.AllowPersonalPaymentPilot = true
	serverPilot := api.NewServer(env.Pool, cfgPilot)

	reqPersonalPilot := httptest.NewRequest("POST", "/api/v1/koperasi/rekening-qris", bytes.NewReader(bodyPersonalNoPilot))
	reqPersonalPilot.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqPersonalPilot.Header.Set("Content-Type", "application/json")
	recPersonalPilot := httptest.NewRecorder()
	serverPilot.Router().ServeHTTP(recPersonalPilot, reqPersonalPilot)

	if recPersonalPilot.Code != http.StatusOK && recPersonalPilot.Code != http.StatusCreated {
		t.Errorf("Expected success for perorangan_pengurus when pilot enabled, got %d (body: %s)", recPersonalPilot.Code, recPersonalPilot.Body.String())
	}

	// 3d. Pendaftaran resmi badan_hukum_koperasi dengan QRIS valid -> 200/201 OK
	bodyLegal, _ := json.Marshal(handlers.AdminDaftarRekeningQRISRequest{
		NamaMerchant:         "Kopdes Juara",
		NMID:                 "ID1099887766556",
		NamaBankPJP:          "Bank Mandiri",
		NoRekeningSettlement: "1234567890",
		NamaPemilikRekening:  "Koperasi Desa Juara",
		IsAtasNamaBadanHukum: true,
		JenisPemilik:         "badan_hukum_koperasi",
		QRISPayloadStatis:    validPayload,
	})
	reqLegal := httptest.NewRequest("POST", "/api/v1/koperasi/rekening-qris", bytes.NewReader(bodyLegal))
	reqLegal.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqLegal.Header.Set("Content-Type", "application/json")
	recLegal := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLegal, reqLegal)

	if recLegal.Code != http.StatusOK && recLegal.Code != http.StatusCreated {
		t.Errorf("Expected success for badan_hukum_koperasi, got %d (body: %s)", recLegal.Code, recLegal.Body.String())
	}
}

func TestS3_Deployment_Hardening_And_CORS(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// 1. CORS Preflight & Header Sanitization (Hapus X-Koperasi-ID dari AllowedHeaders)
	reqPreflight := httptest.NewRequest("OPTIONS", "/api/v1/pembayaran/qris", nil)
	reqPreflight.Header.Set("Origin", "https://app.kopdes.id")
	reqPreflight.Header.Set("Access-Control-Request-Method", "POST")
	recPreflight := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPreflight, reqPreflight)

	allowHeaders := recPreflight.Header().Get("Access-Control-Allow-Headers")
	if strings.Contains(allowHeaders, "X-Koperasi-ID") {
		t.Errorf("SECURITY DEFECT: AllowedHeaders must NOT expose 'X-Koperasi-ID' header, got: %s", allowHeaders)
	}

	// 2. Wildcard Origin dilarang mengirimkan Access-Control-Allow-Credentials: true (W3C Spec)
	cfgWildcard := env.Config
	cfgWildcard.AllowedOrigins = []string{"*"}
	srvWildcard := api.NewServer(env.Pool, cfgWildcard)

	reqWild := httptest.NewRequest("OPTIONS", "/api/v1/pembayaran/qris", nil)
	reqWild.Header.Set("Origin", "https://untrusted-site.com")
	reqWild.Header.Set("Access-Control-Request-Method", "POST")
	recWild := httptest.NewRecorder()
	srvWildcard.Router().ServeHTTP(recWild, reqWild)

	if recWild.Header().Get("Access-Control-Allow-Credentials") == "true" {
		t.Errorf("SECURITY DEFECT: Access-Control-Allow-Credentials must NOT be true when origin is wildcard '*'")
	}

	// 3. Batas ukuran body permintaan (BodyLimit)
	// Server default membatasi ukuran body 10 MB. Kirim payload berukuran 11 MB
	hugeReader := io.LimitReader(bytes.NewReader(make([]byte, 11*1024*1024)), 11*1024*1024)
	reqHuge := httptest.NewRequest("POST", "/api/v1/auth/login", hugeReader)
	reqHuge.Header.Set("Content-Type", "application/json")
	recHuge := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recHuge, reqHuge)

	// MaxBytesReader harus memicu error pembacaan (bukan panic / 200)
	if recHuge.Code == http.StatusOK {
		t.Errorf("Expected failure when payload exceeds BodyLimit, got status 200")
	}
}

func TestP1_1_Pendaftaran_KYC_MakerChecker_And_KasMutasi(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// 1. Pendaftaran Anggota Baru (Calon Anggota)
	nikInput := "3201123456780001"
	regBody, _ := json.Marshal(handlers.AjukanPendaftaranRequest{
		NIK:          nikInput,
		NamaLengkap:  "Budi Santoso",
		TempatLahir:  "Bogor",
		TanggalLahir: "1990-05-15",
		JenisKelamin: "L",
		AlamatKTP:    "Jl. Raya Desa No. 10",
		Pekerjaan:    "Petani",
	})

	reqReg := httptest.NewRequest("POST", "/api/v1/pendaftaran", bytes.NewReader(regBody))
	reqReg.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqReg.Header.Set("Content-Type", "application/json")
	recReg := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recReg, reqReg)

	if recReg.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for Pendaftaran, got %d: %s", recReg.Code, recReg.Body.String())
	}

	var resReg struct {
		Data struct {
			PendaftaranID uuid.UUID `json:"pendaftaran_id"`
			NoRegistrasi  string    `json:"no_registrasi"`
			Status        string    `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recReg.Body.Bytes(), &resReg); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	pendaftaranID := resReg.Data.PendaftaranID
	if pendaftaranID == uuid.Nil || !strings.HasPrefix(resReg.Data.NoRegistrasi, "REG") {
		t.Fatalf("Invalid pendaftaran response data: %+v", resReg.Data)
	}

	// Verifikasi di DB: NIK tersimpan terenkripsi dengan AES-256-GCM (prefix v1:k1:) dan hash blind index
	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())

	var (
		dbNikEnkripsi []byte
		dbNikHash     string
	)
	err = conn.QueryRow(ctx, "SELECT nik_enkripsi, nik_hash FROM pendaftaran_anggota WHERE id = $1", pendaftaranID).Scan(&dbNikEnkripsi, &dbNikHash)
	if err != nil {
		t.Fatalf("Failed to query pendaftaran from DB: %v", err)
	}

	if !strings.HasPrefix(string(dbNikEnkripsi), "v1:k1:") {
		t.Errorf("SECURITY DEFECT: nik_enkripsi must have version/keyID prefix 'v1:k1:', got %s", string(dbNikEnkripsi))
	}
	if dbNikHash == nikInput {
		t.Errorf("SECURITY DEFECT: nik_hash must be a blind index HMAC, not plain NIK")
	}

	// 2. Uji Duplikasi NIK (Wajib 409 Conflict)
	reqDup := httptest.NewRequest("POST", "/api/v1/pendaftaran", bytes.NewReader(regBody))
	reqDup.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqDup.Header.Set("Content-Type", "application/json")
	recDup := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recDup, reqDup)

	if recDup.Code != http.StatusConflict {
		t.Errorf("Expected 409 Conflict for duplicate NIK, got %d: %s", recDup.Code, recDup.Body.String())
	}

	// 3. Upload Dokumen KYC
	// 3a. Tolak file palsu / bukan image nyata (magic bytes mismatch)
	var bFake bytes.Buffer
	wFake := multipart.NewWriter(&bFake)
	_ = wFake.WriteField("jenis_dokumen", "ktp")
	partFake, _ := wFake.CreateFormFile("file", "fake.jpg")
	_, _ = partFake.Write([]byte("INI_BUKAN_GAMBAR_ASLI_TETAPI_TEKS_BIASA"))
	_ = wFake.Close()

	reqFakeDoc := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pendaftaran/%s/dokumen", pendaftaranID), &bFake)
	reqFakeDoc.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqFakeDoc.Header.Set("Content-Type", wFake.FormDataContentType())
	recFakeDoc := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recFakeDoc, reqFakeDoc)

	if recFakeDoc.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for fake JPEG without magic bytes, got %d: %s", recFakeDoc.Code, recFakeDoc.Body.String())
	}

	// 3b. Sukses upload file JPEG valid dengan header magic bytes
	var bValid bytes.Buffer
	wValid := multipart.NewWriter(&bValid)
	_ = wValid.WriteField("jenis_dokumen", "ktp")
	partValid, _ := wValid.CreateFormFile("file", "ktp_asli.jpg")
	// Header JPEG valid (FF D8 FF E0 ...)
	validJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x01, 0x00, 0x60}
	_, _ = partValid.Write(validJPEG)
	_ = wValid.Close()

	reqValidDoc := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pendaftaran/%s/dokumen", pendaftaranID), &bValid)
	reqValidDoc.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqValidDoc.Header.Set("Content-Type", wValid.FormDataContentType())
	recValidDoc := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recValidDoc, reqValidDoc)

	if recValidDoc.Code != http.StatusCreated {
		t.Errorf("Expected 201 Created for valid JPEG KYC upload, got %d: %s", recValidDoc.Code, recValidDoc.Body.String())
	}

	// 4. Anti-IDOR & Masking NIK
	// 4a. Buat user lain (Anggota B)
	otherUserID := uuid.New()
	otherEmail := fmt.Sprintf("other_%s@kopdes.id", uuid.New().String()[:8])
	otherHash := security.HashBlindIndex(otherEmail, []byte(env.Config.BlindIndexSecret))
	otherPwdHash, _ := security.HashPassword("OtherPass123!")

	_, _ = conn.Exec(ctx, "RESET ROLE;")
	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'Other User');
	`, otherUserID, []byte("enkripsi"), otherHash, otherPwdHash)
	if err != nil {
		t.Fatalf("Failed to seed other user: %v", err)
	}

	var roleAnggotaID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'anggota'").Scan(&roleAnggotaID)
	_, _ = conn.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, koperasi_id)
		VALUES ($1, $2, $3);
	`, otherUserID, roleAnggotaID, env.KoperasiID)

	_, _ = conn.Exec(ctx, "SET ROLE kopdes_app;")

	bodyOtherLogin, _ := json.Marshal(handlers.LoginRequest{Email: otherEmail, Password: "OtherPass123!"})
	reqOtherLogin := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyOtherLogin))
	recOtherLogin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recOtherLogin, reqOtherLogin)
	var resOtherLogin struct {
		Data handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recOtherLogin.Body.Bytes(), &resOtherLogin)
	otherToken := resOtherLogin.Data.AccessToken

	// User B mencoba mengakses pendaftaran milik User A -> Wajib 404 (Anti-IDOR)
	reqIDOR := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pendaftaran/%s", pendaftaranID), nil)
	reqIDOR.Header.Set("Authorization", "Bearer "+otherToken)
	recIDOR := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recIDOR, reqIDOR)

	if recIDOR.Code != http.StatusNotFound {
		t.Errorf("SECURITY DEFECT (IDOR): User B should NOT be able to view User A's pendaftaran, expected 404, got %d", recIDOR.Code)
	}

	// User A membaca pendaftarannya sendiri -> 200 OK dengan NIK dimasking (3201********0001)
	reqSelf := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pendaftaran/%s", pendaftaranID), nil)
	reqSelf.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recSelf := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recSelf, reqSelf)

	if recSelf.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for own pendaftaran, got %d: %s", recSelf.Code, recSelf.Body.String())
	}

	var resSelf struct {
		Data struct {
			NIKMasked string `json:"nik_masked"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recSelf.Body.Bytes(), &resSelf)
	if resSelf.Data.NIKMasked != "3201********0001" {
		t.Errorf("SECURITY DEFECT: Expected masked NIK '3201********0001', got '%s'", resSelf.Data.NIKMasked)
	}

	// 5. Maker-Checker Verifikasi Pendaftaran
	// 5a. Pemohon mencoba memverifikasi pendaftarannya sendiri -> Wajib 403 Forbidden
	bodyVerifSelf, _ := json.Marshal(handlers.VerifikasiPendaftaranRequest{
		Status:           "disetujui",
		CatatanPerbaikan: "Verifikasi sendiri",
	})
	reqVerifSelf := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pendaftaran/%s/verifikasi", pendaftaranID), bytes.NewReader(bodyVerifSelf))
	reqVerifSelf.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqVerifSelf.Header.Set("Content-Type", "application/json")
	recVerifSelf := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerifSelf, reqVerifSelf)

	if recVerifSelf.Code != http.StatusForbidden {
		t.Errorf("SECURITY DEFECT (Maker-Checker): Applicant must NOT be able to verify own pendaftaran, expected 403, got %d", recVerifSelf.Code)
	}

	// 5b. Admin memverifikasi pendaftaran -> 200 OK & nomor anggota terbuat aman konkurensi
	bodyVerifAdmin, _ := json.Marshal(handlers.VerifikasiPendaftaranRequest{
		Status:           "disetujui",
		CatatanPerbaikan: "Dokumen KTP valid dan lengkap",
	})
	reqVerifAdmin := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pendaftaran/%s/verifikasi", pendaftaranID), bytes.NewReader(bodyVerifAdmin))
	reqVerifAdmin.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqVerifAdmin.Header.Set("Content-Type", "application/json")
	recVerifAdmin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerifAdmin, reqVerifAdmin)

	if recVerifAdmin.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for admin verification, got %d: %s", recVerifAdmin.Code, recVerifAdmin.Body.String())
	}

	var resVerif struct {
		Data struct {
			Status    string `json:"status"`
			NoAnggota string `json:"no_anggota"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recVerifAdmin.Body.Bytes(), &resVerif)
	if !strings.HasPrefix(resVerif.Data.NoAnggota, "ANG") {
		t.Errorf("Expected valid no_anggota starting with 'ANG', got '%s'", resVerif.Data.NoAnggota)
	}

	// 6. Kas Mutasi List Endpoint (P1-1)
	reqKas := httptest.NewRequest("GET", "/api/v1/kas/mutasi", nil)
	reqKas.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recKas := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recKas, reqKas)

	if recKas.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for kas mutasi list, got %d: %s", recKas.Code, recKas.Body.String())
	}
}

func TestP1_4_Operasional_Metrics_And_Idempotency(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	// 1. Uji Endpoint Prometheus Metrics (GET /metrics)
	reqMetrics := httptest.NewRequest("GET", "/metrics", nil)
	recMetrics := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recMetrics, reqMetrics)

	if recMetrics.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /metrics, got %d", recMetrics.Code)
	}

	bodyMetrics := recMetrics.Body.String()
	requiredMetrics := []string{
		"kopdes_uptime_seconds",
		"kopdes_pembayaran_antrean_verifikasi",
		"kopdes_pembayaran_slot_kode_unik_aktif",
		"kopdes_job_gagal_total",
		"kopdes_partisi_tersisa",
	}

	for _, m := range requiredMetrics {
		if !strings.Contains(bodyMetrics, m) {
			t.Errorf("Prometheus metrics missing expected metric '%s'", m)
		}
	}

	// 2. Uji Idempotency-Key pada POST /api/v1/pembayaran/qris
	billID := env.CreateBill(t, env.AnggotaID, 85000)
	bodyPay, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{billID},
	})

	idempKey := fmt.Sprintf("idemp-test-%d", time.Now().UnixNano())

	// Permintaan Pertama
	reqPay1 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyPay))
	reqPay1.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqPay1.Header.Set("Content-Type", "application/json")
	reqPay1.Header.Set("Idempotency-Key", idempKey)
	recPay1 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPay1, reqPay1)

	if recPay1.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for first payment, got %d: %s", recPay1.Code, recPay1.Body.String())
	}

	var res1 struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	if err := json.Unmarshal(recPay1.Body.Bytes(), &res1); err != nil {
		t.Fatalf("Parse response 1 failed: %v", err)
	}

	// Permintaan Kedua dengan Idempotency-Key yang identik
	reqPay2 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyPay))
	reqPay2.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqPay2.Header.Set("Content-Type", "application/json")
	reqPay2.Header.Set("Idempotency-Key", idempKey)
	recPay2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPay2, reqPay2)

	if recPay2.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for idempotent replay, got %d: %s", recPay2.Code, recPay2.Body.String())
	}

	var res2 struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	if err := json.Unmarshal(recPay2.Body.Bytes(), &res2); err != nil {
		t.Fatalf("Parse response 2 failed: %v", err)
	}

	// Pembayaran ID dan NoPembayaran harus identik murni (idempoten)
	if res1.Data.PembayaranID != res2.Data.PembayaranID {
		t.Errorf("IDEMPOTENCY FAILED: PembayaranID differs: %s vs %s", res1.Data.PembayaranID, res2.Data.PembayaranID)
	}
	if res1.Data.NoPembayaran != res2.Data.NoPembayaran {
		t.Errorf("IDEMPOTENCY FAILED: NoPembayaran differs: %s vs %s", res1.Data.NoPembayaran, res2.Data.NoPembayaran)
	}
}

func TestP1_1_PDP_30Day_Purge(t *testing.T) {
	env := setupAPITestEnv(t)
	defer env.Pool.Close()

	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire conn failed: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())

	// Sisipkan data pendaftaran ditolak bertanggal 35 hari yang lalu
	pendaftaranID := uuid.New()
	tglLama := time.Now().Add(-35 * 24 * time.Hour)
	nikHash := hashSHA256(fmt.Sprintf("3201%d", time.Now().UnixNano()))

	// Ambil wilayah_desa_id default
	var wilayahID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT wilayah_desa_id FROM koperasi WHERE id = $1", env.KoperasiID).Scan(&wilayahID)

	_, err = conn.Exec(ctx, `
		INSERT INTO pendaftaran_anggota (
			id, koperasi_id, user_id, no_registrasi,
			nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir,
			tanggal_lahir_enkripsi, jenis_kelamin, alamat_ktp_enkripsi,
			wilayah_desa_id, status, diajukan_oleh, alasan_ditolak, updated_at, created_by
		) VALUES (
			$1, $2, $3, 'REG-PURGE-01',
			'v1:k1:dummy', $4, 'Calon Ditolak', 'Bogor',
			'v1:k1:dummy', 'L', 'v1:k1:dummy',
			$5, 'ditolak', $3, 'Persyaratan tidak memenuhi kriteria', $6, $3
		);
	`, pendaftaranID, env.KoperasiID, env.AnggotaUserID, nikHash, wilayahID, tglLama)
	if err != nil {
		t.Fatalf("Seed pendaftaran ditolak failed: %v", err)
	}

	// Panggil endpoint pembersihan PDP via admin
	reqPurge := httptest.NewRequest("POST", "/api/v1/pendaftaran/purge-ditolak", nil)
	reqPurge.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recPurge := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPurge, reqPurge)

	if recPurge.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for purge-ditolak, got %d: %s", recPurge.Code, recPurge.Body.String())
	}

	// Verifikasi di DB: data pendaftaran ditolak > 30 hari telah terhapus
	var exists bool
	_ = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pendaftaran_anggota WHERE id = $1);", pendaftaranID).Scan(&exists)
	if exists {
		t.Errorf("PDP RETENTION VIOLATION: Berkas pendaftaran ditolak > 30 hari seharusnya terhapus, tetapi masih ditemukan di DB")
	}
}

func TestTask2_MetricsEndpoint_PartitionMetrics(t *testing.T) {
	env := setupAPITestEnv(t)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /metrics, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	requiredSubstrings := []string{
		`kopdes_partisi_bulan_tersisa{tabel="audit_log"}`,
		`kopdes_partisi_bulan_tersisa{tabel="kas_mutasi"}`,
		`kopdes_partisi_bulan_tersisa{tabel="stok_mutasi"}`,
		`kopdes_partisi_bulan_tersisa{tabel="notifikasi"}`,
		`kopdes_partisi_default_row_count{tabel="audit_log"} 0`,
		`kopdes_partisi_default_row_count{tabel="kas_mutasi"} 0`,
		`kopdes_partisi_default_row_count{tabel="stok_mutasi"} 0`,
		`kopdes_partisi_default_row_count{tabel="notifikasi"} 0`,
		`kopdes_partisi_peringatan 0`,
	}

	for _, sub := range requiredSubstrings {
		if !strings.Contains(body, sub) {
			t.Errorf("Metrics response missing expected substring: %q\nResponse body:\n%s", sub, body)
		}
	}
}

func TestTask3_PDPPurge_SecurityTenantValidation(t *testing.T) {
	env := setupAPITestEnv(t)
	ctx := context.Background()

	pool := getTestPool(t)
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire conn failed: %v", err)
	}
	defer conn.Release()

	// Set tenant context A
	tenantA := env.KoperasiID
	tenantB := uuid.New()
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", tenantA.String())
	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false);")

	// Panggil fn_purge_rejected_pendaftaran_pdp dengan tenant B (mismatch)
	// Wajib ditolak dengan exception SQLSTATE 42501
	var count int
	err = conn.QueryRow(ctx, "SELECT fn_purge_rejected_pendaftaran_pdp($1, 30);", tenantB).Scan(&count)
	if err == nil {
		t.Fatalf("SECURITY VIOLATION: fn_purge_rejected_pendaftaran_pdp seharusnya menolak tenant mismatch (tenantA vs tenantB), tetapi lolos!")
	}
	if !strings.Contains(err.Error(), "42501") && !strings.Contains(strings.ToLower(err.Error()), "akses ditolak") {
		t.Fatalf("Expected SQLSTATE 42501 or Akses Ditolak, got: %v", err)
	}
	t.Logf("Sukses: Pemanggilan lintas tenant berhasil ditolak: %v", err)
}

func TestTask3_SIMKOPDES_Table_And_Adapter_Restored(t *testing.T) {
	ctx := context.Background()
	pool := getTestPool(t)
	defer pool.Close()

	// 1. Verifikasi tabel sinkronisasi_simkopdes ada di database
	var tableExists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_name = 'sinkronisasi_simkopdes'
		);
	`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("Gagal memeriksa keberadaan tabel sinkronisasi_simkopdes: %v", err)
	}
	if !tableExists {
		t.Fatalf("Tabel sinkronisasi_simkopdes belum dipulihkan di database!")
	}

	// 2. Verifikasi adapter stub terpasang dan berfungsi
	adapter := simkopdes.NewStubAdapter(pool)
	_, err = adapter.EnqueueReport(ctx, uuid.New(), "rekap_anggota", map[string]interface{}{"total": 10})
	if err != simkopdes.ErrSIMKOPDESDeScoped {
		t.Errorf("Expected ErrSIMKOPDESDeScoped from stub adapter, got: %v", err)
	}
}

func TestTask3_PDPPurge_AuditLog_WithoutPII_And_ConfigRetention(t *testing.T) {
	env := setupAPITestEnv(t)
	ctx := context.Background()

	pool := getTestPool(t)
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire conn failed: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())

	// 1. Ambil wilayah default
	var wilayahID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT wilayah_desa_id FROM koperasi WHERE id = $1", env.KoperasiID).Scan(&wilayahID)

	pendaftaranID := uuid.New()
	dokumenID := uuid.New()
	tglLama := time.Now().Add(-35 * 24 * time.Hour)
	nikHash := hashSHA256(fmt.Sprintf("3201%d", time.Now().UnixNano()))

	// 2. Buat data pendaftaran ditolak
	_, err = conn.Exec(ctx, `
		INSERT INTO pendaftaran_anggota (
			id, koperasi_id, user_id, no_registrasi,
			nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir,
			tanggal_lahir_enkripsi, jenis_kelamin, alamat_ktp_enkripsi,
			wilayah_desa_id, status, diajukan_oleh, alasan_ditolak, updated_at, created_by
		) VALUES (
			$1, $2, $3, 'REG-TASK3-PURGE',
			'v1:k1:dummy', $4, 'Calon Ditolak Task3', 'Bandung',
			'v1:k1:dummy', 'P', 'v1:k1:dummy',
			$5, 'ditolak', $3, 'Syarat kurang lengkap', $6, $3
		);
	`, pendaftaranID, env.KoperasiID, env.AnggotaUserID, nikHash, wilayahID, tglLama)
	if err != nil {
		t.Fatalf("Seed pendaftaran failed: %v", err)
	}

	// 3. Buat dokumen KYC di tabel dokumen dan pendaftaran_dokumen
	storageKey := fmt.Sprintf("pendaftaran/%s/%s/%s_ktp.jpg", env.KoperasiID, pendaftaranID, dokumenID)
	_, err = conn.Exec(ctx, `
		INSERT INTO dokumen (
			id, koperasi_id, nama_file, mime_type, ukuran_byte, storage_key, checksum_sha256, klasifikasi_data, retensi_sampai, created_by
		) VALUES (
			$1, $2, 'ktp.jpg', 'image/jpeg', 1024, $3, 'dummychecksum', 'rahasia_pii', CURRENT_DATE + INTERVAL '1 year', $4
		);
	`, dokumenID, env.KoperasiID, storageKey, env.AnggotaUserID)
	if err != nil {
		t.Fatalf("Seed dokumen failed: %v", err)
	}

	_, err = conn.Exec(ctx, `
		INSERT INTO pendaftaran_dokumen (
			pendaftaran_id, dokumen_id, koperasi_id, jenis_dokumen, storage_key, checksum_sha256, created_by
		) VALUES (
			$1, $2, $3, 'ktp', $4, 'dummychecksum', $5
		);
	`, pendaftaranID, dokumenID, env.KoperasiID, storageKey, env.AnggotaUserID)
	if err != nil {
		t.Fatalf("Seed pendaftaran_dokumen failed: %v", err)
	}

	_, err = conn.Exec(ctx, `
		INSERT INTO pendaftaran_status_log (
			id, koperasi_id, pendaftaran_id, status_lama, status_baru, created_by
		) VALUES (
			$1, $2, $3, 'dalam_verifikasi', 'ditolak', $4
		);
	`, uuid.New(), env.KoperasiID, pendaftaranID, env.AnggotaUserID)
	if err != nil {
		t.Fatalf("Seed pendaftaran_status_log failed: %v", err)
	}

	// 4. Jalankan endpoint purge-ditolak
	reqPurge := httptest.NewRequest("POST", "/api/v1/pendaftaran/purge-ditolak", nil)
	reqPurge.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recPurge := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPurge, reqPurge)

	if recPurge.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for purge-ditolak, got %d: %s", recPurge.Code, recPurge.Body.String())
	}

	// 5. Verifikasi pendaftaran dan dokumen terhapus
	var existsPendaftaran, existsDokumen bool
	_ = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pendaftaran_anggota WHERE id = $1);", pendaftaranID).Scan(&existsPendaftaran)
	_ = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM dokumen WHERE id = $1);", dokumenID).Scan(&existsDokumen)

	if existsPendaftaran {
		t.Errorf("Pendaftaran harusnya terhapus setelah purge!")
	}
	if existsDokumen {
		t.Errorf("Dokumen KYC harusnya terhapus setelah purge!")
	}

	// 6. Verifikasi audit log tercatat tanpa PII
	var auditAksi string
	var auditDataBaru string
	err = conn.QueryRow(ctx, `
		SELECT aksi, data_baru::text 
		FROM audit_log 
		WHERE koperasi_id = $1 AND nama_tabel = 'pendaftaran_anggota' AND aksi = 'ANONYMIZE'
		ORDER BY created_at DESC LIMIT 1;
	`, env.KoperasiID).Scan(&auditAksi, &auditDataBaru)
	if err != nil {
		t.Fatalf("Audit log untuk purge PDP tidak ditemukan: %v", err)
	}

	// Pastikan tidak ada PII di audit_log
	if strings.Contains(auditDataBaru, "Calon Ditolak Task3") || strings.Contains(auditDataBaru, "Bandung") || strings.Contains(auditDataBaru, nikHash) {
		t.Fatalf("SECURITY VIOLATION: Audit log memuat PII calon anggota: %s", auditDataBaru)
	}
	if !strings.Contains(auditDataBaru, "PERLU VERIFIKASI HUKUM") {
		t.Errorf("Audit log harus memuat catatan PERLU VERIFIKASI HUKUM: %s", auditDataBaru)
	}
	t.Logf("Sukses: Audit log bersih tanpa PII: %s", auditDataBaru)
}

func TestTask4_DatabaseBacked_Idempotency_And_422OnDifferentBody(t *testing.T) {
	env := setupAPITestEnv(t)
	ctx := context.Background()

	pool := getTestPool(t)
	defer pool.Close()

	billID1 := env.CreateBill(t, env.AnggotaID, 50000)
	billID2 := env.CreateBill(t, env.AnggotaID, 75000)

	body1, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{billID1},
	})

	body2, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{billID2},
	})

	idempKey := fmt.Sprintf("idemp-task4-%d", time.Now().UnixNano())

	// 1. Request pertama dengan body1
	req1 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(body1))
	req1.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", idempKey)
	rec1 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for first request, got %d: %s", rec1.Code, rec1.Body.String())
	}

	var res1 struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	if err := json.Unmarshal(rec1.Body.Bytes(), &res1); err != nil {
		t.Fatalf("Parse response 1 failed: %v", err)
	}

	// 2. Verifikasi tabel database idempotency_keys memuat entri ini
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire conn failed: %v", err)
	}
	defer conn.Release()
	_, _ = conn.Exec(ctx, "RESET ROLE;")
	_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", env.KoperasiID.String())

	var existsDB bool
	var reqHashDB string
	var respCodeDB int
	err = conn.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM idempotency_keys WHERE kunci = $1),
		       COALESCE((SELECT request_hash FROM idempotency_keys WHERE kunci = $1), ''),
		       COALESCE((SELECT response_code FROM idempotency_keys WHERE kunci = $1), 0);
	`, idempKey).Scan(&existsDB, &reqHashDB, &respCodeDB)
	if err != nil {
		t.Fatalf("Query idempotency_keys failed: %v", err)
	}
	if !existsDB {
		t.Fatalf("IDEMPOTENCY FAILURE: Entri tidak tersimpan di tabel database idempotency_keys!")
	}
	if respCodeDB != http.StatusCreated {
		t.Errorf("Expected response_code in DB 201, got %d", respCodeDB)
	}

	// 3. Request kedua dengan Idempotency-Key yang SAMA tetapi BODY BERBEDA -> WAJIB 422 Unprocessable Entity
	req2 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", idempKey)
	rec2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("IDEMPOTENCY CONFLICT VIOLATION: Kunci sama dengan body berbeda seharusnya mengembalikan 422 Unprocessable Entity, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// 4. Request ketiga dengan Idempotency-Key yang SAMA dan BODY PERSIS SAMA -> WAJIB 201 dengan cached response
	req3 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(body1))
	req3.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Idempotency-Key", idempKey)
	rec3 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for identical retry, got %d: %s", rec3.Code, rec3.Body.String())
	}

	var res3 struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	if err := json.Unmarshal(rec3.Body.Bytes(), &res3); err != nil {
		t.Fatalf("Parse response 3 failed: %v", err)
	}

	if res3.Data.PembayaranID != res1.Data.PembayaranID {
		t.Errorf("Expected identical PembayaranID for replay: %s vs %s", res3.Data.PembayaranID, res1.Data.PembayaranID)
	}
	if res3.Data.NoPembayaran != res1.Data.NoPembayaran {
		t.Errorf("Expected identical NoPembayaran for replay: %s vs %s", res3.Data.NoPembayaran, res1.Data.NoPembayaran)
	}
	if res3.Data.NominalTotalRp != res1.Data.NominalTotalRp {
		t.Errorf("Expected identical NominalTotalRp for replay: %d vs %d", res3.Data.NominalTotalRp, res1.Data.NominalTotalRp)
	}
	t.Logf("Sukses: Idempotency database-backed dan deteksi beda body (422) terverifikasi!")
}

func TestTask5_PaymentVerification_Reconciliation_EndToEnd(t *testing.T) {
	env := setupAPITestEnv(t)

	// --- 1. Uji CSV Bank Mutation Import & Auto Matching ---
	billID := env.CreateBill(t, env.AnggotaID, 120000)
	bodyPay, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{billID},
	})

	reqPay := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyPay))
	reqPay.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqPay.Header.Set("Content-Type", "application/json")
	recPay := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPay, reqPay)

	if recPay.Code != http.StatusCreated {
		t.Fatalf("Buat QRIS failed: %d: %s", recPay.Code, recPay.Body.String())
	}
	var resPay struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recPay.Body.Bytes(), &resPay)

	// Klaim bukti bayar
	reqClaim := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/bukti", resPay.Data.PembayaranID), strings.NewReader(`{"referensi_rrn":"RRN-AUTO-MATCH"}`))
	reqClaim.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqClaim.Header.Set("Content-Type", "application/json")
	recClaim := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recClaim, reqClaim)
	if recClaim.Code != http.StatusOK {
		t.Fatalf("Claim bukti failed: %d: %s", recClaim.Code, recClaim.Body.String())
	}

	// Upload CSV mutasi bank dengan nominal_total yang persis sama
	csvContent := fmt.Sprintf("tanggal,keterangan,tipe,nominal,no_rekening\n2026-11-10 10:00:00,QRIS SETTLEMENT / CR,CR,%d.00,1234567890\n", resPay.Data.NominalTotalRp)
	bodyCSV := &bytes.Buffer{}
	writer := multipart.NewWriter(bodyCSV)
	part, _ := writer.CreateFormFile("file", "mutasi.csv")
	part.Write([]byte(csvContent))
	writer.Close()

	reqImport := httptest.NewRequest("POST", "/api/v1/pembayaran/mutasi-bank/impor", bodyCSV)
	reqImport.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqImport.Header.Set("Content-Type", writer.FormDataContentType())
	recImport := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recImport, reqImport)

	if recImport.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for CSV import, got %d: %s", recImport.Code, recImport.Body.String())
	}

	// Verifikasi pembayaran status berubah menjadi 'disetujui' secara otomatis karena matched
	reqStatus := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/%s", resPay.Data.PembayaranID), nil)
	reqStatus.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recStatus := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recStatus, reqStatus)

	var resStatus struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recStatus.Body.Bytes(), &resStatus)
	if resStatus.Data.Status != "disetujui" {
		t.Errorf("Expected status disetujui after auto-matching, got: %s", resStatus.Data.Status)
	}

	// --- 2. Uji Persetujuan Ganda di Atas Ambang Batas (Dual Approval) ---
	// Buat tagihan bernilai tinggi Rp 6.000.000 (di atas default ambang Rp 5.000.000)
	highBillID := env.CreateBill(t, env.AnggotaID, 6000000)
	bodyHigh, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{highBillID},
	})
	reqHigh := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyHigh))
	reqHigh.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqHigh.Header.Set("Content-Type", "application/json")
	recHigh := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recHigh, reqHigh)
	if recHigh.Code != http.StatusCreated {
		t.Fatalf("Buat QRIS high value failed: %d: %s", recHigh.Code, recHigh.Body.String())
	}
	var resHigh struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recHigh.Body.Bytes(), &resHigh)

	// Klaim bukti
	reqClaimHigh := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/bukti", resHigh.Data.PembayaranID), strings.NewReader(`{"referensi_rrn":"RRN-HIGH"}`))
	reqClaimHigh.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqClaimHigh.Header.Set("Content-Type", "application/json")
	recClaimHigh := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recClaimHigh, reqClaimHigh)

	// Persetujuan Pertama oleh Admin
	reqApprove1 := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", resHigh.Data.PembayaranID), strings.NewReader(`{"disetujui":true,"catatan":"Approve 1 ok"}`))
	reqApprove1.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqApprove1.Header.Set("Content-Type", "application/json")
	recApprove1 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recApprove1, reqApprove1)

	if recApprove1.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for first approval, got %d: %s", recApprove1.Code, recApprove1.Body.String())
	}
	if !strings.Contains(recApprove1.Body.String(), "menunggu_persetujuan_kedua") {
		t.Errorf("Expected response indicating menunggu_persetujuan_kedua, got: %s", recApprove1.Body.String())
	}

	// Persetujuan Kedua oleh Admin yang SAMA -> HARUS DITOLAK (Maker-Checker / Dual approval violation)
	reqApproveSelf := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", resHigh.Data.PembayaranID), strings.NewReader(`{"disetujui":true,"catatan":"Approve 2 self"}`))
	reqApproveSelf.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqApproveSelf.Header.Set("Content-Type", "application/json")
	recApproveSelf := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recApproveSelf, reqApproveSelf)

	if recApproveSelf.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for self-dual-approval, got %d: %s", recApproveSelf.Code, recApproveSelf.Body.String())
	}

	// Persetujuan Kedua oleh Pengurus Lain (pengurusToken) -> BERHASIL
	pengurusToken, _ := env.createAndLoginUser(t, "pengurus")
	reqApprove2 := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", resHigh.Data.PembayaranID), strings.NewReader(`{"disetujui":true,"catatan":"Approve 2 verified"}`))
	reqApprove2.Header.Set("Authorization", "Bearer "+pengurusToken)
	reqApprove2.Header.Set("Content-Type", "application/json")
	recApprove2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recApprove2, reqApprove2)

	if recApprove2.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for second approval, got %d: %s", recApprove2.Code, recApprove2.Body.String())
	}

	// --- 3. Uji Laporan Rekonsiliasi Harian ---
	tglHariIni := time.Now().Format("2006-01-02")
	reqReconcile := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/rekonsiliasi/harian?tanggal=%s", tglHariIni), nil)
	reqReconcile.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recReconcile := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recReconcile, reqReconcile)

	if recReconcile.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for reconciliation report, got %d: %s", recReconcile.Code, recReconcile.Body.String())
	}
	if !strings.Contains(recReconcile.Body.String(), "total_tercocokkan") {
		t.Errorf("Reconciliation report missing total_tercocokkan: %s", recReconcile.Body.String())
	}
}









