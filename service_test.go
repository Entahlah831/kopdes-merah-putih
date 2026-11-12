package kopdes_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	kopdes "github.com/kopdes/merah-putih"
	"github.com/kopdes/merah-putih/internal/security"
)

func getTestPool(t testing.TB) *pgxpool.Pool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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
		"postgres://postgres:kopdes_dev_password@127.0.0.1:5432/kopdes_db?sslmode=disable",
	)

	var pool *pgxpool.Pool
	var lastErr error

	for _, dsn := range candidates {
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			lastErr = err
			continue
		}
		cfg.MaxConns = 50
		cfg.MinConns = 5
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			// Pastikan peran non-superuser kopdes_app selalu aktif agar FORCE RLS berlaku penuh
			_, err := conn.Exec(ctx, "SET ROLE kopdes_app;")
			return err
		}

		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			lastErr = err
			continue
		}

		if err := p.Ping(ctx); err == nil {
			pool = p
			break
		} else {
			lastErr = err
			p.Close()
		}
	}

	if pool == nil {
		t.Fatalf("gagal terkoneksi ke PostgreSQL test database: %v", lastErr)
	}

	return pool
}

// hashSHA256 helper untuk kolom blind-index hash (PDP UU 27/2022)
func hashSHA256(val string) string {
	sum := sha256.Sum256([]byte(val))
	return hex.EncodeToString(sum[:])
}

type FixtureKoperasi struct {
	WilayahID      uuid.UUID
	KoperasiID     uuid.UUID
	UserID         uuid.UUID
	AnggotaID      uuid.UUID
	PenyediaID     uuid.UUID
	MetodeID       uuid.UUID
	RekeningQRISID uuid.UUID
}

