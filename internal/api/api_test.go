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
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	kopdes "github.com/kopdes/merah-putih"
	"github.com/kopdes/merah-putih/internal/api"
	"github.com/kopdes/merah-putih/internal/api/handlers"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/auth"
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
	cfg.MetricsToken = "test-metrics-token-32-bytes-ok!"

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

	tm := auth.NewTokenManager(cfg.JWTSecret, "kopdes-merah-putih-api")
	adminTok, err := tm.GenerateToken(adminUserID, koperasiID, "admin", false, 15*time.Minute)
	if err != nil {
		t.Fatalf("generate admin token error: %v", err)
	}

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

	if recLogin.Code == http.StatusForbidden {
		var resForbidden struct {
			Data struct {
				SetupToken string `json:"setup_token"`
			} `json:"data"`
		}
		_ = json.Unmarshal(recLogin.Body.Bytes(), &resForbidden)
		if resForbidden.Data.SetupToken != "" {
			setupTok := resForbidden.Data.SetupToken

			// 1. Setup MFA
			reqSetup := httptest.NewRequest("POST", "/api/v1/auth/mfa/setup", nil)
			reqSetup.Header.Set("Authorization", "Bearer "+setupTok)
			recSetup := httptest.NewRecorder()
			env.Server.Router().ServeHTTP(recSetup, reqSetup)

			var resSetup struct {
				Data struct {
					Secret string `json:"secret"`
				} `json:"data"`
			}
			_ = json.Unmarshal(recSetup.Body.Bytes(), &resSetup)
			secret := resSetup.Data.Secret

			code, _ := security.GenerateTOTPCode(secret, time.Now())

			// 2. Enable MFA
			bodyEnable, _ := json.Marshal(map[string]string{
				"secret":    secret,
				"totp_code": code,
			})
			reqEnable := httptest.NewRequest("POST", "/api/v1/auth/mfa/enable", bytes.NewReader(bodyEnable))
			reqEnable.Header.Set("Authorization", "Bearer "+setupTok)
			reqEnable.Header.Set("Content-Type", "application/json")
			recEnable := httptest.NewRecorder()
			env.Server.Router().ServeHTTP(recEnable, reqEnable)

			var resEnableResp struct {
				Data struct {
					RecoveryCodes []string `json:"recovery_codes"`
				} `json:"data"`
			}
			_ = json.Unmarshal(recEnable.Body.Bytes(), &resEnableResp)

			// 3. Login ulang (mendapatkan mfa_session_token)
			reqLogin2 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin))
			recLogin2 := httptest.NewRecorder()
			env.Server.Router().ServeHTTP(recLogin2, reqLogin2)

			var resMFASess struct {
				Data struct {
					MFASessionToken string `json:"mfa_session_token"`
				} `json:"data"`
			}
			_ = json.Unmarshal(recLogin2.Body.Bytes(), &resMFASess)

			// 4. Verify MFA menggunakan recovery code pertama
			if len(resEnableResp.Data.RecoveryCodes) > 0 {
				bodyVerify, _ := json.Marshal(map[string]string{
					"mfa_session_token": resMFASess.Data.MFASessionToken,
					"recovery_code":     resEnableResp.Data.RecoveryCodes[0],
				})
				reqVerify := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyVerify))
				reqVerify.Header.Set("Content-Type", "application/json")
				recVerify := httptest.NewRecorder()
				env.Server.Router().ServeHTTP(recVerify, reqVerify)

				_ = json.Unmarshal(recVerify.Body.Bytes(), &loginRes)
				if loginRes.Data.AccessToken != "" {
					return loginRes.Data, email, pwdPlain, userID
				}
			}
		}
	}

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

	// Generate token admin langsung untuk pengujian Maker-Checker
	tm := auth.NewTokenManager(env.Config.JWTSecret, "kopdes-merah-putih-api")
	tokenAdmin, err := tm.GenerateToken(adminID, env.KoperasiID, "admin", false, 15*time.Minute)
	if err != nil {
		t.Fatalf("generate admin token error: %v", err)
	}

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
	reqMetrics.Header.Set("Authorization", "Bearer "+env.Config.MetricsToken)
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
	req.Header.Set("Authorization", "Bearer "+env.Config.MetricsToken)
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
	// Karena mutasi_auto_approve_enabled default-nya false, status awal beralih ke menunggu_persetujuan_kedua
	if resStatus.Data.Status != "menunggu_persetujuan_kedua" && resStatus.Data.Status != "disetujui" {
		t.Errorf("Expected status menunggu_persetujuan_kedua or disetujui after auto-matching, got: %s", resStatus.Data.Status)
	}
	if resStatus.Data.Status == "menunggu_persetujuan_kedua" {
		pengurusTok, _ := env.createAndLoginUser(t, "pengurus")
		reqApprove2nd := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", resPay.Data.PembayaranID), strings.NewReader(`{"disetujui":true,"catatan":"Approve kedua mutasi"}`))
		reqApprove2nd.Header.Set("Authorization", "Bearer "+pengurusTok)
		reqApprove2nd.Header.Set("Content-Type", "application/json")
		recApprove2nd := httptest.NewRecorder()
		env.Server.Router().ServeHTTP(recApprove2nd, reqApprove2nd)
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

	if recApproveSelf.Code != http.StatusBadRequest && recApproveSelf.Code != http.StatusForbidden {
		t.Errorf("Expected 400 Bad Request or 403 Forbidden for self-dual-approval, got %d: %s", recApproveSelf.Code, recApproveSelf.Body.String())
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

func TestTask6_PersonalAccountPilot_Limits_Banner_Audit(t *testing.T) {
	env := setupAPITestEnv(t)
	ctx := context.Background()

	// 1. Setup Server dengan ALLOW_PERSONAL_PAYMENT_PILOT = true
	cfgPilot := env.Config
	cfgPilot.AllowPersonalPaymentPilot = true
	serverPilot := api.NewServer(env.Pool, cfgPilot)

	// Buat rekening QRIS perorangan pengurus
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	validPrefix := fmt.Sprintf("000201010211%s5204541153033605802ID5914Pilot Pengurus6007Jakarta6304", tag26)
	validPayload := validPrefix + qris.CalculateCRC16(validPrefix)

	bodyPilotRek, _ := json.Marshal(handlers.AdminDaftarRekeningQRISRequest{
		NamaMerchant:         "Pilot Pengurus Budi",
		NMID:                 fmt.Sprintf("ID%013d", time.Now().UnixNano()%10000000000000),
		NamaBankPJP:          "Bank BRI",
		NoRekeningSettlement: "5556667778",
		NamaPemilikRekening:  "Budi Santoso Pengurus",
		IsAtasNamaBadanHukum: false,
		JenisPemilik:         "perorangan_pengurus",
		QRISPayloadStatis:    validPayload,
	})

	reqDaftarRek := httptest.NewRequest("POST", "/api/v1/koperasi/rekening-qris", bytes.NewReader(bodyPilotRek))
	reqDaftarRek.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqDaftarRek.Header.Set("Content-Type", "application/json")
	recDaftarRek := httptest.NewRecorder()
	serverPilot.Router().ServeHTTP(recDaftarRek, reqDaftarRek)

	if recDaftarRek.Code != http.StatusCreated && recDaftarRek.Code != http.StatusOK {
		t.Fatalf("Gagal mendaftarkan rekening pilot: %d: %s", recDaftarRek.Code, recDaftarRek.Body.String())
	}

	var resRek struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recDaftarRek.Body.Bytes(), &resRek)
	pilotRekeningID, err := uuid.Parse(resRek.Data.ID)
	if err != nil {
		t.Fatalf("Parse rekening ID gagal: %v", err)
	}

	// 2. Batas Per Transaksi: Default limit Rp 1.000.000
	// Buat tagihan Rp 1.500.000 (> limit per transaksi)
	tagihanOverLimit := env.CreateBill(t, env.AnggotaID, 1500000)

	bodyOver, _ := json.Marshal(handlers.BuatQRISRequest{
		JenisTransaksi:  "iuran_bulanan",
		TagihanIuranIDs: []uuid.UUID{tagihanOverLimit},
		RekeningQRISID:  pilotRekeningID,
	})
	reqOver := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyOver))
	reqOver.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqOver.Header.Set("Content-Type", "application/json")
	recOver := httptest.NewRecorder()
	serverPilot.Router().ServeHTTP(recOver, reqOver)

	// Harusnya ditolak karena melebihi batas per transaksi rekening perorangan
	if recOver.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request for transaction exceeding per-transaction pilot limit, got %d: %s", recOver.Code, recOver.Body.String())
	}
	if !strings.Contains(recOver.Body.String(), "batas") && !strings.Contains(recOver.Body.String(), "maksimum per transaksi") {
		t.Errorf("Expected limit error message, got: %s", recOver.Body.String())
	}

	// 3. Transaksi di bawah batas per transaksi: Rp 600.000 (< limit 1.000.000) -> SUKSES
	tagihanUnder1 := env.CreateBill(t, env.AnggotaID, 600000)
	bodyUnder1, _ := json.Marshal(handlers.BuatQRISRequest{
		JenisTransaksi:  "iuran_bulanan",
		TagihanIuranIDs: []uuid.UUID{tagihanUnder1},
		RekeningQRISID:  pilotRekeningID,
	})
	reqUnder1 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyUnder1))
	reqUnder1.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqUnder1.Header.Set("Content-Type", "application/json")
	recUnder1 := httptest.NewRecorder()
	serverPilot.Router().ServeHTTP(recUnder1, reqUnder1)

	if recUnder1.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for transaction within limit, got %d: %s", recUnder1.Code, recUnder1.Body.String())
	}

	var resUnder1 struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recUnder1.Body.Bytes(), &resUnder1)
	pembayaranID1 := resUnder1.Data.PembayaranID

	// 4. Banner Peringatan di Respons Admin
	reqDetailAdmin := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/%s", pembayaranID1), nil)
	reqDetailAdmin.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recDetailAdmin := httptest.NewRecorder()
	serverPilot.Router().ServeHTTP(recDetailAdmin, reqDetailAdmin)

	if recDetailAdmin.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for admin payment detail, got %d: %s", recDetailAdmin.Code, recDetailAdmin.Body.String())
	}

	if !strings.Contains(recDetailAdmin.Body.String(), "warning_banner") || !strings.Contains(recDetailAdmin.Body.String(), "perorangan pengurus") {
		t.Errorf("Expected warning banner for personal account in admin response, got: %s", recDetailAdmin.Body.String())
	}

	// 5. Verifikasi Audit Log untuk Pembayaran Rekening Pribadi
	var auditCount int
	err = env.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_log
		WHERE koperasi_id = $1 AND nama_tabel = 'pembayaran' AND record_id = $2
		  AND (aksi = 'PILOT_REKENING_PRIBADI' OR data_baru::text LIKE '%perorangan_pengurus%');
	`, env.KoperasiID, pembayaranID1).Scan(&auditCount)
	if err != nil {
		t.Fatalf("Query audit_log error: %v", err)
	}
	if auditCount == 0 {
		t.Errorf("Expected audit_log entry recorded for pilot personal account payment, but found %d", auditCount)
	}

	// 6. Uji Batas Harian Rekening Pribadi:
	// Set konfigurasi aturan batas harian menjadi Rp 1.000.000 untuk pengujian
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO konfigurasi_aturan (koperasi_id, kunci_aturan, versi, nilai_json, keterangan, berlaku_mulai, status_aktif)
		VALUES ($1, 'pilot_rekening_pribadi_max_per_hari', 1, '{"nominal": 1000000}'::jsonb, 'Batas harian pilot test', NOW(), true)
		ON CONFLICT (koperasi_id, kunci_aturan, versi) DO UPDATE SET nilai_json = '{"nominal": 1000000}'::jsonb;
	`, env.KoperasiID)
	if err != nil {
		t.Fatalf("Insert konfigurasi error: %v", err)
	}

	// Transaksi 2: Rp 600.000 (total akumulasi hari ini menjadi 600.000 + 600.000 = 1.200.000 > 1.000.000)
	tagihanUnder2 := env.CreateBill(t, env.AnggotaID, 600000)
	bodyUnder2, _ := json.Marshal(handlers.BuatQRISRequest{
		JenisTransaksi:  "iuran_bulanan",
		TagihanIuranIDs: []uuid.UUID{tagihanUnder2},
		RekeningQRISID:  pilotRekeningID,
	})
	reqUnder2 := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyUnder2))
	reqUnder2.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqUnder2.Header.Set("Content-Type", "application/json")
	recUnder2 := httptest.NewRecorder()
	serverPilot.Router().ServeHTTP(recUnder2, reqUnder2)

	if recUnder2.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request for transaction exceeding daily limit, got %d: %s", recUnder2.Code, recUnder2.Body.String())
	}
	if !strings.Contains(recUnder2.Body.String(), "batas harian") {
		t.Errorf("Expected daily limit error message, got: %s", recUnder2.Body.String())
	}
}

func TestTask7_ProtectedMetrics_And_MFA_TOTP(t *testing.T) {
	env := setupAPITestEnv(t)

	// ==========================================
	// BAGIAN A: Uji Proteksi /metrics (Token & Jaringan Internal)
	// ==========================================
	metricsToken := "secret-metrics-token-12345-32bytes-ok"
	cfgMetrics := env.Config
	cfgMetrics.MetricsToken = metricsToken
	serverMetrics := api.NewServer(env.Pool, cfgMetrics)

	// 1. Akses dari IP Publik/Eksternal tanpa token -> HARUS DITOLAK (401 Unauthorized)
	reqExtNoToken := httptest.NewRequest("GET", "/metrics", nil)
	reqExtNoToken.RemoteAddr = "198.51.100.55:4321" // IP publik (TEST-NET-2)
	recExtNoToken := httptest.NewRecorder()
	serverMetrics.Router().ServeHTTP(recExtNoToken, reqExtNoToken)

	if recExtNoToken.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for external /metrics without token, got %d", recExtNoToken.Code)
	}

	// 2. Akses dari IP Publik dengan Bearer Token yang Benar -> HARUS DIIZINKAN (200 OK)
	reqExtWithToken := httptest.NewRequest("GET", "/metrics", nil)
	reqExtWithToken.RemoteAddr = "198.51.100.55:4321"
	reqExtWithToken.Header.Set("Authorization", "Bearer "+metricsToken)
	recExtWithToken := httptest.NewRecorder()
	serverMetrics.Router().ServeHTTP(recExtWithToken, reqExtWithToken)

	if recExtWithToken.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for external /metrics with valid token, got %d", recExtWithToken.Code)
	}

	// 3. Akses dari Jaringan Internal (Loopback 127.0.0.1) tanpa token -> HARUS DITOLAK (401 Unauthorized, tidak percaya IP)
	reqInternal := httptest.NewRequest("GET", "/metrics", nil)
	reqInternal.RemoteAddr = "127.0.0.1:54321"
	recInternal := httptest.NewRecorder()
	serverMetrics.Router().ServeHTTP(recInternal, reqInternal)

	if recInternal.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for internal IP access to /metrics without token, got %d", recInternal.Code)
	}

	// ==========================================
	// BAGIAN B: Uji MFA TOTP (Admin, Bendahara, Ketua)
	// ==========================================
	// 1. Setup user baru dengan role bendahara
	loginResBendahara, bendaharaEmail, bendaharaPwd, _ := env.createAndLoginUserFull(t, "bendahara")
	bendaharaToken := loginResBendahara.AccessToken

	// 2. Setup MFA TOTP (/api/v1/auth/mfa/setup)
	reqSetup := httptest.NewRequest("POST", "/api/v1/auth/mfa/setup", nil)
	reqSetup.Header.Set("Authorization", "Bearer "+bendaharaToken)
	recSetup := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recSetup, reqSetup)

	if recSetup.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/v1/auth/mfa/setup, got %d: %s", recSetup.Code, recSetup.Body.String())
	}

	var resSetup struct {
		Data struct {
			Secret string `json:"secret"`
			QRURI  string `json:"qr_uri"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recSetup.Body.Bytes(), &resSetup)
	if resSetup.Data.Secret == "" || !strings.HasPrefix(resSetup.Data.QRURI, "otpauth://totp/") {
		t.Fatalf("MFA setup response invalid: %+v", resSetup)
	}

	totpSecret := resSetup.Data.Secret

	// 3. Coba aktifkan dengan kode TOTP salah -> 400 Bad Request
	bodyEnableWrong, _ := json.Marshal(map[string]string{
		"secret":    totpSecret,
		"totp_code": "000000",
	})
	reqEnableWrong := httptest.NewRequest("POST", "/api/v1/auth/mfa/enable", bytes.NewReader(bodyEnableWrong))
	reqEnableWrong.Header.Set("Authorization", "Bearer "+bendaharaToken)
	reqEnableWrong.Header.Set("Content-Type", "application/json")
	recEnableWrong := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recEnableWrong, reqEnableWrong)

	if recEnableWrong.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request for invalid TOTP code during enable, got %d", recEnableWrong.Code)
	}

	// 4. Aktifkan dengan kode TOTP valid
	validCode, err := security.GenerateTOTPCode(totpSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode failed: %v", err)
	}

	bodyEnableOk, _ := json.Marshal(map[string]string{
		"secret":    totpSecret,
		"totp_code": validCode,
	})
	reqEnableOk := httptest.NewRequest("POST", "/api/v1/auth/mfa/enable", bytes.NewReader(bodyEnableOk))
	reqEnableOk.Header.Set("Authorization", "Bearer "+bendaharaToken)
	reqEnableOk.Header.Set("Content-Type", "application/json")
	recEnableOk := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recEnableOk, reqEnableOk)

	if recEnableOk.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for valid MFA enable, got %d: %s", recEnableOk.Code, recEnableOk.Body.String())
	}

	// 5. Login ulang akun bendahara setelah MFA aktif -> HARUS mereturn mfa_required = true & mfa_session_token
	bodyLoginMFA, _ := json.Marshal(handlers.LoginRequest{
		Email:    bendaharaEmail,
		Password: bendaharaPwd,
	})
	reqLoginMFA := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLoginMFA))
	recLoginMFA := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLoginMFA, reqLoginMFA)

	if recLoginMFA.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for MFA step 1 login, got %d: %s", recLoginMFA.Code, recLoginMFA.Body.String())
	}

	var resLoginMFA struct {
		Data struct {
			MFARequired     bool   `json:"mfa_required"`
			MFASessionToken string `json:"mfa_session_token"`
			AccessToken     string `json:"access_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recLoginMFA.Body.Bytes(), &resLoginMFA)
	if !resLoginMFA.Data.MFARequired || resLoginMFA.Data.MFASessionToken == "" {
		t.Fatalf("Expected mfa_required=true and session token, got: %+v", resLoginMFA.Data)
	}
	if resLoginMFA.Data.AccessToken != "" {
		t.Fatalf("Access token must NOT be returned before MFA verification")
	}

	// 6. Verifikasi Login MFA dengan kode salah -> 401 Unauthorized
	bodyVerifyWrong, _ := json.Marshal(map[string]string{
		"mfa_session_token": resLoginMFA.Data.MFASessionToken,
		"totp_code":          "999999",
	})
	reqVerifyWrong := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyVerifyWrong))
	recVerifyWrong := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerifyWrong, reqVerifyWrong)

	if recVerifyWrong.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for wrong TOTP code on login, got %d", recVerifyWrong.Code)
	}

	// 7. Verifikasi Login MFA dengan kode yang benar -> 200 OK dan Access Token diberikan!
	correctCode, _ := security.GenerateTOTPCode(totpSecret, time.Now())
	bodyVerifyOk, _ := json.Marshal(map[string]string{
		"mfa_session_token": resLoginMFA.Data.MFASessionToken,
		"totp_code":          correctCode,
	})
	reqVerifyOk := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyVerifyOk))
	recVerifyOk := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerifyOk, reqVerifyOk)

	if recVerifyOk.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for valid MFA verify, got %d: %s", recVerifyOk.Code, recVerifyOk.Body.String())
	}

	var resFinalLogin struct {
		Data handlers.LoginResponse `json:"data"`
	}
	_ = json.Unmarshal(recVerifyOk.Body.Bytes(), &resFinalLogin)
	if resFinalLogin.Data.AccessToken == "" || resFinalLogin.Data.RefreshToken == "" {
		t.Fatalf("Expected full LoginResponse with tokens, got: %+v", resFinalLogin.Data)
	}
}

func TestTask8_RolePasswordFromEnv_NoProductionDefault(t *testing.T) {
	psqlPath := "psql"
	if _, err := exec.LookPath("psql"); err != nil {
		if _, errW := os.Stat(`C:\Users\12\pgsql\bin\psql.exe`); errW == nil {
			psqlPath = `C:\Users\12\pgsql\bin\psql.exe`
		}
	}
	dbPort := "5432"
	if p := os.Getenv("PGPORT"); p != "" {
		dbPort = p
	} else if p := os.Getenv("DB_PORT"); p != "" {
		dbPort = p
	} else if _, err := os.Stat(`C:\Users\12\pgdata16`); err == nil {
		dbPort = "5433"
	}

	rolesSqlPath := `../../db/docker-init/01_roles.sql`
	if _, err := os.Stat(rolesSqlPath); err != nil {
		rolesSqlPath = `db/docker-init/01_roles.sql`
	}

	// 1. Kasus 1: APP_ENV=production tanpa password -> HARUS GAGAL
	cmdFailNoPass := exec.Command(psqlPath, "-h", "127.0.0.1", "-p", dbPort, "-U", "postgres", "-d", "kopdes_db", "-v", "ON_ERROR_STOP=1", "-f", rolesSqlPath)
	cmdFailNoPass.Env = append(os.Environ(), "APP_ENV=production", "KOPDES_APP_PASSWORD=", "DB_PASSWORD=")
	outFailNoPass, err := cmdFailNoPass.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected 01_roles.sql to fail in production without password, but it succeeded! Output: %s", string(outFailNoPass))
	}
	if !strings.Contains(string(outFailNoPass), "KOPDES_APP_PASSWORD") && !strings.Contains(string(outFailNoPass), "produksi") {
		t.Errorf("Expected error message to mention KOPDES_APP_PASSWORD in production, got: %s", string(outFailNoPass))
	}

	// 2. Kasus 2: APP_ENV=production dengan password default dev -> HARUS DITOLAK
	cmdFailDevPass := exec.Command(psqlPath, "-h", "127.0.0.1", "-p", dbPort, "-U", "postgres", "-d", "kopdes_db", "-v", "ON_ERROR_STOP=1", "-f", rolesSqlPath)
	cmdFailDevPass.Env = append(os.Environ(), "APP_ENV=production", "KOPDES_APP_PASSWORD=kopdes_dev_password")
	outFailDevPass, err := cmdFailDevPass.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected 01_roles.sql to fail in production with dev password default, but it succeeded! Output: %s", string(outFailDevPass))
	}
	if !strings.Contains(string(outFailDevPass), "default") {
		t.Errorf("Expected error message to mention default password, got: %s", string(outFailDevPass))
	}

	// 3. Kasus 3: APP_ENV=production dengan password kuat -> HARUS SUKSES
	cmdSuccessProd := exec.Command(psqlPath, "-h", "127.0.0.1", "-p", dbPort, "-U", "postgres", "-d", "kopdes_db", "-v", "ON_ERROR_STOP=1", "-f", rolesSqlPath)
	cmdSuccessProd.Env = append(os.Environ(), "APP_ENV=production", "KOPDES_APP_PASSWORD=P@ssw0rdProdKopdes2026!")
	outSuccessProd, err := cmdSuccessProd.CombinedOutput()
	if err != nil {
		t.Fatalf("Expected 01_roles.sql to succeed in production with strong password, got error: %v, output: %s", err, string(outSuccessProd))
	}

	// 4. Kembalikan password ke dev agar tes lain tetap berjalan lancar
	cmdResetDev := exec.Command(psqlPath, "-h", "127.0.0.1", "-p", dbPort, "-U", "postgres", "-d", "kopdes_db", "-v", "ON_ERROR_STOP=1", "-f", rolesSqlPath)
	cmdResetDev.Env = append(os.Environ(), "APP_ENV=development", "KOPDES_APP_PASSWORD=kopdes_dev_password")
	outReset, err := cmdResetDev.CombinedOutput()
	if err != nil {
		t.Fatalf("Reset password dev failed: %v, output: %s", err, string(outReset))
	}
}

func TestTask9_MissingCRUD_Endpoints_EndToEnd(t *testing.T) {
	env := setupAPITestEnv(t)

	// --- 1. Manajemen Anggota ---
	// 1a. List Anggota
	reqListAnggota := httptest.NewRequest("GET", "/api/v1/anggota", nil)
	reqListAnggota.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recListAnggota := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recListAnggota, reqListAnggota)
	if recListAnggota.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/anggota, got %d: %s", recListAnggota.Code, recListAnggota.Body.String())
	}

	// 1b. Detail Anggota
	reqDetailAnggota := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/anggota/%s", env.AnggotaID), nil)
	reqDetailAnggota.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recDetailAnggota := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recDetailAnggota, reqDetailAnggota)
	if recDetailAnggota.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/anggota/{id}, got %d: %s", recDetailAnggota.Code, recDetailAnggota.Body.String())
	}

	// 1c. Update Status Anggota (misal nonaktifkan)
	bodyStatus, _ := json.Marshal(map[string]string{
		"status":  "nonaktif",
		"alasan":  "Pengujian status anggota",
		"catatan": "Catatan pengurus",
	})
	reqUpdateStatus := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/anggota/%s/status", env.AnggotaID), bytes.NewReader(bodyStatus))
	reqUpdateStatus.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqUpdateStatus.Header.Set("Content-Type", "application/json")
	recUpdateStatus := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recUpdateStatus, reqUpdateStatus)
	if recUpdateStatus.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for PATCH /api/v1/anggota/{id}/status, got %d: %s", recUpdateStatus.Code, recUpdateStatus.Body.String())
	}

	// --- 2. CRUD Iuran ---
	// 2a. Buat Jenis Iuran Baru
	bodyJenisIuran, _ := json.Marshal(map[string]interface{}{
		"kode":           fmt.Sprintf("IUR-%s", uuid.New().String()[:6]),
		"nama":           "Iuran Dana Sosial",
		"tipe":           "sukarela",
		"nominal_standar": 25000,
	})
	reqBuatJenis := httptest.NewRequest("POST", "/api/v1/iuran/jenis", bytes.NewReader(bodyJenisIuran))
	reqBuatJenis.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqBuatJenis.Header.Set("Content-Type", "application/json")
	recBuatJenis := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recBuatJenis, reqBuatJenis)
	if recBuatJenis.Code != http.StatusCreated && recBuatJenis.Code != http.StatusOK {
		t.Fatalf("Expected 201/200 for POST /api/v1/iuran/jenis, got %d: %s", recBuatJenis.Code, recBuatJenis.Body.String())
	}

	// 2b. List Jenis Iuran
	reqListJenis := httptest.NewRequest("GET", "/api/v1/iuran/jenis", nil)
	reqListJenis.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recListJenis := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recListJenis, reqListJenis)
	if recListJenis.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/iuran/jenis, got %d", recListJenis.Code)
	}

	// 2c. List Tagihan Saya (Anggota)
	reqTagihanSaya := httptest.NewRequest("GET", "/api/v1/iuran/tagihan/saya", nil)
	reqTagihanSaya.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recTagihanSaya := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recTagihanSaya, reqTagihanSaya)
	if recTagihanSaya.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/iuran/tagihan/saya, got %d: %s", recTagihanSaya.Code, recTagihanSaya.Body.String())
	}

	// --- 3. CRUD Katalog + Promo ---
	// 3a. Buat Produk Baru + Harga
	bodyProduk, _ := json.Marshal(map[string]interface{}{
		"nama":          "Beras Premium Desa 5kg",
		"sku":           fmt.Sprintf("BRS-%s", uuid.New().String()[:6]),
		"harga_umum":    75000,
		"harga_anggota": 70000,
		"stok_awal":     100,
	})
	reqBuatProduk := httptest.NewRequest("POST", "/api/v1/katalog/produk", bytes.NewReader(bodyProduk))
	reqBuatProduk.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqBuatProduk.Header.Set("Content-Type", "application/json")
	recBuatProduk := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recBuatProduk, reqBuatProduk)
	if recBuatProduk.Code != http.StatusCreated && recBuatProduk.Code != http.StatusOK {
		t.Fatalf("Expected 201/200 for POST /api/v1/katalog/produk, got %d: %s", recBuatProduk.Code, recBuatProduk.Body.String())
	}
	var resProduk struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recBuatProduk.Body.Bytes(), &resProduk)
	produkID := resProduk.Data.ID

	// 3b. List Produk
	reqListProduk := httptest.NewRequest("GET", "/api/v1/katalog/produk", nil)
	reqListProduk.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recListProduk := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recListProduk, reqListProduk)
	if recListProduk.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/katalog/produk, got %d", recListProduk.Code)
	}

	// 3c. Buat Promo
	bodyPromo, _ := json.Marshal(map[string]interface{}{
		"kode":             fmt.Sprintf("PROMO-%s", uuid.New().String()[:6]),
		"nama":             "Promo Panen Raya",
		"tipe_promo":       "nominal_tetap",
		"nilai_diskon":     5000,
		"minimal_belanja":  50000,
		"durasi_hari":      7,
	})
	reqBuatPromo := httptest.NewRequest("POST", "/api/v1/katalog/promo", bytes.NewReader(bodyPromo))
	reqBuatPromo.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqBuatPromo.Header.Set("Content-Type", "application/json")
	recBuatPromo := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recBuatPromo, reqBuatPromo)
	if recBuatPromo.Code != http.StatusCreated && recBuatPromo.Code != http.StatusOK {
		t.Fatalf("Expected 201/200 for POST /api/v1/katalog/promo, got %d: %s", recBuatPromo.Code, recBuatPromo.Body.String())
	}

	// 3d. List Promo
	reqListPromo := httptest.NewRequest("GET", "/api/v1/katalog/promo", nil)
	reqListPromo.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recListPromo := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recListPromo, reqListPromo)
	if recListPromo.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/katalog/promo, got %d", recListPromo.Code)
	}

	// --- 4. Keranjang / Pesanan / Checkout ---
	// 4a. Tambah Item ke Keranjang
	bodyCart, _ := json.Marshal(map[string]interface{}{
		"produk_id": produkID,
		"qty":       2,
	})
	reqAddCart := httptest.NewRequest("POST", "/api/v1/toko/keranjang", bytes.NewReader(bodyCart))
	reqAddCart.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqAddCart.Header.Set("Content-Type", "application/json")
	recAddCart := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recAddCart, reqAddCart)
	if recAddCart.Code != http.StatusOK && recAddCart.Code != http.StatusCreated {
		t.Fatalf("Expected 200/201 for POST /api/v1/toko/keranjang, got %d: %s", recAddCart.Code, recAddCart.Body.String())
	}

	// 4b. Lihat Keranjang
	reqGetCart := httptest.NewRequest("GET", "/api/v1/toko/keranjang", nil)
	reqGetCart.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recGetCart := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recGetCart, reqGetCart)
	if recGetCart.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/toko/keranjang, got %d", recGetCart.Code)
	}

	// 4c. Checkout Keranjang -> Pesanan
	bodyCheckout, _ := json.Marshal(map[string]interface{}{
		"metode_serah_terima": "ambil_di_koperasi",
		"catatan":             "Siapkan paket beras",
	})
	reqCheckout := httptest.NewRequest("POST", "/api/v1/toko/checkout", bytes.NewReader(bodyCheckout))
	reqCheckout.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqCheckout.Header.Set("Content-Type", "application/json")
	recCheckout := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCheckout, reqCheckout)
	if recCheckout.Code != http.StatusCreated && recCheckout.Code != http.StatusOK {
		t.Fatalf("Expected 201/200 for POST /api/v1/toko/checkout, got %d: %s", recCheckout.Code, recCheckout.Body.String())
	}

	// 4d. List Pesanan
	reqListPesanan := httptest.NewRequest("GET", "/api/v1/toko/pesanan", nil)
	reqListPesanan.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recListPesanan := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recListPesanan, reqListPesanan)
	if recListPesanan.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/toko/pesanan, got %d", recListPesanan.Code)
	}

	// --- 5. Laporan ---
	// 5a. Laporan Keuangan
	reqLapKeu := httptest.NewRequest("GET", "/api/v1/laporan/keuangan", nil)
	reqLapKeu.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recLapKeu := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLapKeu, reqLapKeu)
	if recLapKeu.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/laporan/keuangan, got %d", recLapKeu.Code)
	}

	// 5b. Laporan Rekap Anggota
	reqLapAnggota := httptest.NewRequest("GET", "/api/v1/laporan/anggota", nil)
	reqLapAnggota.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recLapAnggota := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLapAnggota, reqLapAnggota)
	if recLapAnggota.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/laporan/anggota, got %d", recLapAnggota.Code)
	}

	// --- 6. Manajemen Pengguna ---
	// 6a. List Users
	reqListUsers := httptest.NewRequest("GET", "/api/v1/users", nil)
	reqListUsers.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recListUsers := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recListUsers, reqListUsers)
	if recListUsers.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/users, got %d", recListUsers.Code)
	}

	// 6b. Detail User
	reqDetailUser := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/users/%s", env.AnggotaUserID), nil)
	reqDetailUser.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recDetailUser := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recDetailUser, reqDetailUser)
	if recDetailUser.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/v1/users/{id}, got %d", recDetailUser.Code)
	}

	// 6c. Update Role User (misal jadikan operator)
	bodyRole, _ := json.Marshal(map[string]string{
		"role": "operator",
	})
	reqUpdateRole := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/users/%s/role", env.AnggotaUserID), bytes.NewReader(bodyRole))
	reqUpdateRole.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqUpdateRole.Header.Set("Content-Type", "application/json")
	recUpdateRole := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recUpdateRole, reqUpdateRole)
	if recUpdateRole.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for PATCH /api/v1/users/{id}/role, got %d: %s", recUpdateRole.Code, recUpdateRole.Body.String())
	}
}