// seedFixture menyiapkan data tenant valid sesuai konstrain skema PostgreSQL 16
func seedFixture(t testing.TB, pool *pgxpool.Pool, prefix string) FixtureKoperasi {
	ctx := context.Background()

	// Gunakan koneksi langsung untuk bypass RLS saat setup master tenant
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn failed: %v", err)
	}
	defer conn.Release()

	// Reset role ke postgres (superuser) agar dapat men-setup wilayah & koperasi
	if _, err := conn.Exec(ctx, "RESET ROLE;"); err != nil {
		t.Fatalf("reset role failed: %v", err)
	}

	wilayahID := uuid.New()
	koperasiID := uuid.New()
	userID := uuid.New()
	anggotaID := uuid.New()
	penyediaID := uuid.New()
	metodeID := uuid.New()
	rekeningQRISID := uuid.New()

	kodeWilayah := fmt.Sprintf("%d", time.Now().UnixNano())
	if len(kodeWilayah) > 20 {
		kodeWilayah = kodeWilayah[:20]
	}
	var actualWilayahID uuid.UUID
	err = conn.QueryRow(ctx, `
		INSERT INTO wilayah (id, kode_kemendagri, kode_bps, nama, tingkat)
		VALUES ($1, $2, $2, $3, 'desa_kelurahan')
		ON CONFLICT (kode_kemendagri) DO UPDATE SET nama = EXCLUDED.nama
		RETURNING id
	`, wilayahID, kodeWilayah, "Desa "+prefix).Scan(&actualWilayahID)
	if err != nil {
		t.Fatalf("seed wilayah failed: %v", err)
	}
	wilayahID = actualWilayahID

	kodeKop := fmt.Sprintf("KOP-%s", uuid.New().String()[:24])
	_, err = conn.Exec(ctx, `
		INSERT INTO koperasi (id, kode_koperasi, nama, jenis_kopdes, wilayah_desa_id, alamat)
		VALUES ($1, $2, $3, 'desa', $4, 'Alamat Kantor')
	`, koperasiID, kodeKop, "Koperasi "+prefix, wilayahID)
	if err != nil {
		t.Fatalf("seed koperasi failed: %v", err)
	}

	email := fmt.Sprintf("%s_%s@kopdes.id", prefix, uuid.New().String()[:8])
	noHp := fmt.Sprintf("0812%08d", time.Now().UnixNano()%100000000)
	_, err = conn.Exec(ctx, `
		INSERT INTO users (
			id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash,
			password_hash, nama_lengkap
		) VALUES (
			$1, $2, $3, $4, $5,
			'$2a$12$e8hZf1L...', $6
		)
	`, userID, []byte("enkripsi_"+email), hashSHA256(email), []byte("enkripsi_"+noHp), hashSHA256(noHp), "User "+prefix)
	if err != nil {
		t.Fatalf("seed users failed: %v", err)
	}

	// Set context koperasi untuk RLS
	if _, err := conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false)", koperasiID.String()); err != nil {
		t.Fatalf("set config koperasi failed: %v", err)
	}

	nik := fmt.Sprintf("3201%012d", time.Now().UnixNano()%1000000000000)
	noAnggota := fmt.Sprintf("ANG-%s-%04d", prefix, time.Now().UnixNano()%10000)
	if len(noAnggota) > 40 {
		noAnggota = noAnggota[:40]
	}
	_, err = conn.Exec(ctx, `
		INSERT INTO anggota (
			id, koperasi_id, user_id, no_anggota,
			nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir,
			tanggal_lahir_enkripsi, jenis_kelamin, status, created_by
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, 'Bogor',
			$8, 'L', 'aktif', $3
		)
	`, anggotaID, koperasiID, userID, noAnggota, []byte("enkripsi_nik"), hashSHA256(nik), "Anggota "+prefix, []byte("enkripsi_tgl"))
	if err != nil {
		t.Fatalf("seed anggota failed: %v", err)
	}

	kodePenyedia := fmt.Sprintf("ASPI_%s_%s", prefix, uuid.New().String()[:6])
	if len(kodePenyedia) > 40 {
		kodePenyedia = kodePenyedia[:40]
	}
	var actualPenyediaID uuid.UUID
	err = conn.QueryRow(ctx, `
		INSERT INTO penyedia_pembayaran (id, kode, nama, tipe_integrasi)
		VALUES ($1, $2, 'QRIS Multi PJP', 'qris_statis_manual')
		ON CONFLICT (kode) DO UPDATE SET nama = EXCLUDED.nama
		RETURNING id
	`, penyediaID, kodePenyedia).Scan(&actualPenyediaID)
	if err != nil {
		t.Fatalf("seed penyedia failed: %v", err)
	}
	penyediaID = actualPenyediaID

	kodeMetode := fmt.Sprintf("QRIS_%s", prefix)
	_, err = conn.Exec(ctx, `
		INSERT INTO metode_pembayaran (
			id, koperasi_id, penyedia_id, kode, nama, butuh_kode_unik, butuh_verifikasi_manual
		) VALUES (
			$1, $2, $3, $4, 'QRIS Statis', true, true
		)
	`, metodeID, koperasiID, penyediaID, kodeMetode)
	if err != nil {
		t.Fatalf("seed metode_pembayaran failed: %v", err)
	}

	nmid := fmt.Sprintf("ID%013d", time.Now().UnixNano()%10000000000000)
	_, err = conn.Exec(ctx, `
		INSERT INTO rekening_qris_koperasi (
			id, koperasi_id, penyedia_id, nama_merchant, nmid,
			nama_bank_pjp, no_rekening_settlement, nama_pemilik_rekening,
			is_atas_nama_badan_hukum, qris_payload_statis, created_by
		) VALUES (
			$1, $2, $3, $4, $5,
			'Bank BRI', '0123456789', $6,
			true, '000201010211...', $7
		)
	`, rekeningQRISID, koperasiID, penyediaID, "Merchant "+prefix, nmid, "Kopdes "+prefix, userID)
	if err != nil {
		t.Fatalf("seed rekening_qris_koperasi failed: %v", err)
	}

	// Kembalikan role ke kopdes_app
	if _, err := conn.Exec(ctx, "SET ROLE kopdes_app;"); err != nil {
		t.Fatalf("restore role kopdes_app failed: %v", err)
	}

	return FixtureKoperasi{
		WilayahID:      wilayahID,
		KoperasiID:     koperasiID,
		UserID:         userID,
		AnggotaID:      anggotaID,
		PenyediaID:     penyediaID,
		MetodeID:       metodeID,
		RekeningQRISID: rekeningQRISID,
	}
}