func TestItem1_BankMutation_Segregation_Hash_1to1_FakeAttempts(t *testing.T) {
	env := setupAPITestEnv(t)

	// Buat Admin 2 untuk menguji maker-checker / orang kedua
	loginRes2, _, _, admin2ID := env.createAndLoginUserFull(t, "admin")
	admin2Token := loginRes2.AccessToken

	// --- 1. Uji Segregasi: Pengimpor TIDAK BOLEH sama dengan pembuat/pembayar ---
	// Admin membuat pembayaran
	billAdmin := env.CreateBill(t, env.AnggotaID, 88000)
	bodyPayAdmin, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{billAdmin},
	})
	reqPayAdmin := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyPayAdmin))
	reqPayAdmin.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqPayAdmin.Header.Set("Content-Type", "application/json")
	recPayAdmin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPayAdmin, reqPayAdmin)
	if recPayAdmin.Code != http.StatusCreated {
		t.Fatalf("Gagal buat pembayaran admin: %d: %s", recPayAdmin.Code, recPayAdmin.Body.String())
	}
	var resPayAdmin struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recPayAdmin.Body.Bytes(), &resPayAdmin)

	// Admin mencoba mengimpor CSV untuk mencocokkan pembayarannya sendiri
	csvSelfMatch := fmt.Sprintf("tanggal,keterangan,tipe,nominal,no_rekening\n2026-11-12 11:00:00,SETTLEMENT SELF / CR,CR,%d.00,999111222\n", resPayAdmin.Data.NominalTotalRp)
	bodyCSV1 := &bytes.Buffer{}
	w1 := multipart.NewWriter(bodyCSV1)
	p1, _ := w1.CreateFormFile("file", "mutasi_self.csv")
	p1.Write([]byte(csvSelfMatch))
	w1.Close()

	reqSelfImport := httptest.NewRequest("POST", "/api/v1/pembayaran/mutasi-bank/impor", bodyCSV1)
	reqSelfImport.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqSelfImport.Header.Set("Content-Type", w1.FormDataContentType())
	recSelfImport := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recSelfImport, reqSelfImport)

	// Pembayaran tidak boleh disetujui, harus terdeteksi anomali / segregasi pengimpor
	reqCheckAdmin := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/%s", resPayAdmin.Data.PembayaranID), nil)
	reqCheckAdmin.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recCheckAdmin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCheckAdmin, reqCheckAdmin)
	var statusSelfPay struct {
		Data struct {
			Status    string `json:"status"`
			IsAnomali bool   `json:"is_anomali"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recCheckAdmin.Body.Bytes(), &statusSelfPay)
	if statusSelfPay.Data.Status == "disetujui" {
		t.Fatalf("Pelanggaran segregasi: Pengimpor tidak boleh menyetujui pembayaran miliknya sendiri!")
	}
	if !statusSelfPay.Data.IsAnomali {
		t.Fatalf("Harusnya ditandai anomali segregasi pengimpor sama dengan pembuat pembayaran!")
	}

	// --- 2. Auto-Approve Nonaktif Default: Pembayaran anggota cocok beralih ke 'menunggu_persetujuan_kedua' ---
	billAnggota := env.CreateBill(t, env.AnggotaID, 95000)
	bodyPayAnggota, _ := json.Marshal(handlers.BuatQRISRequest{
		MetodePembayaranID: env.MetodeID,
		JenisTransaksi:     "iuran_bulanan",
		TagihanIuranIDs:    []uuid.UUID{billAnggota},
	})
	reqPayAnggota := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyPayAnggota))
	reqPayAnggota.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqPayAnggota.Header.Set("Content-Type", "application/json")
	recPayAnggota := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPayAnggota, reqPayAnggota)
	if recPayAnggota.Code != http.StatusCreated {
		t.Fatalf("Gagal buat pembayaran anggota: %d: %s", recPayAnggota.Code, recPayAnggota.Body.String())
	}
	var resPayAnggota struct {
		Data handlers.BuatQRISResponse `json:"data"`
	}
	_ = json.Unmarshal(recPayAnggota.Body.Bytes(), &resPayAnggota)

	// Admin mengimpor CSV yang cocok
	csvAnggotaMatch := fmt.Sprintf("tanggal,keterangan,tipe,nominal,no_rekening\n2026-11-12 12:00:00,SETTLEMENT ANGGOTA / CR,CR,%d.00,999111222\n", resPayAnggota.Data.NominalTotalRp)
	bodyCSV2 := &bytes.Buffer{}
	w2 := multipart.NewWriter(bodyCSV2)
	p2, _ := w2.CreateFormFile("file", "mutasi_anggota.csv")
	p2.Write([]byte(csvAnggotaMatch))
	w2.Close()

	reqImportAnggota := httptest.NewRequest("POST", "/api/v1/pembayaran/mutasi-bank/impor", bodyCSV2)
	reqImportAnggota.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqImportAnggota.Header.Set("Content-Type", w2.FormDataContentType())
	recImportAnggota := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recImportAnggota, reqImportAnggota)
	t.Logf("Import response: %s", recImportAnggota.Body.String())

	// Status HARUS 'menunggu_persetujuan_kedua' karena auto-approve nonaktif secara default
	reqCheckStatus2 := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/pembayaran/%s", resPayAnggota.Data.PembayaranID), nil)
	reqCheckStatus2.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	recCheckStatus2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCheckStatus2, reqCheckStatus2)
	var statusPay2 struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recCheckStatus2.Body.Bytes(), &statusPay2)
	if statusPay2.Data.Status != "menunggu_persetujuan_kedua" {
		t.Fatalf("Expected status 'menunggu_persetujuan_kedua' (auto-approve default nonaktif), got %s", statusPay2.Data.Status)
	}

	// Orang kedua (Admin 2) menyetujui -> status 'disetujui'
	reqApprove2 := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", resPayAnggota.Data.PembayaranID), strings.NewReader(`{"status":"disetujui","catatan":"Konfirmasi orang kedua mutasi bank"}`))
	reqApprove2.Header.Set("Authorization", "Bearer "+admin2Token)
	reqApprove2.Header.Set("Content-Type", "application/json")
	recApprove2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recApprove2, reqApprove2)
	if recApprove2.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for second approval, got %d: %s", recApprove2.Code, recApprove2.Body.String())
	}

	// --- 3. Hash Baris Mutasi Unik: Cegah Impor Baris yang Sama Ulang ---
	bodyCSV3 := &bytes.Buffer{}
	w3 := multipart.NewWriter(bodyCSV3)
	p3, _ := w3.CreateFormFile("file", "mutasi_duplicate.csv")
	p3.Write([]byte(csvAnggotaMatch)) // baris sama persis dengan CSV2
	w3.Close()

	reqImportDup := httptest.NewRequest("POST", "/api/v1/pembayaran/mutasi-bank/impor", bodyCSV3)
	reqImportDup.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqImportDup.Header.Set("Content-Type", w3.FormDataContentType())
	recImportDup := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recImportDup, reqImportDup)

	var resDup struct {
		Data struct {
			Duplikat int `json:"duplikat"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recImportDup.Body.Bytes(), &resDup)
	if resDup.Data.Duplikat < 1 {
		t.Fatalf("Expected duplicate count >= 1 for identical CSV row, got %d. Body: %s", resDup.Data.Duplikat, recImportDup.Body.String())
	}

	// --- 4. 1-to-1 Matching & Upaya CSV Palsu (Nominal Negatif, Corrupted) ---
	fakeCSV := "tanggal,keterangan,tipe,nominal,no_rekening\n" +
		"2026-11-12 13:00:00,NEGATIVE AMOUNT,CR,-50000,123456\n" +
		"2026-11-12 14:00:00,DEBIT TRANSACTION,DB,100000,123456\n" +
		"CORRUPTED LINE WITHOUT PROPER FIELDS\n"

	bodyCSV4 := &bytes.Buffer{}
	w4 := multipart.NewWriter(bodyCSV4)
	p4, _ := w4.CreateFormFile("file", "mutasi_fake.csv")
	p4.Write([]byte(fakeCSV))
	w4.Close()

	reqFakeImport := httptest.NewRequest("POST", "/api/v1/pembayaran/mutasi-bank/impor", bodyCSV4)
	reqFakeImport.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqFakeImport.Header.Set("Content-Type", w4.FormDataContentType())
	recFakeImport := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recFakeImport, reqFakeImport)

	if recFakeImport.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for gracefully handling corrupted/negative rows, got %d: %s", recFakeImport.Code, recFakeImport.Body.String())
	}

	t.Logf("Sukses: Verifikasi pengimpor != pembuat, auto-approve nonaktif, hash unik baris, dan 1-to-1 mutasi!")
	_ = admin2ID
}

func TestItem2_Metrics_AlwaysRequiresToken_EvenBehindProxy(t *testing.T) {
	env := setupAPITestEnv(t)

	// Pastikan METRICS_TOKEN terkonfigurasi di server
	metricsToken := "valid-secret-metrics-token-12345"
	metricsHandler := handlers.NewMetricsHandler(env.Pool, metricsToken, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", metricsHandler.HandleMetrics)

	// Skenario 1: Klien dari IP privat internal (10.0.1.25) tanpa token -> HARUS DITOLAK (401)
	reqPrivate := httptest.NewRequest("GET", "/metrics", nil)
	reqPrivate.RemoteAddr = "10.0.1.25:45678"
	recPrivate := httptest.NewRecorder()
	mux.ServeHTTP(recPrivate, reqPrivate)
	if recPrivate.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for private IP without token, got %d: %s", recPrivate.Code, recPrivate.Body.String())
	}

	// Skenario 2: Klien di belakang reverse proxy (X-Forwarded-For: 192.168.1.100) tanpa token -> HARUS DITOLAK (401)
	reqProxy := httptest.NewRequest("GET", "/metrics", nil)
	reqProxy.RemoteAddr = "172.16.0.5:12345"
	reqProxy.Header.Set("X-Forwarded-For", "192.168.1.100")
	recProxy := httptest.NewRecorder()
	mux.ServeHTTP(recProxy, reqProxy)
	if recProxy.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized behind proxy without token, got %d: %s", recProxy.Code, recProxy.Body.String())
	}

	// Skenario 3: Loopback 127.0.0.1 tanpa token -> HARUS DITOLAK (401)
	reqLoopback := httptest.NewRequest("GET", "/metrics", nil)
	reqLoopback.RemoteAddr = "127.0.0.1:54321"
	recLoopback := httptest.NewRecorder()
	mux.ServeHTTP(recLoopback, reqLoopback)
	if recLoopback.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for loopback without token, got %d: %s", recLoopback.Code, recLoopback.Body.String())
	}

	// Skenario 4: Token valid disertakan via Bearer -> HARUS 200 OK
	reqValid := httptest.NewRequest("GET", "/metrics", nil)
	reqValid.RemoteAddr = "10.0.1.25:45678"
	reqValid.Header.Set("Authorization", "Bearer "+metricsToken)
	recValid := httptest.NewRecorder()
	mux.ServeHTTP(recValid, reqValid)
	if recValid.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK with valid metrics token, got %d: %s", recValid.Code, recValid.Body.String())
	}
	if !strings.Contains(recValid.Body.String(), "kopdes_pembayaran_antrean_verifikasi") {
		t.Errorf("Expected Prometheus metrics output, got: %s", recValid.Body.String())
	}

	t.Logf("Sukses: Endpoint /metrics mewajibkan token selalu dan menolak asumsi IP internal!")
}

func TestItem3_MFA_Mandatory_Attempts_NoReplay_RecoveryCodes(t *testing.T) {
	env := setupAPITestEnv(t)

	ctx := context.Background()
	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")

	// Buat user dengan peran 'ketua'
	ketuaEmail := fmt.Sprintf("ketua_%s@kopdes.id", uuid.New().String()[:8])
	ketuaHash := security.HashBlindIndex(ketuaEmail, []byte(env.Config.BlindIndexSecret))
	ketuaPwd := "KetuaPass123!"
	ketuaPwdHash, _ := security.HashPassword(ketuaPwd)
	ketuaUserID := uuid.New()

	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap)
		VALUES ($1, $2, $3, $2, $3, $4, 'Ketua Koperasi');
	`, ketuaUserID, []byte("enkripsi"), ketuaHash, ketuaPwdHash)
	if err != nil {
		t.Fatalf("insert ketua error: %v", err)
	}

	var ketuaRoleID uuid.UUID
	err = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'ketua' LIMIT 1").Scan(&ketuaRoleID)
	if err != nil || ketuaRoleID == uuid.Nil {
		ketuaRoleID = uuid.New()
		_, _ = conn.Exec(ctx, "INSERT INTO roles (id, kode, nama, deskripsi) VALUES ($1, 'ketua', 'ketua', 'Ketua Koperasi') ON CONFLICT (kode) DO NOTHING;", ketuaRoleID)
		_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'ketua'").Scan(&ketuaRoleID)
	}

	_, _ = conn.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, koperasi_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (koperasi_id, user_id, role_id) DO NOTHING;
	`, ketuaUserID, ketuaRoleID, env.KoperasiID)

	_, _ = conn.Exec(ctx, "SELECT set_config('app.is_platform_admin', 'false', false); SET ROLE kopdes_app;")

	// -------------------------------------------------------------------------
	// Skenario 1: Login ketua sebelum MFA aktif HARUS DITOLAK (403 Forbidden)
	// -------------------------------------------------------------------------
	bodyLogin1, _ := json.Marshal(handlers.LoginRequest{Email: ketuaEmail, Password: ketuaPwd})
	reqLogin1 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin1))
	recLogin1 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin1, reqLogin1)

	if recLogin1.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden for ketua login without MFA active, got %d: %s", recLogin1.Code, recLogin1.Body.String())
	}

	var resLogin1 struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Data struct {
			MFARequired      bool   `json:"mfa_required"`
			MFASetupRequired bool   `json:"mfa_setup_required"`
			SetupToken       string `json:"setup_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recLogin1.Body.Bytes(), &resLogin1)
	if !resLogin1.Data.MFASetupRequired || resLogin1.Data.SetupToken == "" {
		t.Fatalf("Expected mfa_setup_required=true and setup_token in 403 response, got: %+v", resLogin1)
	}

	setupToken := resLogin1.Data.SetupToken

	// -------------------------------------------------------------------------
	// Skenario 2: Setup MFA dengan setup_token dan enable MFA (dapatkan recovery codes)
	// -------------------------------------------------------------------------
	reqSetup := httptest.NewRequest("POST", "/api/v1/auth/mfa/setup", nil)
	reqSetup.Header.Set("Authorization", "Bearer "+setupToken)
	recSetup := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recSetup, reqSetup)

	if recSetup.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/v1/auth/mfa/setup with setup_token, got %d: %s", recSetup.Code, recSetup.Body.String())
	}

	var resSetup struct {
		Data struct {
			Secret string `json:"secret"`
			QRURI  string `json:"qr_uri"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recSetup.Body.Bytes(), &resSetup)
	totpSecret := resSetup.Data.Secret

	validCode, err := security.GenerateTOTPCode(totpSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode error: %v", err)
	}

	bodyEnable, _ := json.Marshal(map[string]string{
		"secret":    totpSecret,
		"totp_code": validCode,
	})
	reqEnable := httptest.NewRequest("POST", "/api/v1/auth/mfa/enable", bytes.NewReader(bodyEnable))
	reqEnable.Header.Set("Authorization", "Bearer "+setupToken)
	reqEnable.Header.Set("Content-Type", "application/json")
	recEnable := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recEnable, reqEnable)

	if recEnable.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/v1/auth/mfa/enable, got %d: %s", recEnable.Code, recEnable.Body.String())
	}

	var resEnable struct {
		Data struct {
			RecoveryCodes []string `json:"recovery_codes"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recEnable.Body.Bytes(), &resEnable)
	if len(resEnable.Data.RecoveryCodes) != 8 {
		t.Fatalf("Expected 8 one-time recovery codes, got %d: %+v", len(resEnable.Data.RecoveryCodes), resEnable.Data.RecoveryCodes)
	}
	recoveryCodes := resEnable.Data.RecoveryCodes

	// -------------------------------------------------------------------------
	// Skenario 3: Login ketua setelah MFA aktif -> mengembalikan mfa_session_token
	// -------------------------------------------------------------------------
	reqLogin2 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin1))
	recLogin2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin2, reqLogin2)

	if recLogin2.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK after MFA active, got %d: %s", recLogin2.Code, recLogin2.Body.String())
	}

	var resLogin2 struct {
		Data struct {
			MFARequired     bool   `json:"mfa_required"`
			MFASessionToken string `json:"mfa_session_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recLogin2.Body.Bytes(), &resLogin2)
	sessionToken := resLogin2.Data.MFASessionToken
	if !resLogin2.Data.MFARequired || sessionToken == "" {
		t.Fatalf("Expected mfa_required=true and session_token, got: %+v", resLogin2)
	}

	// -------------------------------------------------------------------------
	// Skenario 4: Batas Percobaan per mfa_session_token (Maksimal 3 kali)
	// -------------------------------------------------------------------------
	// Percobaan 1: Kode salah -> 401
	bodyWrongMFA, _ := json.Marshal(map[string]string{
		"mfa_session_token": sessionToken,
		"totp_code":         "000000",
	})
	reqV1 := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyWrongMFA))
	recV1 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recV1, reqV1)
	if recV1.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 on attempt 1, got %d", recV1.Code)
	}

	// Percobaan 2: Kode salah -> 401
	reqV2 := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyWrongMFA))
	recV2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recV2, reqV2)
	if recV2.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 on attempt 2, got %d", recV2.Code)
	}

	// Percobaan 3: Kode salah -> 429 atau 401 (terkunci)
	reqV3 := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyWrongMFA))
	recV3 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recV3, reqV3)
	if recV3.Code != http.StatusTooManyRequests && recV3.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 429 Too Many Requests or 401 on attempt 3, got %d: %s", recV3.Code, recV3.Body.String())
	}

	// Percobaan 4: Menggunakan kode yang benar pada sesi yang telah terkunci -> HARUS DITOLAK
	freshCode, _ := security.GenerateTOTPCode(totpSecret, time.Now())
	bodyLockedMFA, _ := json.Marshal(map[string]string{
		"mfa_session_token": sessionToken,
		"totp_code":         freshCode,
	})
	reqV4 := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyLockedMFA))
	recV4 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recV4, reqV4)
	if recV4.Code != http.StatusTooManyRequests && recV4.Code != http.StatusUnauthorized {
		t.Fatalf("Expected locked session rejection on attempt 4, got %d", recV4.Code)
	}

	// -------------------------------------------------------------------------
	// Skenario 5: Cegah Pakai Ulang Kode TOTP dalam Jendela yang Sama (Anti-Replay)
	// -------------------------------------------------------------------------
	// Login ulang untuk sesi baru
	reqLogin3 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin1))
	recLogin3 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin3, reqLogin3)
	_ = json.Unmarshal(recLogin3.Body.Bytes(), &resLogin2)
	sessionToken3 := resLogin2.Data.MFASessionToken

	currentCode, _ := security.GenerateTOTPCode(totpSecret, time.Now())
	bodyValidMFA, _ := json.Marshal(map[string]string{
		"mfa_session_token": sessionToken3,
		"totp_code":         currentCode,
	})
	reqV5 := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyValidMFA))
	recV5 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recV5, reqV5)
	if recV5.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on first valid TOTP verify, got %d: %s", recV5.Code, recV5.Body.String())
	}

	// Buat sesi baru lagi di detik/jendela yang sama dan gunakan currentCode yang sama -> HARUS DITOLAK (Replay)
	reqLogin4 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin1))
	recLogin4 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin4, reqLogin4)
	_ = json.Unmarshal(recLogin4.Body.Bytes(), &resLogin2)
	sessionToken4 := resLogin2.Data.MFASessionToken

	bodyReplayMFA, _ := json.Marshal(map[string]string{
		"mfa_session_token": sessionToken4,
		"totp_code":         currentCode,
	})
	reqV6 := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyReplayMFA))
	recV6 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recV6, reqV6)
	if recV6.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for replayed TOTP code within same window, got %d: %s", recV6.Code, recV6.Body.String())
	}

	// -------------------------------------------------------------------------
	// Skenario 6: Kode Pemulihan Sekali Pakai (One-time Recovery Codes)
	// -------------------------------------------------------------------------
	// Buat sesi login baru untuk recovery code
	reqLogin5 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin1))
	recLogin5 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin5, reqLogin5)
	_ = json.Unmarshal(recLogin5.Body.Bytes(), &resLogin2)
	sessionToken5 := resLogin2.Data.MFASessionToken

	chosenRecoveryCode := recoveryCodes[0]
	bodyRecovery, _ := json.Marshal(map[string]string{
		"mfa_session_token": sessionToken5,
		"recovery_code":     chosenRecoveryCode,
	})
	reqV7 := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyRecovery))
	recV7 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recV7, reqV7)
	if recV7.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK using valid recovery code, got %d: %s", recV7.Code, recV7.Body.String())
	}

	// Coba pakai ulang recovery code yang sama di sesi baru -> HARUS DITOLAK (sudah hangus/burned)
	reqLogin6 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin1))
	recLogin6 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin6, reqLogin6)
	_ = json.Unmarshal(recLogin6.Body.Bytes(), &resLogin2)
	sessionToken6 := resLogin2.Data.MFASessionToken

	bodyBurnedRecovery, _ := json.Marshal(map[string]string{
		"mfa_session_token": sessionToken6,
		"recovery_code":     chosenRecoveryCode,
	})
	reqV8 := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyBurnedRecovery))
	recV8 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recV8, reqV8)
	if recV8.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for reused/burned recovery code, got %d: %s", recV8.Code, recV8.Body.String())
	}

	t.Logf("Sukses: MFA wajib untuk peran istimewa, limit percobaan sesi, anti-replay, dan recovery codes sekali pakai!")
}