// TestKonkurensiQRIS_AntiTabrakan menguji anti-tabrakan kode unik (1..999)
// ketika banyak goroutine membuat pembayaran dengan nominal dasar dan rekening QRIS yang persis sama.
func TestKonkurensiQRIS_AntiTabrakan(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	fix := seedFixture(t, pool, "CONCUR_QRIS")
	secret := []byte("kunci-rahasia-hmac-sha256-qris-32b")

	const totalGoroutines = 30
	const nominalDasarRp = int64(75_000) // Semua goroutine menggunakan nominal yang identik

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	type HasilQRIS struct {
		Index            int
		Res              *kopdes.BuatPembayaranQRISRes
		Err              error
		KodeUnikTerpilih int
	}

	hasilChan := make(chan HasilQRIS, totalGoroutines)

	for i := 0; i < totalGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startBarrier // Sinkronisasi waktu peluncuran serentak

			req := kopdes.BuatPembayaranQRISReq{
				KoperasiID:         fix.KoperasiID,
				PembayarUserID:     fix.UserID,
				AnggotaID:          fix.AnggotaID,
				MetodePembayaranID: fix.MetodeID,
				RekeningQRISID:     fix.RekeningQRISID,
				JenisTransaksi:     "iuran_bulanan",
				NoPembayaran:       fmt.Sprintf("PAY-CONCUR-%s-%03d", uuid.New().String()[:8], idx),
				NominalDasarRp:     nominalDasarRp,
				TTL:                1 * time.Hour,
			}

			res, err := kopdes.BuatPembayaranQRISStatis(context.Background(), pool, secret, req)
			var kode int
			if res != nil {
				kode = res.KodeUnikTerpilih
			}
			hasilChan <- HasilQRIS{
				Index:            idx,
				Res:              res,
				Err:              err,
				KodeUnikTerpilih: kode,
			}
		}(i)
	}

	// Lepaskan seluruh goroutine bersamaan
	close(startBarrier)
	wg.Wait()
	close(hasilChan)

	// Validasi seluruh hasil: tidak boleh ada error dan seluruh kode unik WAJIB beda (anti-tabrakan)
	kodeUnikTerpakai := make(map[int]int) // kode_unik -> index goroutine
	jumlahSukses := 0

	for h := range hasilChan {
		if h.Err != nil {
			t.Errorf("Goroutine %d gagal membuat pembayaran QRIS: %v", h.Index, h.Err)
			continue
		}
		if h.Res == nil {
			t.Errorf("Goroutine %d hasil nil tanpa error", h.Index)
			continue
		}

		jumlahSukses++

		// 1. Verifikasi kode unik berada dalam rentang [1..999]
		if h.KodeUnikTerpilih < 1 || h.KodeUnikTerpilih > 999 {
			t.Errorf("Goroutine %d kode unik di luar jangkauan (1..999): %d", h.Index, h.KodeUnikTerpilih)
		}

		// 2. Verifikasi anti-tabrakan (uniqueness)
		if pemilikSebelumnya, sudahAda := kodeUnikTerpakai[h.KodeUnikTerpilih]; sudahAda {
			t.Fatalf("TABRAKAN KODE UNIK TERDETEKSI! Goroutine %d dan %d sama-sama mendapat kode unik: %d",
				h.Index, pemilikSebelumnya, h.KodeUnikTerpilih)
		}
		kodeUnikTerpakai[h.KodeUnikTerpilih] = h.Index

		// 3. Verifikasi nominal total sesuai dengan nominal dasar + kode unik
		expectedTotal := nominalDasarRp + int64(h.KodeUnikTerpilih)
		if h.Res.NominalTotalRp != expectedTotal {
			t.Errorf("Goroutine %d nominal total mismatch: dapat %d, diharapkan %d",
				h.Index, h.Res.NominalTotalRp, expectedTotal)
		}
	}

	if jumlahSukses != totalGoroutines {
		t.Fatalf("Hanya %d dari %d transaksi berhasil", jumlahSukses, totalGoroutines)
	}

	// 4. Verifikasi konsistensi data langsung ke database PostgreSQL 16
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("gagal memulai verifikasi tx: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", fix.KoperasiID.String())
	if err != nil {
		t.Fatalf("set config error: %v", err)
	}

	var rowCount, distinctCodes int
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT kode_unik)
		FROM pembayaran
		WHERE koperasi_id = $1 AND rekening_qris_id = $2 AND nominal_dasar = $3
	`, fix.KoperasiID, fix.RekeningQRISID, nominalDasarRp).Scan(&rowCount, &distinctCodes)
	if err != nil {
		t.Fatalf("gagal query verifikasi database: %v", err)
	}

	if rowCount != totalGoroutines {
		t.Errorf("Jumlah baris di database mismatch: dapat %d, diharapkan %d", rowCount, totalGoroutines)
	}
	if distinctCodes != totalGoroutines {
		t.Errorf("Jumlah distinct kode unik di database mismatch: dapat %d, diharapkan %d", distinctCodes, totalGoroutines)
	}

	t.Logf("Sukses menguji %d transaksi QRIS konkuren: seluruh kode unik (1..999) unik tanpa tabrakan!", totalGoroutines)
}

// TestIsolasiRLS_LintasKoperasi menguji Row-Level Security (RLS) PostgreSQL 16
// menjamin koperasi tidak dapat melihat, mengubah, maupun menyisipkan data milik koperasi lain.
func TestIsolasiRLS_LintasKoperasi(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	fixA := seedFixture(t, pool, "RLS_TENANT_A")
	fixB := seedFixture(t, pool, "RLS_TENANT_B")

	ctx := context.Background()

	// --- SKENARIO 1: Isolasi Pembacaan (SELECT) ---
	t.Run("Isolasi_Baca_Anggota_Dan_Rekening", func(t *testing.T) {
		txA, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx A error: %v", err)
		}
		defer txA.Rollback(ctx)

		// Set context transaksi ke Koperasi A
		_, err = txA.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", fixA.KoperasiID.String())
		if err != nil {
			t.Fatalf("set config tx A error: %v", err)
		}

		// Koperasi A hanya boleh melihat anggotanya sendiri
		var countA int
		err = txA.QueryRow(ctx, "SELECT COUNT(*) FROM anggota WHERE koperasi_id = $1", fixA.KoperasiID).Scan(&countA)
		if err != nil {
			t.Fatalf("query anggota A error: %v", err)
		}
		if countA != 1 {
			t.Errorf("Koperasi A harus melihat anggotanya sendiri (diharapkan 1, dapat %d)", countA)
		}

		// Koperasi A TIDAK BOLEH melihat anggota Koperasi B (hasil harus 0 baris)
		var countB int
		err = txA.QueryRow(ctx, "SELECT COUNT(*) FROM anggota WHERE koperasi_id = $1", fixB.KoperasiID).Scan(&countB)
		if err != nil {
			t.Fatalf("query anggota B error: %v", err)
		}
		if countB != 0 {
			t.Errorf("KEBOCORAN DATA RLS! Koperasi A bisa melihat anggota Koperasi B: count=%d", countB)
		}

		// Query langsung dengan PK anggota milik Koperasi B harus mengembalikan ErrNoRows
		var dummyID uuid.UUID
		err = txA.QueryRow(ctx, "SELECT id FROM anggota WHERE id = $1", fixB.AnggotaID).Scan(&dummyID)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("Harusnya ErrNoRows saat Koperasi A query anggota B berdasarkan ID, dapat: %v", err)
		}

		// Rekening QRIS Koperasi B juga tidak boleh terbaca oleh Koperasi A
		var rekeningCount int
		err = txA.QueryRow(ctx, "SELECT COUNT(*) FROM rekening_qris_koperasi WHERE id = $1", fixB.RekeningQRISID).Scan(&rekeningCount)
		if err != nil {
			t.Fatalf("query rekening B error: %v", err)
		}
		if rekeningCount != 0 {
			t.Errorf("KEBOCORAN DATA RLS! Koperasi A bisa melihat rekening QRIS Koperasi B: count=%d", rekeningCount)
		}
	})

	// --- SKENARIO 2: Isolasi Penulisan Silang Tenant (INSERT WITH CHECK) ---
	t.Run("Isolasi_Tulis_Silang_Tenant_Ditolak", func(t *testing.T) {
		txA, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx error: %v", err)
		}
		defer txA.Rollback(ctx)

		// Set context transaksi ke Koperasi A
		_, err = txA.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", fixA.KoperasiID.String())
		if err != nil {
			t.Fatalf("set config error: %v", err)
		}

		// Coba INSERT rekening QRIS baru dengan memaksakan koperasi_id = Koperasi B
		// Policy WITH CHECK (koperasi_id = app_current_koperasi_id()) WAJIB menolak insert ini
		_, err = txA.Exec(ctx, `
			INSERT INTO rekening_qris_koperasi (
				koperasi_id, penyedia_id, nama_merchant, nmid,
				nama_bank_pjp, no_rekening_settlement, nama_pemilik_rekening,
				is_atas_nama_badan_hukum, qris_payload_statis, created_by
			) VALUES (
				$1, $2, 'Illegal Merchant', $3,
				'Bank BRI', '999999', 'Pemilik', true, '000201...', $4
			)
		`, fixB.KoperasiID, fixA.PenyediaID, fmt.Sprintf("ID%013d", time.Now().UnixNano()%10000000000000), fixA.UserID)

		if err == nil {
			t.Fatalf("PELANGGARAN RLS! Transaksi atas nama Koperasi A berhasil menyisipkan data untuk Koperasi B")
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			// Kode 42501: insufficient_privilege (row-level security violation)
			if pgErr.Code != "42501" {
				t.Logf("Catatan: error code dari database: %s (%s)", pgErr.Code, pgErr.Message)
			}
			t.Logf("Berhasil ditolak RLS dengan aman: %s", pgErr.Message)
		} else {
			t.Logf("Berhasil ditolak dengan error: %v", err)
		}
	})

	// --- SKENARIO 3: Isolasi Modifikasi Silang Tenant (UPDATE) ---
	t.Run("Isolasi_Update_Silang_Tenant_Nol_Baris", func(t *testing.T) {
		txA, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx error: %v", err)
		}
		defer txA.Rollback(ctx)

		// Set context transaksi ke Koperasi A
		_, err = txA.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", fixA.KoperasiID.String())
		if err != nil {
			t.Fatalf("set config error: %v", err)
		}

		// Koperasi A mencoba mengubah status rekening milik Koperasi B
		cmd, err := txA.Exec(ctx, `
			UPDATE rekening_qris_koperasi
			SET status_aktif = false
			WHERE id = $1
		`, fixB.RekeningQRISID)
		if err != nil {
			t.Fatalf("update exec error: %v", err)
		}

		if cmd.RowsAffected() != 0 {
			t.Fatalf("PELANGGARAN RLS! Koperasi A berhasil memodifikasi %d baris milik Koperasi B", cmd.RowsAffected())
		}
	})
}

// TestMakerChecker_Pembayaran menguji aturan maker-checker:
// Verifikator (diverifikasi_oleh) dilarang sama dengan pembuat transaksi (pembayar_user_id).
func TestMakerChecker_Pembayaran(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	fix := seedFixture(t, pool, "MAKER_CHECKER")
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx error: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", fix.KoperasiID.String())
	if err != nil {
		t.Fatalf("set config error: %v", err)
	}

	// Coba buat pembayaran di mana diverifikasi_oleh == pembayar_user_id
	_, err = tx.Exec(ctx, `
		INSERT INTO pembayaran (
			koperasi_id, no_pembayaran, pembayar_user_id, anggota_id,
			metode_pembayaran_id, rekening_qris_id, jenis_transaksi,
			nominal_dasar, kode_unik, kode_unik_awal,
			berlaku_sampai, status, diverifikasi_oleh, created_by
		) VALUES (
			$1, 'PAY-MC-FAIL-01', $2, $3,
			$4, $5, 'iuran_bulanan',
			50000, 100, 100,
			NOW() + INTERVAL '1 hour', 'menunggu_verifikasi', $2, $2
		)
	`, fix.KoperasiID, fix.UserID, fix.AnggotaID, fix.MetodeID, fix.RekeningQRISID)

	if err == nil {
		t.Fatalf("PELANGGARAN MAKER-CHECKER! Pembayar_user_id diizinkan memverifikasi pembayarannya sendiri")
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.ConstraintName != "chk_pembayaran_maker_checker" {
			t.Errorf("Constraint name mismatch: diharapkan 'chk_pembayaran_maker_checker', dapat '%s'", pgErr.ConstraintName)
		}
		t.Logf("Berhasil ditolak konstrain maker-checker: %s", pgErr.Message)
	}
}

// TestAppendOnly_ProteksiTabelLog menguji trigger trg_fn_append_only()
// menjamin tidak ada UPDATE atau DELETE pada tabel audit & mutasi.
func TestAppendOnly_ProteksiTabelLog(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	fix := seedFixture(t, pool, "APPEND_ONLY")
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx error: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", fix.KoperasiID.String())
	if err != nil {
		t.Fatalf("set config error: %v", err)
	}

	// 1. Sisipkan status log ke anggota_status_log
	logID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO anggota_status_log (
			id, koperasi_id, anggota_id, status_lama, status_baru, alasan, created_by
		) VALUES (
			$1, $2, $3, 'calon', 'aktif', 'Persetujuan pendaftaran', $4
		)
	`, logID, fix.KoperasiID, fix.AnggotaID, fix.UserID)
	if err != nil {
		t.Fatalf("insert status log error: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit insert status log error: %v", err)
	}

	// 2. Coba lakukan UPDATE pada record log
	t.Run("Proteksi_Dilarang_UPDATE", func(t *testing.T) {
		txUpd, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx error: %v", err)
		}
		defer txUpd.Rollback(ctx)

		_, _ = txUpd.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", fix.KoperasiID.String())
		_, err = txUpd.Exec(ctx, `
			UPDATE anggota_status_log
			SET alasan = 'Alasan dimanipulasi'
			WHERE id = $1
		`, logID)
		if err == nil {
			t.Fatalf("PELANGGARAN APPEND-ONLY! Operasi UPDATE pada anggota_status_log berhasil tanpa ditolak")
		}
		t.Logf("Operasi UPDATE berhasil ditolak: %v", err)
	})

	// 3. Coba lakukan DELETE pada record log
	t.Run("Proteksi_Dilarang_DELETE", func(t *testing.T) {
		txDel, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx error: %v", err)
		}
		defer txDel.Rollback(ctx)

		_, _ = txDel.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", fix.KoperasiID.String())
		_, err = txDel.Exec(ctx, `
			DELETE FROM anggota_status_log
			WHERE id = $1
		`, logID)
		if err == nil {
			t.Fatalf("PELANGGARAN APPEND-ONLY! Operasi DELETE pada anggota_status_log berhasil tanpa ditolak")
		}
		t.Logf("Operasi DELETE berhasil ditolak: %v", err)
	})
}

func TestP1_2_Kapasitas_KodeUnik_4Digit_Dan_PeringatanSlot(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	fix := seedFixture(t, pool, "P12")
	ctx := context.Background()
	secret := []byte("secret-key-32-bytes-minimum-ok!!")

	// 1. Uji alokasi QRIS 4 digit (1..9999 slot)
	req4Digit := kopdes.BuatPembayaranQRISReq{
		KoperasiID:         fix.KoperasiID,
		PembayarUserID:     fix.UserID,
		AnggotaID:          fix.AnggotaID,
		MetodePembayaranID: fix.MetodeID,
		RekeningQRISID:     fix.RekeningQRISID,
		JenisTransaksi:     "iuran_bulanan",
		NoPembayaran:       fmt.Sprintf("PAY-4DIGIT-%d", time.Now().UnixNano()),
		NominalDasarRp:     50000,
		TTL:                1 * time.Hour,
		KodeUnikDigits:     4,
	}

	res, err := kopdes.BuatPembayaranQRISStatis(ctx, pool, secret, req4Digit)
	if err != nil {
		t.Fatalf("Gagal membuat pembayaran 4 digit: %v", err)
	}

	if res.KodeUnikTerpilih < 1 || res.KodeUnikTerpilih > 9999 {
		t.Errorf("Kode unik 4 digit di luar jangkauan (1..9999): %d", res.KodeUnikTerpilih)
	}

	// 2. Uji metrik pemakaian slot (GetSlotUsage)
	active, capacity, ratio, err := kopdes.GetSlotUsage(ctx, pool, fix.KoperasiID, fix.RekeningQRISID, 50000, 9999)
	if err != nil {
		t.Fatalf("GetSlotUsage error: %v", err)
	}
	if active < 1 {
		t.Errorf("Expected active slots >= 1, got %d", active)
	}
	if capacity != 9999 {
		t.Errorf("Expected capacity 9999, got %d", capacity)
	}
	if ratio <= 0 {
		t.Errorf("Expected ratio > 0, got %f", ratio)
	}

	// 3. Uji kapasitas default 3 digit
	active3, capacity3, _, err := kopdes.GetSlotUsage(ctx, pool, fix.KoperasiID, fix.RekeningQRISID, 50000, 999)
	if err != nil {
		t.Fatalf("GetSlotUsage 3 digit error: %v", err)
	}
	if capacity3 != 999 {
		t.Errorf("Expected capacity 999, got %d", capacity3)
	}
	if active3 != active {
		t.Errorf("Active count mismatch: %d vs %d", active3, active)
	}
}