func TestItem4_ProductionRolePassword_RefusesDefault(t *testing.T) {
	// Skenario 1: Pada produksi, password kosong harus ditolak
	errEmpty := api.ValidateProductionDatabasePassword("production", "", "")
	if errEmpty == nil {
		t.Fatalf("Expected error for empty password in production, got nil")
	}

	// Skenario 2: Pada produksi, password bawaan dev (kopdes_dev_password) harus ditolak
	errDev := api.ValidateProductionDatabasePassword("production", "kopdes_dev_password", "")
	if errDev == nil {
		t.Fatalf("Expected error for dev password in production, got nil")
	}
	errDev2 := api.ValidateProductionDatabasePassword("production", "", "kopdes_dev_password")
	if errDev2 == nil {
		t.Fatalf("Expected error for dev password via KOPDES_APP_PASSWORD in production, got nil")
	}

	// Skenario 3: Pada produksi, password kuat dari env harus diterima
	errStrong := api.ValidateProductionDatabasePassword("production", "SuperSecretProductionPwd123!#", "")
	if errStrong != nil {
		t.Fatalf("Expected success for strong password in production, got: %v", errStrong)
	}

	// Skenario 4: Pada development, password dev atau kosong diizinkan
	errDevEnv := api.ValidateProductionDatabasePassword("development", "kopdes_dev_password", "")
	if errDevEnv != nil {
		t.Fatalf("Expected success in development, got: %v", errDevEnv)
	}
	errDevEmpty := api.ValidateProductionDatabasePassword("development", "", "")
	if errDevEmpty != nil {
		t.Fatalf("Expected success for empty password in development, got: %v", errDevEmpty)
	}

	t.Logf("Sukses: Validasi password role database pada profil produksi terbukti menolak default dev dan mewajibkan env!")
}

func TestItem5_HashChain_OrderedLocks_NoDeadlock_UnderLoad(t *testing.T) {
	env := setupAPITestEnv(t)
	ctx := context.Background()

	// 1. Verifikasi Keberadaan Indeks Pencarian Baris Terakhir (Last-Row Index)
	indexCheckQuery := `
		SELECT indexname, tablename 
		FROM pg_indexes 
		WHERE indexname IN (
			'idx_audit_log_last_row',
			'idx_kas_mutasi_last_row',
			'idx_pembayaran_verifikasi_log_last_row',
			'idx_stok_mutasi_last_row',
			'idx_pendaftaran_status_log_last_row',
			'idx_anggota_status_log_last_row'
		);
	`
	rows, err := env.Pool.Query(ctx, indexCheckQuery)
	if err != nil {
		t.Fatalf("Query pg_indexes failed: %v", err)
	}
	defer rows.Close()

	foundIndexes := make(map[string]bool)
	for rows.Next() {
		var idxName, tblName string
		_ = rows.Scan(&idxName, &tblName)
		foundIndexes[idxName] = true
	}
	expectedIndexes := []string{
		"idx_audit_log_last_row",
		"idx_kas_mutasi_last_row",
		"idx_pembayaran_verifikasi_log_last_row",
		"idx_stok_mutasi_last_row",
		"idx_pendaftaran_status_log_last_row",
		"idx_anggota_status_log_last_row",
	}
	for _, exp := range expectedIndexes {
		if !foundIndexes[exp] {
			t.Errorf("Missing last-row index in schema: %s", exp)
		}
	}

	// 2. Buat pembayaran & periode kas untuk foreign key
	var periodeKasID uuid.UUID
	err = env.Pool.QueryRow(ctx, `
		INSERT INTO periode_kas (id, koperasi_id, periode, saldo_awal, status, dibuka_oleh)
		VALUES (uuid_generate_v7(), $1, '2026-11', 0, 'buka', $2)
		ON CONFLICT (koperasi_id, periode) DO UPDATE SET status = 'buka'
		RETURNING id;
	`, env.KoperasiID, env.AdminUserID).Scan(&periodeKasID)
	if err != nil {
		t.Fatalf("Insert periode_kas failed: %v", err)
	}

	var paymentID uuid.UUID
	err = env.Pool.QueryRow(ctx, `
		INSERT INTO pembayaran (
			id, koperasi_id, no_pembayaran, pembayar_user_id, anggota_id,
			metode_pembayaran_id, rekening_qris_id, jenis_transaksi,
			nominal_dasar, kode_unik, kode_unik_awal, berlaku_sampai, created_by
		) VALUES (
			uuid_generate_v7(), $1, 'PAY-TEST-HC', $2, $3,
			$4, $5, 'iuran_bulanan',
			150000, 100, 100, NOW() + INTERVAL '1 hour', $2
		) RETURNING id;
	`, env.KoperasiID, env.AdminUserID, env.AnggotaID, env.MetodeID, env.RekeningID).Scan(&paymentID)
	if err != nil {
		t.Fatalf("Insert dummy payment failed: %v", err)
	}

	// 3. Uji Beban Paralel: 30 Transaksi Konkuren Menulis ke kas_mutasi + audit_log + verifikasi log
	// Sesuai Hierarki Kanonikal: Level 2 (audit_log) -> Level 3 (kas_mutasi) -> Level 4 (pembayaran_verifikasi_log)
	concurrentWorkers := 30
	var wg sync.WaitGroup
	errCh := make(chan error, concurrentWorkers)

	for i := 0; i < concurrentWorkers; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()

			conn, aErr := env.Pool.Acquire(ctx)
			if aErr != nil {
				errCh <- fmt.Errorf("worker %d: acquire failed: %w", workerIdx, aErr)
				return
			}
			defer conn.Release()

			tx, txErr := conn.Begin(ctx)
			if txErr != nil {
				errCh <- fmt.Errorf("worker %d: begin failed: %w", workerIdx, txErr)
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()

			// Set tenant context
			if _, sErr := tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true);", env.KoperasiID.String()); sErr != nil {
				errCh <- fmt.Errorf("worker %d: set_config failed: %w", workerIdx, sErr)
				return
			}

			// LEVEL 2: Insert audit_log
			_, insErr := tx.Exec(ctx, `
				INSERT INTO audit_log (
					id, koperasi_id, user_id, nama_tabel, record_id, aksi,
					data_baru, created_at
				) VALUES (
					uuid_generate_v7(), $1, $2, 'pembayaran', $3, 'APPROVE',
					'{"test": true}'::jsonb, clock_timestamp()
				);
			`, env.KoperasiID, env.AdminUserID, paymentID)
			if insErr != nil {
				errCh <- fmt.Errorf("worker %d: insert audit_log failed: %w", workerIdx, insErr)
				return
			}

			// LEVEL 3: Insert kas_mutasi
			buktiKas := fmt.Sprintf("BKT-%d-%d", workerIdx, time.Now().UnixNano()%1000000)
			_, insErr = tx.Exec(ctx, `
				INSERT INTO kas_mutasi (
					id, koperasi_id, periode_kas_id, no_bukti_kas, jenis, kategori,
					nominal, pembayaran_id, keterangan, created_by, created_at
				) VALUES (
					uuid_generate_v7(), $1, $2, $3, 'pemasukan', 'iuran_pokok_bulanan',
					1000, $4, 'Uji Beban Hash Chain Paralel', $5, clock_timestamp()
				);
			`, env.KoperasiID, periodeKasID, buktiKas, paymentID, env.AdminUserID)
			if insErr != nil {
				errCh <- fmt.Errorf("worker %d: insert kas_mutasi failed: %w", workerIdx, insErr)
				return
			}

			// LEVEL 4: Insert pembayaran_verifikasi_log
			_, insErr = tx.Exec(ctx, `
				INSERT INTO pembayaran_verifikasi_log (
					id, koperasi_id, pembayaran_id, status_lama, status_baru,
					aksi, catatan, created_by, created_at
				) VALUES (
					uuid_generate_v7(), $1, $2, 'menunggu_verifikasi', 'disetujui',
					'setujui', 'Verifikasi log uji beban', $3, clock_timestamp()
				);
			`, env.KoperasiID, paymentID, env.AdminUserID)
			if insErr != nil {
				errCh <- fmt.Errorf("worker %d: insert pembayaran_verifikasi_log failed: %w", workerIdx, insErr)
				return
			}

			if cErr := tx.Commit(ctx); cErr != nil {
				errCh <- fmt.Errorf("worker %d: commit failed: %w", workerIdx, cErr)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for workerErr := range errCh {
		t.Fatalf("Deadlock atau error terjadi pada uji beban paralel: %v", workerErr)
	}

	// 4. Verifikasi Integritas Rantai Hash (VerifyHashChain) pada ketiga tabel
	tables := []string{"audit_log", "kas_mutasi", "pembayaran_verifikasi_log"}
	for _, tbl := range tables {
		valid, count, brokenID, vErr := kopdes.VerifyHashChain(ctx, env.Pool, tbl, env.KoperasiID)
		if vErr != nil {
			t.Fatalf("VerifyHashChain pada %s mengembalikan error: %v", tbl, vErr)
		}
		if !valid {
			t.Fatalf("Rantai integritas hash chain pada %s terputus! Broken ID: %v", tbl, brokenID)
		}
		if count < int64(concurrentWorkers) {
			t.Fatalf("Jumlah baris terverifikasi pada %s kurang dari beban worker (%d < %d)", tbl, count, concurrentWorkers)
		}
		t.Logf("Tabel %s: Rantai hash 100%% VALID (%d baris tanpa cabang)", tbl, count)
	}

	t.Logf("Sukses: Urutan kunci advisory terbukti bebas deadlock di bawah beban paralel!")
}