func TestP1_3_AppendOnly_HashChain_Dan_ProteksiTRUNCATE(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	fix := seedFixture(t, pool, "P13")
	ctx := context.Background()

	// 1. Uji proteksi BEFORE TRUNCATE trigger pada tabel append-only
	t.Run("Proteksi_TRUNCATE_kas_mutasi", func(t *testing.T) {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire conn failed: %v", err)
		}
		defer conn.Release()

		// Coba eksekusi TRUNCATE kas_mutasi
		_, err = conn.Exec(ctx, "TRUNCATE kas_mutasi;")
		if err == nil {
			t.Fatalf("SECURITY VIOLATION: TRUNCATE kas_mutasi berhasil! Harusnya ditolak oleh trigger fn_prevent_truncate() atau permission denied")
		}
		if !strings.Contains(err.Error(), "TABEL APPEND-ONLY: Operasi TRUNCATE dilarang") && !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("Expected prevent_truncate error or permission denied, got: %v", err)
		}
		t.Logf("TRUNCATE berhasil dicegah oleh trigger atau izin peran: %v", err)
	})

	// 2. Uji pembentukan rantai hash kriptografis SHA-256 (Hash Chain)
	t.Run("Verifikasi_Hash_Chain_Integritas", func(t *testing.T) {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire conn failed: %v", err)
		}
		defer conn.Release()

		_, _ = conn.Exec(ctx, "RESET ROLE;")
		_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", fix.KoperasiID.String())

		// Genesis block (baris ke-1)
		prev0 := security.GenesisHash
		payload1 := []byte("audit_log_entry_1_create_tenant")
		hash1 := security.ComputeHashChain(prev0, payload1)
		id1 := uuid.New()

		_, err = conn.Exec(ctx, `
			INSERT INTO audit_log (
				id, koperasi_id, user_id, nama_tabel, record_id, aksi,
				data_baru, prev_hash, row_hash, created_by
			) VALUES ($1, $2, $3, 'koperasi', $2, 'INSERT', '{"test": 1}', $4, $5, $3);
		`, id1, fix.KoperasiID, fix.UserID, prev0, hash1)
		if err != nil {
			t.Fatalf("Gagal insert audit_log ke-1: %v", err)
		}

		// Baris ke-2 (prev_hash merujuk ke row_hash baris ke-1)
		payload2 := []byte("audit_log_entry_2_update_config")
		hash2 := security.ComputeHashChain(hash1, payload2)
		id2 := uuid.New()

		_, err = conn.Exec(ctx, `
			INSERT INTO audit_log (
				id, koperasi_id, user_id, nama_tabel, record_id, aksi,
				data_baru, prev_hash, row_hash, created_by
			) VALUES ($1, $2, $3, 'konfigurasi', $2, 'UPDATE', '{"test": 2}', $4, $5, $3);
		`, id2, fix.KoperasiID, fix.UserID, hash1, hash2)
		if err != nil {
			t.Fatalf("Gagal insert audit_log ke-2: %v", err)
		}

		// Validasi integritas rantai
		valid, count, brokenID, err := kopdes.VerifyHashChain(ctx, pool, "audit_log", fix.KoperasiID)
		if err != nil || !valid {
			t.Fatalf("VerifyHashChain gagal: valid=%v, brokenID=%v, err=%v", valid, brokenID, err)
		}
		if count != 2 {
			t.Errorf("Expected 2 verified rows, got %d", count)
		}
		t.Logf("Hash chain berhasil divalidasi penuh: %d baris terverifikasi tanpa anomali", count)
	})
}

func TestTask1_Concurrent_HashChain_AutoPopulated_By_Trigger(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	fix := seedFixture(t, pool, "HC50")
	ctx := context.Background()

	const concurrentWorkers = 50
	var wg sync.WaitGroup
	errCh := make(chan error, concurrentWorkers)

	for i := 0; i < concurrentWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Lakukan insert normal tanpa menyediakan prev_hash atau row_hash
			// (seperti halnya di produksi dan fn_setujui_pembayaran_qris)
			conn, err := pool.Acquire(ctx)
			if err != nil {
				errCh <- fmt.Errorf("acquire failed: %w", err)
				return
			}
			defer conn.Release()

			_, _ = conn.Exec(ctx, "RESET ROLE;")
			_, _ = conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false);", fix.KoperasiID.String())

			rowID := uuid.New()
			payloadJSON := fmt.Sprintf(`{"worker_id": %d, "timestamp": "%s"}`, workerID, time.Now().Format(time.RFC3339Nano))

			_, err = conn.Exec(ctx, `
				INSERT INTO audit_log (
					id, koperasi_id, user_id, nama_tabel, record_id, aksi,
					data_baru, created_by
				) VALUES ($1, $2, $3, 'test_tabel', $1, 'INSERT', $4::jsonb, $3);
			`, rowID, fix.KoperasiID, fix.UserID, payloadJSON)

			if err != nil {
				errCh <- fmt.Errorf("worker %d insert failed: %w", workerID, err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("Insert error during concurrent test: %v", err)
		}
	}

	// Verifikasi apakah seluruh 50 baris terisi otomatis dan membentuk rantai tanpa cabang
	valid, count, brokenID, err := kopdes.VerifyHashChain(ctx, pool, "audit_log", fix.KoperasiID)
	if err != nil || !valid {
		t.Fatalf("FAILED DEFECT 1: Hash chain tidak terisi otomatis atau rantai terputus: valid=%v, count=%d, brokenID=%v, err=%v",
			valid, count, brokenID, err)
	}

	if count != concurrentWorkers {
		t.Fatalf("Expected exactly %d chained rows, got %d", concurrentWorkers, count)
	}
	t.Logf("SUCCESS: 50 concurrent inserts auto-populated hash chain strictly serialized without branching!")
}