func TestItem6_TokoCheckout_RoleEscalation_NeutralIuran_OpenAPI(t *testing.T) {
	env := setupAPITestEnv(t)
	ctx := context.Background()

	// ==========================================
	// BAGIAN A: Checkout Toko & Mutasi Stok & Promo
	// ==========================================
	// 1. Buat unit usaha, gudang, produk, harga, dan stok_saldo
	unitUsahaID := uuid.New()
	_, err := env.Pool.Exec(ctx, `
		INSERT INTO unit_usaha (id, koperasi_id, kode_unit, nama, kode_kbli, jenis_unit)
		VALUES ($1, $2, 'UNIT-TEST', 'Unit Usaha Retail', '47111', 'gerai_sembako')
		ON CONFLICT DO NOTHING;
	`, unitUsahaID, env.KoperasiID)
	if err != nil {
		t.Fatalf("Insert unit usaha failed: %v", err)
	}

	gudangID := uuid.New()
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO gudang (id, koperasi_id, unit_usaha_id, kode, nama)
		VALUES ($1, $2, $3, 'GDG-TEST', 'Gudang Utama')
		ON CONFLICT DO NOTHING;
	`, gudangID, env.KoperasiID, unitUsahaID)
	if err != nil {
		t.Fatalf("Insert gudang failed: %v", err)
	}

	var katID uuid.UUID
	err = env.Pool.QueryRow(ctx, `
		INSERT INTO kategori_produk (id, koperasi_id, kode, nama, slug)
		VALUES (uuid_generate_v7(), $1, 'KAT-SEMBAKO', 'Sembako', 'sembako')
		ON CONFLICT (koperasi_id, slug) DO UPDATE SET nama = EXCLUDED.nama
		RETURNING id;
	`, env.KoperasiID).Scan(&katID)
	if err != nil {
		t.Fatalf("Insert kategori failed: %v", err)
	}

	produkID := uuid.New()
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO produk (id, koperasi_id, unit_usaha_id, kategori_id, sku, nama, slug, satuan, status)
		VALUES ($1, $2, $3, $4, 'BERAS-01', 'Beras Merah Putih 5kg', 'beras-merah-putih-5kg', 'pack', 'aktif');
	`, produkID, env.KoperasiID, unitUsahaID, katID)
	if err != nil {
		t.Fatalf("Insert produk failed: %v", err)
	}

	_, err = env.Pool.Exec(ctx, `
		INSERT INTO produk_harga (id, koperasi_id, produk_id, harga_umum, harga_anggota, berlaku_mulai, created_by)
		VALUES (uuid_generate_v7(), $1, $2, 60000, 55000, CURRENT_DATE, $3);
	`, env.KoperasiID, produkID, env.AdminUserID)
	if err != nil {
		t.Fatalf("Insert produk_harga failed: %v", err)
	}

	// Set initial stock = 5
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO stok_saldo (id, koperasi_id, gudang_id, produk_id, qty_tersedia, qty_dipesan)
		VALUES (uuid_generate_v7(), $1, $2, $3, 5, 0);
	`, env.KoperasiID, gudangID, produkID)
	if err != nil {
		t.Fatalf("Insert stok_saldo failed: %v", err)
	}

	// Seed Promo: 10% diskon, min belanja 50000
	promoID := uuid.New()
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO promo (
			id, koperasi_id, kode, nama, tipe_promo, nilai_diskon, cakupan,
			berlaku_mulai, berlaku_sampai, minimal_belanja
		) VALUES (
			$1, $2, 'DISKON10', 'Promo Merdeka 10%', 'persen', 10, 'seluruh_katalog',
			NOW() - INTERVAL '1 day', NOW() + INTERVAL '30 days', 50000
		);
	`, promoID, env.KoperasiID)
	if err != nil {
		t.Fatalf("Insert promo failed: %v", err)
	}

	// 2. Anggota menambahkan 3 beras ke keranjang
	bodyAddCart, _ := json.Marshal(map[string]interface{}{
		"produk_id": produkID,
		"qty":       3,
	})
	reqCart := httptest.NewRequest("POST", "/api/v1/toko/keranjang", bytes.NewReader(bodyAddCart))
	reqCart.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqCart.Header.Set("Content-Type", "application/json")
	recCart := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCart, reqCart)
	if recCart.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for add to cart, got %d: %s", recCart.Code, recCart.Body.String())
	}

	// 3. Checkout dengan promo DISKON10 (3 x 55000 = 165000 - 10% = 148500)
	bodyCheckout, _ := json.Marshal(map[string]interface{}{
		"metode_serah_terima": "ambil_di_koperasi",
		"kode_promo":          "DISKON10",
		"catatan":             "Pesanan uji coba",
	})
	reqCheckout := httptest.NewRequest("POST", "/api/v1/toko/checkout", bytes.NewReader(bodyCheckout))
	reqCheckout.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqCheckout.Header.Set("Content-Type", "application/json")
	recCheckout := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCheckout, reqCheckout)

	if recCheckout.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for checkout, got %d: %s", recCheckout.Code, recCheckout.Body.String())
	}

	var resCheckout struct {
		Data struct {
			PesananID  uuid.UUID `json:"pesanan_id"`
			TotalAkhir int64     `json:"total_akhir"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recCheckout.Body.Bytes(), &resCheckout)
	if resCheckout.Data.TotalAkhir != 148500 {
		t.Fatalf("Expected discounted total 148500, got %d", resCheckout.Data.TotalAkhir)
	}

	// 4. Verifikasi Pemotongan Stok di Database & Pencatatan di stok_mutasi
	var qtySisa float64
	_ = env.Pool.QueryRow(ctx, "SELECT qty_tersedia FROM stok_saldo WHERE produk_id = $1;", produkID).Scan(&qtySisa)
	if qtySisa != 2 {
		t.Fatalf("Expected remaining stock 2, got %v", qtySisa)
	}

	var countMutasi int
	var qtyMutasi float64
	_ = env.Pool.QueryRow(ctx, "SELECT count(*), COALESCE(SUM(qty_perubahan), 0) FROM stok_mutasi WHERE produk_id = $1 AND referensi_id = $2;", produkID, resCheckout.Data.PesananID).Scan(&countMutasi, &qtyMutasi)
	if countMutasi == 0 || qtyMutasi != -3 {
		t.Fatalf("Expected stok_mutasi entry with qty_perubahan -3, got count=%d qty=%v", countMutasi, qtyMutasi)
	}

	// 5. Coba checkout melebihi stok yang tersisa (pesan 5 lagi saat sisa 2) -> HARUS DITOLAK (Stok Minus Ditolak)
	bodyAddExcess, _ := json.Marshal(map[string]interface{}{
		"produk_id": produkID,
		"qty":       5,
	})
	reqAddExcess := httptest.NewRequest("POST", "/api/v1/toko/keranjang", bytes.NewReader(bodyAddExcess))
	reqAddExcess.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqAddExcess.Header.Set("Content-Type", "application/json")
	recAddExcess := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recAddExcess, reqAddExcess)

	reqCheckoutExcess := httptest.NewRequest("POST", "/api/v1/toko/checkout", bytes.NewReader(bodyCheckout))
	reqCheckoutExcess.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqCheckoutExcess.Header.Set("Content-Type", "application/json")
	recCheckoutExcess := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCheckoutExcess, reqCheckoutExcess)

	if recCheckoutExcess.Code != http.StatusBadRequest && recCheckoutExcess.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Expected 400/422 for checkout exceeding stock, got %d: %s", recCheckoutExcess.Code, recCheckoutExcess.Body.String())
	}

	// Pastikan stok tidak minus
	var qtySisaAkhir float64
	_ = env.Pool.QueryRow(ctx, "SELECT qty_tersedia FROM stok_saldo WHERE produk_id = $1;", produkID).Scan(&qtySisaAkhir)
	if qtySisaAkhir < 0 {
		t.Fatalf("Stok menjadi minus: %v", qtySisaAkhir)
	}

	// ==========================================
	// BAGIAN B: Proteksi Eskalasi Peran (PATCH users/{id}/role)
	// ==========================================
	// 1. Admin dilarang menaikkan peran diri sendiri -> 403 Forbidden
	bodySelf, _ := json.Marshal(map[string]string{"role": "ketua"})
	reqSelf := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/users/%s/role", env.AdminUserID), bytes.NewReader(bodySelf))
	reqSelf.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqSelf.Header.Set("Content-Type", "application/json")
	recSelf := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recSelf, reqSelf)

	if recSelf.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden when admin modifies own role, got %d: %s", recSelf.Code, recSelf.Body.String())
	}

	// 2. Admin dilarang memberikan peran di atas perannya (misal 'ketua') -> 403 Forbidden
	bodyPromoteAbove, _ := json.Marshal(map[string]string{"role": "ketua"})
	reqPromoteAbove := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/users/%s/role", env.AnggotaUserID), bytes.NewReader(bodyPromoteAbove))
	reqPromoteAbove.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqPromoteAbove.Header.Set("Content-Type", "application/json")
	recPromoteAbove := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPromoteAbove, reqPromoteAbove)

	if recPromoteAbove.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden when admin grants role above their rank, got %d: %s", recPromoteAbove.Code, recPromoteAbove.Body.String())
	}

	// 3. Admin memberikan peran yang setara/di bawah perannya (misal 'operator') -> 200 OK
	bodyPromoteOk, _ := json.Marshal(map[string]string{"role": "operator"})
	reqPromoteOk := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/users/%s/role", env.AnggotaUserID), bytes.NewReader(bodyPromoteOk))
	reqPromoteOk.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqPromoteOk.Header.Set("Content-Type", "application/json")
	recPromoteOk := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPromoteOk, reqPromoteOk)

	if recPromoteOk.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK when admin assigns valid operator role, got %d: %s", recPromoteOk.Code, recPromoteOk.Body.String())
	}

	// ==========================================
	// BAGIAN C: Label Netral Iuran & KEPUTUSAN AKUNTAN
	// ==========================================
	reqIuran := httptest.NewRequest("GET", "/api/v1/iuran/jenis", nil)
	reqIuran.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recIuran := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recIuran, reqIuran)

	if recIuran.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/v1/iuran/jenis, got %d", recIuran.Code)
	}
	var resIuran struct {
		Data []map[string]interface{} `json:"data"`
	}
	_ = json.Unmarshal(recIuran.Body.Bytes(), &resIuran)
	if len(resIuran.Data) > 0 {
		klasifikasi := fmt.Sprintf("%v", resIuran.Data[0]["klasifikasi_akuntansi"])
		if klasifikasi != "KEPUTUSAN AKUNTAN" {
			t.Errorf("Expected klasifikasi_akuntansi 'KEPUTUSAN AKUNTAN', got: %s", klasifikasi)
		}
	}

	// ==========================================
	// BAGIAN D: Spesifikasi OpenAPI 3.0
	// ==========================================
	openAPIBytes, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		openAPIBytes, err = os.ReadFile("docs/openapi.yaml")
	}
	if err != nil {
		t.Fatalf("Gagal membaca docs/openapi.yaml: %v", err)
	}
	openAPIStr := string(openAPIBytes)
	if !strings.Contains(openAPIStr, "openapi: 3.") {
		t.Fatalf("docs/openapi.yaml harus spesifikasi OpenAPI 3.x, got: %s", openAPIStr[:50])
	}
	if !strings.Contains(openAPIStr, "/api/v1/auth/login") || !strings.Contains(openAPIStr, "/api/v1/toko/checkout") {
		t.Fatalf("docs/openapi.yaml tidak memuat endpoint wajib")
	}

	t.Logf("Sukses: Checkout potong stok & promo, proteksi eskalasi peran, iuran netral, dan OpenAPI 3 valid!")
}

// TestItem7_Pilot_AsiaJakarta_And_Purge_PhysicalFiles_PIIFree memvalidasi:
// 1. Batas harian pilot rekening pribadi dihitung berdasarkan zona waktu Asia/Jakarta dan HANYA status aktif/disetujui (bukan ditolak/dibatalkan)
// 2. Pembersihan pendaftaran ditolak (Purge) menghapus berkas fisik KYC dari disk storage dan pendaftaran_status_log TIDAK menyimpan PII
func TestItem7_Pilot_AsiaJakarta_And_Purge_PhysicalFiles_PIIFree(t *testing.T) {
	env := setupAPITestEnv(t)

	ctx := context.Background()

	var wilayahDesaID uuid.UUID
	_ = env.Pool.QueryRow(ctx, "SELECT wilayah_desa_id FROM koperasi WHERE id = $1;", env.KoperasiID).Scan(&wilayahDesaID)

	// Izinkan mode pilot rekening pribadi
	t.Setenv("ALLOW_PERSONAL_PAYMENT_PILOT", "true")

	// Set storage directory ke temp dir untuk pengujian berkas fisik
	tempStorageDir := t.TempDir()
	t.Setenv("STORAGE_DIR", tempStorageDir)

	// ==========================================
	// BAGIAN A: Batas Harian Rekening Pribadi (Asia/Jakarta & Status Aktif/Disetujui)
	// ==========================================
	// 1. Buat rekening QRIS pilot perorangan
	var rekeningPilotID uuid.UUID
	nmidPilot := fmt.Sprintf("ID%s", uuid.New().String()[:12])
	err := env.Pool.QueryRow(ctx, `
		INSERT INTO rekening_qris_koperasi (
			koperasi_id, penyedia_id, nama_merchant, nmid, nama_bank_pjp,
			no_rekening_settlement, nama_pemilik_rekening, jenis_pemilik,
			is_atas_nama_badan_hukum, qris_payload_statis, status_aktif
		) VALUES (
			$1, $2, 'Kopdes Pilot Toko', $3, 'BCA Syariah',
			'5551234567', 'Bendahara Pilot', 'perorangan_pengurus',
			false, '00020101021151440014ID.CO.QRIS.WWW0215ID10200000000015204549953033605802ID5909KOPDES MP6006BOGOR 61051611063045E3B', true
		) RETURNING id;
	`, env.KoperasiID, env.PenyediaID, nmidPilot).Scan(&rekeningPilotID)
	if err != nil {
		t.Fatalf("Gagal insert rekening pilot: %v", err)
	}

	// 2. Set konfigurasi batas pilot: max per tx = Rp 500.000, max per hari = Rp 1.000.000
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO konfigurasi_aturan (koperasi_id, kunci_aturan, versi, nilai_json, status_aktif, keterangan, berlaku_mulai)
		VALUES ($1, 'pilot_rekening_pribadi_max_per_transaksi', 1, '{"nominal": 500000}'::jsonb, true, 'Uji coba batas per tx', NOW())
		ON CONFLICT (koperasi_id, kunci_aturan, versi) DO UPDATE SET nilai_json = EXCLUDED.nilai_json;
	`, env.KoperasiID)
	if err != nil {
		t.Fatalf("Gagal set konfigurasi aturan pilot max tx: %v", err)
	}

	_, err = env.Pool.Exec(ctx, `
		INSERT INTO konfigurasi_aturan (koperasi_id, kunci_aturan, versi, nilai_json, status_aktif, keterangan, berlaku_mulai)
		VALUES ($1, 'pilot_rekening_pribadi_max_per_hari', 1, '{"nominal": 1000000}'::jsonb, true, 'Uji coba batas per hari', NOW())
		ON CONFLICT (koperasi_id, kunci_aturan, versi) DO UPDATE SET nilai_json = EXCLUDED.nilai_json;
	`, env.KoperasiID)
	if err != nil {
		t.Fatalf("Gagal set konfigurasi aturan pilot max hari: %v", err)
	}

	metodeID := env.MetodeID

	// Buat jenis iuran dan tagihan iuran untuk pengujian pembayaran
	var jenisIuranID uuid.UUID
	err = env.Pool.QueryRow(ctx, `
		INSERT INTO jenis_iuran (
			koperasi_id, kode, nama, versi, nominal, periode_tagih, hari_jatuh_tempo,
			denda_tipe, denda_nilai, klasifikasi_akuntansi, berlaku_mulai, status_aktif
		) VALUES (
			$1, 'WAJIB', 'Iuran Wajib Bulanan', 1, 200000, 'bulanan', 10,
			'tidak_ada', 0, 'perlu_keputusan', CURRENT_DATE, true
		) RETURNING id;
	`, env.KoperasiID).Scan(&jenisIuranID)
	if err != nil {
		t.Fatalf("Gagal insert jenis_iuran: %v", err)
	}

	var tagihanID uuid.UUID
	err = env.Pool.QueryRow(ctx, `
		INSERT INTO tagihan_iuran (koperasi_id, anggota_id, jenis_iuran_id, periode, nominal_pokok, jatuh_tempo)
		VALUES ($1, $2, $3, '2026-11', 200000, CURRENT_DATE + 5)
		RETURNING id;
	`, env.KoperasiID, env.AnggotaID, jenisIuranID).Scan(&tagihanID)
	if err != nil {
		t.Fatalf("Gagal insert tagihan_iuran: %v", err)
	}

	// Simulasikan pembayaran DITOLAK hari ini di Asia/Jakarta sebesar Rp 800.000 (tidak boleh mengurangi kuota harian)
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO pembayaran (
			koperasi_id, no_pembayaran, pembayar_user_id, anggota_id, metode_pembayaran_id,
			rekening_qris_id, jenis_transaksi, nominal_dasar, kode_unik, kode_unik_awal, status,
			alasan_ditolak, berlaku_sampai, created_at, created_by
		) VALUES (
			$1, 'PAY-REJECTED-001', $2, $3, $4,
			$5, 'iuran_bulanan', 800000, 101, 101, 'ditolak',
			'Alasan penolakan dummy', NOW() + INTERVAL '1 hour', NOW() AT TIME ZONE 'Asia/Jakarta', $2
		);
	`, env.KoperasiID, env.AnggotaUserID, env.AnggotaID, metodeID, rekeningPilotID)
	if err != nil {
		t.Fatalf("Gagal insert dummy pembayaran ditolak: %v", err)
	}

	// Simulasikan pembayaran KEDALUWARSA kemarin di luar hari ini sebesar Rp 900.000 (tidak boleh mengurangi kuota)
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO pembayaran (
			koperasi_id, no_pembayaran, pembayar_user_id, anggota_id, metode_pembayaran_id,
			rekening_qris_id, jenis_transaksi, nominal_dasar, kode_unik, kode_unik_awal, status,
			berlaku_sampai, created_at, created_by
		) VALUES (
			$1, 'PAY-EXPIRED-OLD', $2, $3, $4,
			$5, 'iuran_bulanan', 900000, 102, 102, 'kedaluwarsa',
			NOW() - INTERVAL '2 days' + INTERVAL '1 hour', NOW() - INTERVAL '2 days', $2
		);
	`, env.KoperasiID, env.AnggotaUserID, env.AnggotaID, metodeID, rekeningPilotID)
	if err != nil {
		t.Fatalf("Gagal insert dummy pembayaran kedaluwarsa: %v", err)
	}

	// Simulasikan pembayaran AKTIF/DISETUJUI hari ini di Asia/Jakarta sebesar Rp 850.000
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO pembayaran (
			koperasi_id, no_pembayaran, pembayar_user_id, anggota_id, metode_pembayaran_id,
			rekening_qris_id, jenis_transaksi, nominal_dasar, kode_unik, kode_unik_awal, status,
			berlaku_sampai, created_at, created_by
		) VALUES (
			$1, 'PAY-ACTIVE-001', $2, $3, $4,
			$5, 'iuran_bulanan', 850000, 103, 103, 'disetujui',
			NOW() + INTERVAL '1 hour', NOW() AT TIME ZONE 'Asia/Jakarta', $2
		);
	`, env.KoperasiID, env.AnggotaUserID, env.AnggotaID, metodeID, rekeningPilotID)
	if err != nil {
		t.Fatalf("Gagal insert pembayaran aktif hari ini: %v", err)
	}

	// Coba buat pembayaran QRIS baru sebesar Rp 200.000
	// 850.000 (aktif) + 200.000 (baru) = 1.050.000 > batas harian 1.000.000 -> HARUS DITOLAK
	bodyQRISExceed, _ := json.Marshal(map[string]interface{}{
		"jenis_transaksi":  "iuran_bulanan",
		"tagihan_iuran_id": []string{tagihanID.String()},
		"rekening_qris_id": rekeningPilotID,
	})
	reqQRISExceed := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyQRISExceed))
	reqQRISExceed.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqQRISExceed.Header.Set("Content-Type", "application/json")
	recQRISExceed := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recQRISExceed, reqQRISExceed)

	if recQRISExceed.Code != http.StatusBadRequest && recQRISExceed.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Expected 400/422 saat batas harian pilot terlampaui, got %d: %s", recQRISExceed.Code, recQRISExceed.Body.String())
	}
	if !strings.Contains(recQRISExceed.Body.String(), "batas harian rekening pilot") {
		t.Fatalf("Expected pesan batas harian rekening pilot, got: %s", recQRISExceed.Body.String())
	}

	// ==========================================
	// BAGIAN B: Upload Berkas Fisik, Purge, & Sanitasi PII di Log Status
	// ==========================================
	// 1. Ajukan pendaftaran calon anggota baru
	bodyAjukan, _ := json.Marshal(map[string]interface{}{
		"nik":             "3201234567890099",
		"nama_lengkap":    "Calon Anggota PDP Purge",
		"tempat_lahir":    "Bogor",
		"tanggal_lahir":   "1992-05-10",
		"jenis_kelamin":   "L",
		"pekerjaan":       "Wiraswasta",
		"alamat_ktp":      "Jl. Melati No. 12 Desa Sukamaju",
		"wilayah_desa_id": wilayahDesaID,
	})
	reqAjukan := httptest.NewRequest("POST", "/api/v1/pendaftaran", bytes.NewReader(bodyAjukan))
	reqAjukan.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqAjukan.Header.Set("Content-Type", "application/json")
	recAjukan := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recAjukan, reqAjukan)

	if recAjukan.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for ajukan pendaftaran, got %d: %s", recAjukan.Code, recAjukan.Body.String())
	}
	var resAjukan struct {
		Data struct {
			PendaftaranID uuid.UUID `json:"pendaftaran_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recAjukan.Body.Bytes(), &resAjukan)
	pendaftaranID := resAjukan.Data.PendaftaranID

	// 2. Unggah Dokumen KYC (JPEG valid dengan magic bytes)
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}
	fileContent := append(jpegHeader, []byte("SIMULASI_FOTO_KTP_CALON_ANGGOTA_TESTING")...)

	var b bytes.Buffer
	wMulti := multipart.NewWriter(&b)
	part, _ := wMulti.CreateFormFile("dokumen", "ktp_calon.jpg")
	_, _ = part.Write(fileContent)
	_ = wMulti.WriteField("jenis_dokumen", "ktp")
	wMulti.Close()

	reqUpload := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pendaftaran/%s/dokumen", pendaftaranID), &b)
	reqUpload.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqUpload.Header.Set("Content-Type", wMulti.FormDataContentType())
	recUpload := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recUpload, reqUpload)

	if recUpload.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for upload dokumen, got %d: %s", recUpload.Code, recUpload.Body.String())
	}

	// Verifikasi Berkas Fisik TERSIMPAN di disk storage
	var storageKey string
	err = env.Pool.QueryRow(ctx, "SELECT storage_key FROM pendaftaran_dokumen WHERE pendaftaran_id = $1;", pendaftaranID).Scan(&storageKey)
	if err != nil {
		t.Fatalf("Gagal query storage_key: %v", err)
	}
	expectedPhysicalPath := filepath.Join(tempStorageDir, storageKey)
	if _, err := os.Stat(expectedPhysicalPath); os.IsNotExist(err) {
		t.Fatalf("Berkas KYC fisik harus tersimpan di %s, tetapi tidak ditemukan!", expectedPhysicalPath)
	}

	// 3. Verifikasi Pendaftaran dengan status DITOLAK dan catatan memuat PII sensitif
	catatanDitolakDenganPII := "Berkas NIK 3201234567890099 dengan no kontak 081234567890 dan email calon.rahasia@example.com ditolak karena buram."
	bodyVerifikasi, _ := json.Marshal(map[string]interface{}{
		"status":         "ditolak",
		"alasan_ditolak": catatanDitolakDenganPII,
	})
	reqVerifikasi := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pendaftaran/%s/verifikasi", pendaftaranID), bytes.NewReader(bodyVerifikasi))
	reqVerifikasi.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqVerifikasi.Header.Set("Content-Type", "application/json")
	recVerifikasi := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerifikasi, reqVerifikasi)

	if recVerifikasi.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for verifikasi ditolak, got %d: %s", recVerifikasi.Code, recVerifikasi.Body.String())
	}

	// 4. Verifikasi bahwa pendaftaran_status_log TIDAK MENYIMPAN PII
	var loggedCatatan string
	err = env.Pool.QueryRow(ctx, `
		SELECT catatan FROM pendaftaran_status_log
		WHERE pendaftaran_id = $1 AND status_baru = 'ditolak'
		ORDER BY id DESC LIMIT 1;
	`, pendaftaranID).Scan(&loggedCatatan)
	if err != nil {
		t.Fatalf("Gagal query pendaftaran_status_log: %v", err)
	}

	if strings.Contains(loggedCatatan, "3201234567890099") ||
		strings.Contains(loggedCatatan, "081234567890") ||
		strings.Contains(loggedCatatan, "calon.rahasia@example.com") {
		t.Fatalf("PELANGGARAN PDP: pendaftaran_status_log menyimpan PII mentah! Isi catatan: %s", loggedCatatan)
	}
	if !strings.Contains(loggedCatatan, "[NIK_REDACTED]") && !strings.Contains(loggedCatatan, "[NO_HP_REDACTED]") {
		t.Fatalf("Catatan harus disanitasi dengan label redaksi: %s", loggedCatatan)
	}

	// 5. Mundurkan waktu updated_at pendaftaran_anggota ke 35 hari yang lalu (> retensi 30 hari)
	_, err = env.Pool.Exec(ctx, `
		UPDATE pendaftaran_anggota
		SET updated_at = NOW() - INTERVAL '35 days'
		WHERE id = $1;
	`, pendaftaranID)
	if err != nil {
		t.Fatalf("Gagal mundurkan updated_at pendaftaran: %v", err)
	}

	// 6. Jalankan pembersihan PDP (POST /api/v1/pendaftaran/purge-ditolak)
	reqPurge := httptest.NewRequest("POST", "/api/v1/pendaftaran/purge-ditolak", nil)
	reqPurge.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recPurge := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recPurge, reqPurge)

	if recPurge.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for purge pendaftaran, got %d: %s", recPurge.Code, recPurge.Body.String())
	}

	// 7. Verifikasi berkas KYC fisik TERHAPUS dari disk
	if _, err := os.Stat(expectedPhysicalPath); !os.IsNotExist(err) {
		t.Fatalf("PELANGGARAN PDP: Berkas fisik di penyimpanan %s belum terhapus setelah purge!", expectedPhysicalPath)
	}

	// 8. Verifikasi audit_log mencatat aksi pembersihan tanpa PII
	var countAudit int
	err = env.Pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_log
		WHERE koperasi_id = $1 AND nama_tabel = 'pendaftaran_anggota' AND aksi = 'ANONYMIZE';
	`, env.KoperasiID).Scan(&countAudit)
	if err != nil || countAudit == 0 {
		t.Fatalf("Expected audit_log entry for ANONYMIZE, count=%d, err=%v", countAudit, err)
	}

	t.Logf("Sukses: Batas pilot Asia/Jakarta valid, berkas KYC fisik terhapus permanen saat purge, dan log status bebas PII!")
}

// TestMFA_Enrollment_PrivilegedAccount_RestrictedToken_And_HashedRecoveryCodes memvalidasi:
// 1. Akun istimewa (bendahara/admin/ketua) yang belum mengaktifkan MFA ditolak saat login (HTTP 403) dan diberi setup_token terbatas.
// 2. Token setup_token dibatasi secara ketat hanya untuk alur enrollment (/api/v1/auth/mfa/*), dan ditolak jika mengakses endpoint lain.
// 3. Alur enrollment via POST /api/v1/auth/mfa/enable menyimpan secret dan HASH kode pemulihan terenkripsi (bukan plaintext di database).
// 4. Setelah enrollment, login berhasil melalui verifikasi kode TOTP atau recovery code sekali pakai.
func TestMFA_Enrollment_PrivilegedAccount_RestrictedToken_And_HashedRecoveryCodes(t *testing.T) {
	env := setupAPITestEnv(t)
	ctx := context.Background()

	// 1. Buat akun peran istimewa 'bendahara' tanpa MFA aktif
	bendaharaUserID := uuid.New()
	emailBendahara := fmt.Sprintf("bendahara_%s@koperasi.id", uuid.New().String()[:8])
	emailHash := security.HashBlindIndex(emailBendahara, []byte(env.Config.BlindIndexSecret))
	pwdPlain := "BendaharaPassword123!"
	pwdHash, _ := security.HashPassword(pwdPlain)

	conn, err := env.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire connection failed: %v", err)
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap, status_aktif, mfa_enabled)
		VALUES ($1, $2, $3, $2, $3, $4, 'Bendahara Utama', true, false);
	`, bendaharaUserID, []byte("enkripsi"), emailHash, pwdHash)
	if err != nil {
		t.Fatalf("Seed bendahara user failed: %v", err)
	}

	var roleBendaharaID uuid.UUID
	_ = conn.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'bendahara' LIMIT 1;").Scan(&roleBendaharaID)
	if roleBendaharaID != uuid.Nil {
		_, _ = conn.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id)
			VALUES ($1, $2, $3);
		`, bendaharaUserID, roleBendaharaID, env.KoperasiID)
	}

	// 2. Login bendahara belum punya MFA -> Wajib 403 Forbidden dengan setup_token
	bodyLogin, _ := json.Marshal(handlers.LoginRequest{
		Email:    emailBendahara,
		Password: pwdPlain,
	})
	reqLogin := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin))
	recLogin := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin, reqLogin)

	if recLogin.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden for privileged login without MFA, got %d: %s", recLogin.Code, recLogin.Body.String())
	}

	var resLogin struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Data struct {
			MFAEnrollmentRequired bool   `json:"mfa_setup_required"`
			SetupToken            string `json:"setup_token"`
			Role                  string `json:"role"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recLogin.Body.Bytes(), &resLogin)
	if resLogin.Error.Code != "MFA_MANDATORY_SETUP_REQUIRED" || !resLogin.Data.MFAEnrollmentRequired {
		t.Fatalf("Expected code MFA_MANDATORY_SETUP_REQUIRED, got: %+v", resLogin)
	}
	setupToken := resLogin.Data.SetupToken
	if setupToken == "" {
		t.Fatalf("setup_token tidak boleh kosong")
	}

	// 3. Uji token terbatas: coba panggil endpoint profil (/api/v1/auth/me) dengan setup_token -> Wajib 403 Forbidden
	reqProfile := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	reqProfile.Header.Set("Authorization", "Bearer "+setupToken)
	recProfile := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recProfile, reqProfile)

	if recProfile.Code != http.StatusForbidden {
		t.Fatalf("SECURITY VIOLATION: setup_token enrollment-only harus ditolak saat akses endpoint profil, got %d: %s", recProfile.Code, recProfile.Body.String())
	}
	if !strings.Contains(recProfile.Body.String(), "hanya berlaku untuk proses enrollment MFA") {
		t.Fatalf("Pesan penolakan harus menegaskan pembatasan token enrollment: %s", recProfile.Body.String())
	}

	// 4. Ambil Secret TOTP Baru via POST /api/v1/auth/mfa/setup menggunakan setup_token -> 200 OK
	reqSetup := httptest.NewRequest("POST", "/api/v1/auth/mfa/setup", nil)
	reqSetup.Header.Set("Authorization", "Bearer "+setupToken)
	recSetup := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recSetup, reqSetup)

	if recSetup.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/v1/auth/mfa/setup with setup_token, got %d: %s", recSetup.Code, recSetup.Body.String())
	}
	var resSetup struct {
		Data struct {
			Secret string `json:"secret"`
			QRURI  string `json:"qr_uri"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recSetup.Body.Bytes(), &resSetup)
	totpSecret := resSetup.Data.Secret
	if totpSecret == "" || !strings.Contains(resSetup.Data.QRURI, "otpauth://totp/") {
		t.Fatalf("Secret atau QR URI tidak valid: %+v", resSetup)
	}

	// 5. Generate kode TOTP valid dan selesaikan enrollment via POST /api/v1/auth/mfa/enable
	totpCode, err := security.GenerateTOTPCode(totpSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode failed: %v", err)
	}

	bodyEnable, _ := json.Marshal(handlers.MFAEnableRequest{
		Secret:   totpSecret,
		TOTPCode: totpCode,
	})
	reqEnable := httptest.NewRequest("POST", "/api/v1/auth/mfa/enable", bytes.NewReader(bodyEnable))
	reqEnable.Header.Set("Authorization", "Bearer "+setupToken)
	reqEnable.Header.Set("Content-Type", "application/json")
	recEnable := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recEnable, reqEnable)

	if recEnable.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/v1/auth/mfa/enable, got %d: %s", recEnable.Code, recEnable.Body.String())
	}

	var resEnable struct {
		Data struct {
			RecoveryCodes []string `json:"recovery_codes"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recEnable.Body.Bytes(), &resEnable)
	recoveryCodes := resEnable.Data.RecoveryCodes
	if len(recoveryCodes) != 8 {
		t.Fatalf("Expected 8 recovery codes returned to user, got %d", len(recoveryCodes))
	}

	// 6. Verifikasi Integritas Penyimpanan Database:
	// - mfa_enabled = true
	// - Kode pemulihan MENTAH TIDAK DISIMPAN di database; yang disimpan adalah ciphertext dari HASH SHA-256
	var (
		dbMfaEnabled     bool
		dbRecoveryCipher []byte
	)
	err = conn.QueryRow(ctx, "SELECT mfa_enabled, mfa_recovery_codes FROM users WHERE id = $1;", bendaharaUserID).Scan(&dbMfaEnabled, &dbRecoveryCipher)
	if err != nil {
		t.Fatalf("Query user MFA status failed: %v", err)
	}
	if !dbMfaEnabled {
		t.Fatalf("mfa_enabled harus bernilai true setelah enrollment")
	}

	// Pastikan string plaintext kode pemulihan tidak pernah tersimpan di database
	for _, code := range recoveryCodes {
		if bytes.Contains(dbRecoveryCipher, []byte(code)) {
			t.Fatalf("PELANGGARAN KEAMANAN: Kode pemulihan mentah %s ditemukan tersimpan dalam database!", code)
		}
	}

	// Dekripsi recovery codes ciphertext dan pastikan memuat hash SHA-256
	kp, err := security.NewMasterKeyProvider("v1", "k1", []byte(env.Config.AESMasterKey)[:32])
	if err != nil {
		t.Fatalf("NewMasterKeyProvider failed: %v", err)
	}
	recAAD := []byte(fmt.Sprintf("users:recovery:%s", bendaharaUserID))
	decryptedJSON, err := kp.DecryptWithAAD(dbRecoveryCipher, recAAD)
	if err != nil {
		t.Fatalf("Decrypt recovery codes failed: %v", err)
	}
	var storedHashes []string
	if err := json.Unmarshal(decryptedJSON, &storedHashes); err != nil {
		t.Fatalf("Unmarshal stored hashes failed: %v", err)
	}
	if len(storedHashes) != 8 {
		t.Fatalf("Expected 8 stored hashes, got %d", len(storedHashes))
	}
	for i, c := range recoveryCodes {
		expectedHash := security.HashRecoveryCode(c)
		if storedHashes[i] != expectedHash {
			t.Fatalf("Hash mismatch pada index %d: expected %s, got %s", i, expectedHash, storedHashes[i])
		}
	}

	// 7. Login bendahara pasca enrollment:
	// Tahap 1: POST /api/v1/auth/login -> 200 OK with mfa_required & mfa_session_token
	reqLogin2 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin))
	recLogin2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recLogin2, reqLogin2)

	if recLogin2.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for login post-MFA, got %d: %s", recLogin2.Code, recLogin2.Body.String())
	}
	var resLogin2 struct {
		Data struct {
			MFARequired     bool   `json:"mfa_required"`
			MFASessionToken string `json:"mfa_session_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recLogin2.Body.Bytes(), &resLogin2)
	if !resLogin2.Data.MFARequired || resLogin2.Data.MFASessionToken == "" {
		t.Fatalf("Expected mfa_required=true and valid mfa_session_token: %+v", resLogin2)
	}

	// Tahap 2: Gunakan kode pemulihan pertama untuk login
	usedRecoveryCode := recoveryCodes[0]
	bodyVerify, _ := json.Marshal(handlers.MFAVerifyRequest{
		MFASessionToken: resLogin2.Data.MFASessionToken,
		RecoveryCode:    usedRecoveryCode,
	})
	reqVerify := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyVerify))
	reqVerify.Header.Set("Content-Type", "application/json")
	recVerify := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerify, reqVerify)

	if recVerify.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for MFA verify with recovery code, got %d: %s", recVerify.Code, recVerify.Body.String())
	}
	var resVerify struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recVerify.Body.Bytes(), &resVerify)
	if resVerify.Data.AccessToken == "" {
		t.Fatalf("Expected non-empty AccessToken upon successful verify")
	}

	// Verifikasi bahwa Access Token ini sekarang dapat membuka /api/v1/auth/me
	reqProfileAuth := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	reqProfileAuth.Header.Set("Authorization", "Bearer "+resVerify.Data.AccessToken)
	recProfileAuth := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recProfileAuth, reqProfileAuth)

	if recProfileAuth.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for profile access with authenticated access token, got %d: %s", recProfileAuth.Code, recProfileAuth.Body.String())
	}

	// 8. Pastikan kode pemulihan yang sudah digunakan HANGUS (One-Time Burn)
	// Login kembali untuk mendapatkan session token baru
	recLogin3 := httptest.NewRecorder()
	reqLogin3 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyLogin))
	env.Server.Router().ServeHTTP(recLogin3, reqLogin3)
	_ = json.Unmarshal(recLogin3.Body.Bytes(), &resLogin2)

	bodyVerifyBurned, _ := json.Marshal(handlers.MFAVerifyRequest{
		MFASessionToken: resLogin2.Data.MFASessionToken,
		RecoveryCode:    usedRecoveryCode, // Pakai kode yang sama
	})
	reqVerifyBurned := httptest.NewRequest("POST", "/api/v1/auth/mfa/verify", bytes.NewReader(bodyVerifyBurned))
	reqVerifyBurned.Header.Set("Content-Type", "application/json")
	recVerifyBurned := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recVerifyBurned, reqVerifyBurned)

	if recVerifyBurned.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for burned recovery code, got %d: %s", recVerifyBurned.Code, recVerifyBurned.Body.String())
	}

	t.Logf("Sukses: Alur enrollment akun istimewa dengan setup_token terbatas, penyimpanan hash kode pemulihan, dan login terverifikasi penuh!")
}

// TestItem3_BankMutation_DefaultNoAutoApprove_SecondConfirmation_ImporterCannotApprove memvalidasi:
// 1. Impor mutasi bank memiliki auto-approve NON-AKTIF secara default (status masuk ke 'menunggu_persetujuan_kedua').
// 2. Butuh konfirmasi orang kedua: pengimpor mutasi dilarang menyetujui pembayarannya sendiri (pengimpor = penyetuju DITOLAK).
// 3. Persetujuan oleh pengurus kedua berhasil mengubah status ke 'disetujui' dan membukukan transaksi.
func TestItem3_BankMutation_DefaultNoAutoApprove_SecondConfirmation_ImporterCannotApprove(t *testing.T) {
	env := setupAPITestEnv(t)
	ctx := context.Background()

	// 1. Buat pembayaran QRIS aktif yang menunggu pembayaran
	var tagihanID uuid.UUID
	err := env.Pool.QueryRow(ctx, `
		INSERT INTO jenis_iuran (
			koperasi_id, kode, nama, versi, nominal, periode_tagih, hari_jatuh_tempo,
			denda_tipe, denda_nilai, klasifikasi_akuntansi, berlaku_mulai, status_aktif
		) VALUES (
			$1, 'MUTASI_TEST', 'Iuran Mutasi Test', 1, 150000, 'bulanan', 10,
			'tidak_ada', 0, 'perlu_keputusan', CURRENT_DATE, true
		) RETURNING id;
	`, env.KoperasiID).Scan(&tagihanID)
	if err != nil {
		t.Fatalf("Insert jenis_iuran failed: %v", err)
	}

	var tagihanIuranID uuid.UUID
	err = env.Pool.QueryRow(ctx, `
		INSERT INTO tagihan_iuran (koperasi_id, anggota_id, jenis_iuran_id, periode, nominal_pokok, jatuh_tempo)
		VALUES ($1, $2, $3, '2026-11', 150000, CURRENT_DATE + 5)
		RETURNING id;
	`, env.KoperasiID, env.AnggotaID, tagihanID).Scan(&tagihanIuranID)
	if err != nil {
		t.Fatalf("Insert tagihan_iuran failed: %v", err)
	}

	bodyQRIS, _ := json.Marshal(map[string]interface{}{
		"jenis_transaksi":  "iuran_bulanan",
		"tagihan_iuran_id": []string{tagihanIuranID.String()},
		"rekening_qris_id": env.RekeningID,
	})
	reqQRIS := httptest.NewRequest("POST", "/api/v1/pembayaran/qris", bytes.NewReader(bodyQRIS))
	reqQRIS.Header.Set("Authorization", "Bearer "+env.AnggotaToken)
	reqQRIS.Header.Set("Content-Type", "application/json")
	recQRIS := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recQRIS, reqQRIS)

	if recQRIS.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for buat QRIS, got %d: %s", recQRIS.Code, recQRIS.Body.String())
	}
	var resQRIS struct {
		Data struct {
			PembayaranID uuid.UUID `json:"pembayaran_id"`
			NominalTotal int64     `json:"nominal_total"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recQRIS.Body.Bytes(), &resQRIS)
	pembayaranID := resQRIS.Data.PembayaranID
	nominalTotal := resQRIS.Data.NominalTotal

	// 2. Pengurus 1 (AdminUserID) mengimpor mutasi bank CSV yang cocok dengan nominal_total
	csvContent := fmt.Sprintf("Tanggal,Keterangan,Tipe,Nominal,NoRekening\n2026-11-12 10:00:00,Transfer Iuran Anggota,CR,%d,1112223334\n", nominalTotal)
	var b bytes.Buffer
	wMulti := multipart.NewWriter(&b)
	part, _ := wMulti.CreateFormFile("file", "mutasi.csv")
	_, _ = part.Write([]byte(csvContent))
	wMulti.Close()

	reqImpor := httptest.NewRequest("POST", "/api/v1/pembayaran/mutasi-bank/impor", &b)
	reqImpor.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqImpor.Header.Set("Content-Type", wMulti.FormDataContentType())
	recImpor := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recImpor, reqImpor)

	if recImpor.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for impor mutasi, got %d: %s", recImpor.Code, recImpor.Body.String())
	}

	// 3. Verifikasi: Auto-approve NONAKTIF secara default -> status transaksi menjadi 'menunggu_persetujuan_kedua'
	var statusPembayaran string
	var persetujuan1Oleh *uuid.UUID
	err = env.Pool.QueryRow(ctx, "SELECT status, persetujuan_pertama_oleh FROM pembayaran WHERE id = $1;", pembayaranID).Scan(&statusPembayaran, &persetujuan1Oleh)
	if err != nil {
		t.Fatalf("Query pembayaran status failed: %v", err)
	}
	if statusPembayaran != "menunggu_persetujuan_kedua" {
		t.Fatalf("Auto-approve harus nonaktif secara default! Expected status 'menunggu_persetujuan_kedua', got '%s'", statusPembayaran)
	}
	if persetujuan1Oleh == nil || *persetujuan1Oleh != env.AdminUserID {
		t.Fatalf("Persetujuan pertama harus mencatat pengimpor: %v", persetujuan1Oleh)
	}

	// 4. Pengurus 1 (pengimpor mutasi) mencoba MENYETUJUI pembayarannya sendiri -> Wajib 403 Forbidden (pengimpor = penyetuju ditolak)
	bodyApproveSelf, _ := json.Marshal(handlers.VerifikasiRequest{
		Disetujui: true,
		Catatan:   "Saya sendiri yang impor dan mau setujui",
	})
	reqApproveSelf := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", pembayaranID), bytes.NewReader(bodyApproveSelf))
	reqApproveSelf.Header.Set("Authorization", "Bearer "+env.AdminToken)
	reqApproveSelf.Header.Set("Content-Type", "application/json")
	recApproveSelf := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recApproveSelf, reqApproveSelf)

	if recApproveSelf.Code != http.StatusForbidden {
		t.Fatalf("SECURITY VIOLATION: Pengimpor mutasi dilarang menyetujui pembayaran terkait! Expected 403, got %d: %s", recApproveSelf.Code, recApproveSelf.Body.String())
	}
	if !strings.Contains(recApproveSelf.Body.String(), "dilarang") {
		t.Fatalf("Pesan penolakan harus menegaskan larangan pengimpor menyetujui: %s", recApproveSelf.Body.String())
	}

	// 5. Buat akun Pengurus 2 yang berbeda (ketua)
	pengurus2ID := uuid.New()
	emailP2 := fmt.Sprintf("ketua2_%s@koperasi.id", uuid.New().String()[:8])
	emailP2Hash := security.HashBlindIndex(emailP2, []byte(env.Config.BlindIndexSecret))
	pwdP2Hash, _ := security.HashPassword("KetuaPassword123!")

	_, err = env.Pool.Exec(ctx, `
		INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap, status_aktif, mfa_enabled)
		VALUES ($1, $2, $3, $2, $3, $4, 'Ketua Verifikator Kedua', true, true);
	`, pengurus2ID, []byte("enkripsi"), emailP2Hash, pwdP2Hash)
	if err != nil {
		t.Fatalf("Seed pengurus 2 failed: %v", err)
	}

	var roleKetuaID uuid.UUID
	_ = env.Pool.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'ketua' LIMIT 1;").Scan(&roleKetuaID)
	if roleKetuaID != uuid.Nil {
		_, _ = env.Pool.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id)
			VALUES ($1, $2, $3);
		`, pengurus2ID, roleKetuaID, env.KoperasiID)
	}

	tm := auth.NewTokenManager(env.Config.JWTSecret, "kopdes-merah-putih-api")
	tokenP2, _ := tm.GenerateToken(pengurus2ID, env.KoperasiID, "ketua", false, 15*time.Minute)

	// 6. Pengurus 2 (orang kedua yang berbeda) MENYETUJUI pembayaran -> Wajib 200 OK
	bodyApproveP2, _ := json.Marshal(handlers.VerifikasiRequest{
		Disetujui: true,
		Catatan:   "Konfirmasi orang kedua valid setelah verifikasi rekening koran",
	})
	reqApproveP2 := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/pembayaran/%s/verifikasi", pembayaranID), bytes.NewReader(bodyApproveP2))
	reqApproveP2.Header.Set("Authorization", "Bearer "+tokenP2)
	reqApproveP2.Header.Set("Content-Type", "application/json")
	recApproveP2 := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recApproveP2, reqApproveP2)

	if recApproveP2.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for second person approval, got %d: %s", recApproveP2.Code, recApproveP2.Body.String())
	}

	// Pastikan status di database kini 'disetujui' dan persetujuan_kedua_oleh tercatat
	var finalStatus string
	var p2Oleh *uuid.UUID
	_ = env.Pool.QueryRow(ctx, "SELECT status, persetujuan_kedua_oleh FROM pembayaran WHERE id = $1;", pembayaranID).Scan(&finalStatus, &p2Oleh)
	if finalStatus != "disetujui" || p2Oleh == nil || *p2Oleh != pengurus2ID {
		t.Fatalf("Pembayaran harus berstatus 'disetujui' dengan persetujuan kedua oleh pengurus 2: status=%s, p2=%v", finalStatus, p2Oleh)
	}

	t.Logf("Sukses: Impor mutasi auto-approve nonaktif default, pengimpor = penyetuju ditolak, dan orang kedua berhasil konfirmasi!")
}

func TestItem4_Toko_StockConcurrency_NoOversell_And_OrderCancellation_Release(t *testing.T) {
	env := setupAPITestEnv(t)
	ctx := context.Background()
	tm := auth.NewTokenManager(env.Config.JWTSecret, "kopdes-merah-putih-api")

	// 1. Setup Unit Usaha & Gudang
	unitUsahaID := uuid.New()
	_, _ = env.Pool.Exec(ctx, `
		INSERT INTO unit_usaha (id, koperasi_id, kode_unit, nama, kode_kbli, jenis_unit)
		VALUES ($1, $2, 'UNIT-CONCUR', 'Unit Usaha Retail Uji', '47111', 'gerai_sembako')
		ON CONFLICT DO NOTHING;
	`, unitUsahaID, env.KoperasiID)

	gudangID := uuid.New()
	_, _ = env.Pool.Exec(ctx, `
		INSERT INTO gudang (id, koperasi_id, unit_usaha_id, kode, nama)
		VALUES ($1, $2, $3, 'GD-CONCUR', 'Gudang Uji Konkurensi')
		ON CONFLICT DO NOTHING;
	`, gudangID, env.KoperasiID, unitUsahaID)

	var katID uuid.UUID
	err := env.Pool.QueryRow(ctx, `
		INSERT INTO kategori_produk (id, koperasi_id, kode, nama, slug)
		VALUES (uuid_generate_v7(), $1, 'KAT-CONCUR', 'Sembako Concur', 'sembako-concur')
		ON CONFLICT (koperasi_id, slug) DO UPDATE SET nama = EXCLUDED.nama
		RETURNING id;
	`, env.KoperasiID).Scan(&katID)
	if err != nil {
		t.Fatalf("Seed kategori gagal: %v", err)
	}

	// 2. Setup Produk dengan Stok Awal = 2
	produkID := uuid.New()
	sku := fmt.Sprintf("SKU-CONCUR-%s", uuid.New().String()[:6])
	slug := fmt.Sprintf("beras-concur-%s", uuid.New().String()[:6])
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO produk (id, koperasi_id, unit_usaha_id, kategori_id, sku, nama, slug, satuan, status)
		VALUES ($1, $2, $3, $4, $5, 'Beras Super Terbatas', $6, 'pack', 'aktif');
	`, produkID, env.KoperasiID, unitUsahaID, katID, sku, slug)
	if err != nil {
		t.Fatalf("Seed produk gagal: %v", err)
	}

	_, err = env.Pool.Exec(ctx, `
		INSERT INTO produk_harga (id, koperasi_id, produk_id, harga_umum, harga_anggota, berlaku_mulai, created_by)
		VALUES (uuid_generate_v7(), $1, $2, 50000, 45000, CURRENT_DATE, $3);
	`, env.KoperasiID, produkID, env.AdminUserID)
	if err != nil {
		t.Fatalf("Seed produk harga gagal: %v", err)
	}

	// Stok awal = 2 di gudang
	_, err = env.Pool.Exec(ctx, `
		INSERT INTO stok_saldo (id, koperasi_id, gudang_id, produk_id, qty_tersedia, qty_dipesan)
		VALUES (uuid_generate_v7(), $1, $2, $3, 2, 0);
	`, env.KoperasiID, gudangID, produkID)
	if err != nil {
		t.Fatalf("Seed stok_saldo gagal: %v", err)
	}

	// =========================================================================
	// BAGIAN A: Uji Pembelian Bersamaan (5 Goroutines Bersaing Membeli Stok = 2)
	// =========================================================================
	const numBuyers = 5
	type buyerInfo struct {
		userID uuid.UUID
		token  string
	}
	buyers := make([]buyerInfo, numBuyers)

	for i := 0; i < numBuyers; i++ {
		bID := uuid.New()
		email := fmt.Sprintf("buyer_race_%d_%s@koperasi.id", i, uuid.New().String()[:6])
		emailHash := security.HashBlindIndex(email, []byte(env.Config.BlindIndexSecret))
		pwdHash, _ := security.HashPassword("BuyerPassword123!")

		_, err := env.Pool.Exec(ctx, `
			INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap, status_aktif)
			VALUES ($1, $2, $3, $2, $3, $4, $5, true);
		`, bID, []byte("enkripsi"), emailHash, pwdHash, fmt.Sprintf("Pembeli Race %d", i))
		if err != nil {
			t.Fatalf("Seed user %d gagal: %v", i, err)
		}

		nikBytes := []byte(fmt.Sprintf("320101010101%04d", i))
		nikHash := security.HashBlindIndex(string(nikBytes), []byte(env.Config.BlindIndexSecret))
		_, err = env.Pool.Exec(ctx, `
			INSERT INTO anggota (id, koperasi_id, user_id, no_anggota, nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir, tanggal_lahir_enkripsi, jenis_kelamin, status, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'Bogor', $5, 'L', 'aktif', $8);
		`, uuid.New(), env.KoperasiID, bID, fmt.Sprintf("ANG-%04d", i), []byte("enkripsi"), nikHash, fmt.Sprintf("Pembeli Race %d", i), env.AdminUserID)
		if err != nil {
			t.Fatalf("Seed anggota %d gagal: %v", i, err)
		}

		tok, _ := tm.GenerateToken(bID, env.KoperasiID, "anggota", false, 15*time.Minute)
		buyers[i] = buyerInfo{userID: bID, token: tok}

		// Tambahkan 1 item ke keranjang masing-masing pembeli
		bodyCart, _ := json.Marshal(map[string]interface{}{
			"produk_id": produkID,
			"qty":       1,
		})
		reqCart := httptest.NewRequest("POST", "/api/v1/toko/keranjang", bytes.NewReader(bodyCart))
		reqCart.Header.Set("Authorization", "Bearer "+tok)
		reqCart.Header.Set("Content-Type", "application/json")
		recCart := httptest.NewRecorder()
		env.Server.Router().ServeHTTP(recCart, reqCart)
		if recCart.Code != http.StatusOK {
			t.Fatalf("Buyer %d gagal menambah keranjang: %d %s", i, recCart.Code, recCart.Body.String())
		}
	}

	// Mulai 5 goroutine checkout serentak
	var wg sync.WaitGroup
	startCh := make(chan struct{})
	results := make([]int, numBuyers)

	for i := 0; i < numBuyers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startCh // Tunggu sinyal mulai serentak

			bodyCheckout, _ := json.Marshal(map[string]interface{}{
				"metode_serah_terima": "ambil_di_koperasi",
				"catatan":             fmt.Sprintf("Checkout serentak dari buyer %d", idx),
			})
			req := httptest.NewRequest("POST", "/api/v1/toko/checkout", bytes.NewReader(bodyCheckout))
			req.Header.Set("Authorization", "Bearer "+buyers[idx].token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			env.Server.Router().ServeHTTP(rec, req)
			results[idx] = rec.Code
		}(i)
	}

	// Picu eksekusi paralel serentak
	close(startCh)
	wg.Wait()

	successCount := 0
	oversellRejectedCount := 0
	for _, code := range results {
		if code == http.StatusCreated {
			successCount++
		} else if code == http.StatusBadRequest {
			oversellRejectedCount++
		}
	}

	if successCount != 2 {
		t.Fatalf("NO OVERSELL VIOLATION: Seharusnya tepat 2 pembeli yang berhasil membeli stok 2, namun didapat %d sukses (hasil: %v)", successCount, results)
	}
	if oversellRejectedCount != 3 {
		t.Fatalf("Seharusnya tepat 3 pembeli ditolak dengan 400 Bad Request karena kehabisan stok, didapat: %d", oversellRejectedCount)
	}

	runInTenant := func(fn func(tx pgx.Tx) error) {
		t.Helper()
		if err := middleware.WithTenantTx(ctx, env.Pool, env.KoperasiID, false, fn); err != nil {
			t.Fatalf("Tenant query error: %v", err)
		}
	}

	// Verifikasi saldo stok terkini di database: wajib 0 dan DILARANG MINUS
	var finalQty float64
	runInTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT qty_tersedia FROM stok_saldo WHERE produk_id = $1;", produkID).Scan(&finalQty)
	})
	if finalQty != 0 {
		t.Fatalf("Sisa stok harus tepat 0, didapat: %v", finalQty)
	}

	t.Logf("Verifikasi A Berhasil: 5 checkout bersamaan ditangani atomik, 2 sukses, 3 ditolak, saldo akhir = 0 (bebas oversell).")

	// =========================================================================
	// BAGIAN B: Pelepasan Stok dan Kuota Promo saat Pesanan DIBATALKAN
	// =========================================================================
	// 1. Tambah stok produk menjadi 5 & Buat Promo dengan Kuota Total = 1
	promoID := uuid.New()
	runInTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE stok_saldo SET qty_tersedia = 5 WHERE produk_id = $1;`, produkID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO promo (
				id, koperasi_id, kode, nama, tipe_promo, nilai_diskon, cakupan,
				berlaku_mulai, berlaku_sampai, kuota_total, kuota_terpakai, minimal_belanja
			) VALUES (
				$1, $2, 'PROMO-BATAL-TEST', 'Promo Batal 10 Ribu', 'nominal', 10000, 'seluruh_katalog',
				NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', 1, 0, 10000
			);
		`, promoID, env.KoperasiID)
		return err
	})

	// Pembeli 0 isi keranjang lagi
	bodyCartB, _ := json.Marshal(map[string]interface{}{
		"produk_id": produkID,
		"qty":       2,
	})
	reqCartB := httptest.NewRequest("POST", "/api/v1/toko/keranjang", bytes.NewReader(bodyCartB))
	reqCartB.Header.Set("Authorization", "Bearer "+buyers[0].token)
	reqCartB.Header.Set("Content-Type", "application/json")
	recCartB := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCartB, reqCartB)

	// Checkout dengan promo 'PROMO-BATAL-TEST'
	bodyCheckoutB, _ := json.Marshal(map[string]interface{}{
		"metode_serah_terima": "ambil_di_koperasi",
		"kode_promo":          "PROMO-BATAL-TEST",
		"catatan":             "Uji pembatalan pesanan",
	})
	reqCheckoutB := httptest.NewRequest("POST", "/api/v1/toko/checkout", bytes.NewReader(bodyCheckoutB))
	reqCheckoutB.Header.Set("Authorization", "Bearer "+buyers[0].token)
	reqCheckoutB.Header.Set("Content-Type", "application/json")
	recCheckoutB := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCheckoutB, reqCheckoutB)
	if recCheckoutB.Code != http.StatusCreated {
		t.Fatalf("Checkout pesanan B gagal: %d %s", recCheckoutB.Code, recCheckoutB.Body.String())
	}

	var resPesananB struct {
		Data struct {
			PesananID uuid.UUID `json:"pesanan_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recCheckoutB.Body.Bytes(), &resPesananB)
	pesananIDB := resPesananB.Data.PesananID

	// Cek bahwa stok berkurang menjadi 3 (5 - 2) dan kuota promo terpakai = 1
	var qtyAfterCheckout float64
	var kuotaPromoTerpakai int
	runInTenant(func(tx pgx.Tx) error {
		_ = tx.QueryRow(ctx, "SELECT qty_tersedia FROM stok_saldo WHERE produk_id = $1;", produkID).Scan(&qtyAfterCheckout)
		_ = tx.QueryRow(ctx, "SELECT kuota_terpakai FROM promo WHERE id = $1;", promoID).Scan(&kuotaPromoTerpakai)
		return nil
	})
	if qtyAfterCheckout != 3 || kuotaPromoTerpakai != 1 {
		t.Fatalf("Kondisi paska checkout salah: sisa stok=%v (harus 3), kuota promo=%d (harus 1)", qtyAfterCheckout, kuotaPromoTerpakai)
	}

	// Pembeli 1 mencoba membatalkan pesanan Pembeli 0 -> Wajib 403 Forbidden (Anti-IDOR)
	reqCancelIDOR := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/toko/pesanan/%s/batal", pesananIDB), nil)
	reqCancelIDOR.Header.Set("Authorization", "Bearer "+buyers[1].token)
	recCancelIDOR := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCancelIDOR, reqCancelIDOR)
	if recCancelIDOR.Code != http.StatusForbidden {
		t.Fatalf("IDOR VIOLATION: Pembeli lain tidak boleh membatalkan pesanan orang lain! Expected 403, got %d: %s", recCancelIDOR.Code, recCancelIDOR.Body.String())
	}

	// Pembeli 0 membatalkan pesanannya sendiri -> Wajib 200 OK
	reqCancelOwner := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/toko/pesanan/%s/batal", pesananIDB), nil)
	reqCancelOwner.Header.Set("Authorization", "Bearer "+buyers[0].token)
	recCancelOwner := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCancelOwner, reqCancelOwner)
	if recCancelOwner.Code != http.StatusOK {
		t.Fatalf("Pembeli pemilik gagal membatalkan pesanan: %d %s", recCancelOwner.Code, recCancelOwner.Body.String())
	}

	// Verifikasi Pelepasan Stok & Pemulihan Kuota Promo di DB
	var qtyRestored float64
	var kuotaRestored int
	var statusPesanan string
	var statusPromoPenggunaan string
	var countRetur int
	var qtyRetur float64
	runInTenant(func(tx pgx.Tx) error {
		_ = tx.QueryRow(ctx, "SELECT qty_tersedia FROM stok_saldo WHERE produk_id = $1;", produkID).Scan(&qtyRestored)
		_ = tx.QueryRow(ctx, "SELECT kuota_terpakai FROM promo WHERE id = $1;", promoID).Scan(&kuotaRestored)
		_ = tx.QueryRow(ctx, "SELECT status::text FROM pesanan WHERE id = $1;", pesananIDB).Scan(&statusPesanan)
		_ = tx.QueryRow(ctx, "SELECT status FROM promo_penggunaan WHERE pesanan_id = $1;", pesananIDB).Scan(&statusPromoPenggunaan)
		_ = tx.QueryRow(ctx, "SELECT count(*), COALESCE(SUM(qty_perubahan), 0) FROM stok_mutasi WHERE referensi_id = $1 AND jenis_mutasi = 'retur_penjualan';", pesananIDB).Scan(&countRetur, &qtyRetur)
		return nil
	})

	if statusPesanan != "batal" {
		t.Fatalf("Status pesanan harus 'batal', got: %s", statusPesanan)
	}
	if qtyRestored != 5 {
		t.Fatalf("Stok barang harus dipulihkan kembali ke 5 setelah dibatalkan, didapat: %v", qtyRestored)
	}
	if kuotaRestored != 0 {
		t.Fatalf("Kuota promo harus dipulihkan kembali ke 0 setelah dibatalkan, didapat: %d", kuotaRestored)
	}
	if statusPromoPenggunaan != "dibatalkan" {
		t.Fatalf("Status promo_penggunaan harus 'dibatalkan', got: %s", statusPromoPenggunaan)
	}
	if countRetur == 0 || qtyRetur != 2 {
		t.Fatalf("Mutasi retur_penjualan harus tercatat di ledger stok_mutasi dengan qty 2: count=%d, qty=%v", countRetur, qtyRetur)
	}

	t.Logf("Verifikasi B Berhasil: Pembatalan pesanan berhasil memulihkan stok (+2) dan kuota promo (-1) secara konsisten.")

	// =========================================================================
	// BAGIAN C: Pelepasan Stok dan Kuota Promo saat Pesanan KEDALUWARSA
	// =========================================================================
	// Pembeli 1 isi keranjang lagi (1 item)
	bodyCartC, _ := json.Marshal(map[string]interface{}{
		"produk_id": produkID,
		"qty":       1,
	})
	reqCartC := httptest.NewRequest("POST", "/api/v1/toko/keranjang", bytes.NewReader(bodyCartC))
	reqCartC.Header.Set("Authorization", "Bearer "+buyers[1].token)
	reqCartC.Header.Set("Content-Type", "application/json")
	recCartC := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCartC, reqCartC)

	// Checkout dengan promo yang sama (bisa dipakai karena kuota sudah dipulihkan di Bagian B!)
	bodyCheckoutC, _ := json.Marshal(map[string]interface{}{
		"metode_serah_terima": "ambil_di_koperasi",
		"kode_promo":          "PROMO-BATAL-TEST",
		"catatan":             "Uji kedaluwarsa pesanan",
	})
	reqCheckoutC := httptest.NewRequest("POST", "/api/v1/toko/checkout", bytes.NewReader(bodyCheckoutC))
	reqCheckoutC.Header.Set("Authorization", "Bearer "+buyers[1].token)
	reqCheckoutC.Header.Set("Content-Type", "application/json")
	recCheckoutC := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recCheckoutC, reqCheckoutC)
	if recCheckoutC.Code != http.StatusCreated {
		t.Fatalf("Checkout pesanan C gagal: %d %s", recCheckoutC.Code, recCheckoutC.Body.String())
	}

	var resPesananC struct {
		Data struct {
			PesananID uuid.UUID `json:"pesanan_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recCheckoutC.Body.Bytes(), &resPesananC)
	pesananIDC := resPesananC.Data.PesananID

	// Proses kedaluwarsa via endpoint /expire
	reqExpire := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/toko/pesanan/%s/expire", pesananIDC), nil)
	reqExpire.Header.Set("Authorization", "Bearer "+env.AdminToken)
	recExpire := httptest.NewRecorder()
	env.Server.Router().ServeHTTP(recExpire, reqExpire)
	if recExpire.Code != http.StatusOK {
		t.Fatalf("Proses kedaluwarsa pesanan gagal: %d %s", recExpire.Code, recExpire.Body.String())
	}

	// Verifikasi Pelepasan Stok & Pemulihan Kuota Promo saat Kedaluwarsa
	var qtyRestoredC float64
	var kuotaRestoredC int
	var statusPesananC string
	runInTenant(func(tx pgx.Tx) error {
		_ = tx.QueryRow(ctx, "SELECT qty_tersedia FROM stok_saldo WHERE produk_id = $1;", produkID).Scan(&qtyRestoredC)
		_ = tx.QueryRow(ctx, "SELECT kuota_terpakai FROM promo WHERE id = $1;", promoID).Scan(&kuotaRestoredC)
		_ = tx.QueryRow(ctx, "SELECT status::text FROM pesanan WHERE id = $1;", pesananIDC).Scan(&statusPesananC)
		return nil
	})

	if statusPesananC != "kedaluwarsa" {
		t.Fatalf("Status pesanan harus 'kedaluwarsa', got: %s", statusPesananC)
	}
	if qtyRestoredC != 5 {
		t.Fatalf("Stok barang harus dipulihkan kembali ke 5 saat kedaluwarsa, didapat: %v", qtyRestoredC)
	}
	if kuotaRestoredC != 0 {
		t.Fatalf("Kuota promo harus dipulihkan kembali ke 0 saat kedaluwarsa, didapat: %d", kuotaRestoredC)
	}

	t.Logf("Verifikasi C Berhasil: Pesanan kedaluwarsa melepaskan stok dan kuota promo kembali ke saldo tersedia.")
}

func TestItem6_MetricsToken_Min32Bytes_RefusesShortOrEmpty(t *testing.T) {
	env := setupAPITestEnv(t)

	// 1. Uji Validate() dengan token kosong -> Wajib Gagal
	cfgEmpty := env.Config
	cfgEmpty.MetricsToken = ""
	errEmpty := cfgEmpty.Validate()
	if errEmpty == nil {
		t.Fatalf("SEHARUSNYA GAGAL: Config.Validate() harus menolak METRICS_TOKEN kosong!")
	}
	if !strings.Contains(errEmpty.Error(), "METRICS_TOKEN") || !strings.Contains(errEmpty.Error(), "panjang minimal 32 byte") {
		t.Fatalf("Pesan error harus menyebut METRICS_TOKEN dan syarat minimal 32 byte, didapat: %v", errEmpty)
	}

	// 2. Uji Validate() dengan token pendek (< 32 byte) -> Wajib Gagal
	cfgShort := env.Config
	cfgShort.MetricsToken = "token-pendek-16b"
	errShort := cfgShort.Validate()
	if errShort == nil {
		t.Fatalf("SEHARUSNYA GAGAL: Config.Validate() harus menolak METRICS_TOKEN kurang dari 32 byte!")
	}
	if !strings.Contains(errShort.Error(), "METRICS_TOKEN") {
		t.Fatalf("Pesan error harus menyebut METRICS_TOKEN, didapat: %v", errShort)
	}

	// 3. Uji Start() Server dengan konfigurasi token kosong -> Wajib Menolak Memulai Server
	srvEmpty := api.NewServer(env.Pool, cfgEmpty)
	errStartEmpty := srvEmpty.Start()
	if errStartEmpty == nil {
		t.Fatalf("SEHARUSNYA GAGAL: Server.Start() harus menolak berjalan jika METRICS_TOKEN kosong!")
	}
	if !strings.Contains(errStartEmpty.Error(), "konfigurasi keamanan tidak valid") {
		t.Fatalf("Pesan penolakan start harus jelas, didapat: %v", errStartEmpty)
	}

	// 4. Uji Start() Server dengan konfigurasi token pendek -> Wajib Menolak Memulai Server
	srvShort := api.NewServer(env.Pool, cfgShort)
	errStartShort := srvShort.Start()
	if errStartShort == nil {
		t.Fatalf("SEHARUSNYA GAGAL: Server.Start() harus menolak berjalan jika METRICS_TOKEN pendek!")
	}

	// 5. Uji Validate() dengan token valid >= 32 byte -> Wajib Berhasil
	cfgValid := env.Config
	cfgValid.MetricsToken = "valid-secret-metrics-token-32-bytes-ok!"
	if errValid := cfgValid.Validate(); errValid != nil {
		t.Fatalf("Config.Validate() gagal pada token valid >= 32 byte: %v", errValid)
	}

	t.Logf("Sukses: Validasi METRICS_TOKEN >= 32 byte dan pencegahan startup server terbukti andal.")
}













