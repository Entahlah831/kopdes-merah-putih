package testseed

import (
	"strings"
	"testing"
	"time"

	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
)

func TestAccountsAndResources(t *testing.T) {
	accs := Accounts()
	if len(accs) != 11 {
		t.Fatalf("jumlah akun seeded=%d, ingin 11", len(accs))
	}

	seenKeys := map[string]bool{}
	for _, a := range accs {
		if seenKeys[a.Key] {
			t.Fatalf("duplikat key akun: %s", a.Key)
		}
		seenKeys[a.Key] = true
		if a.Email == "" || a.Role == "" {
			t.Fatalf("akun %s memiliki field wajib kosong", a.Key)
		}
	}

	res := Resources()
	if res.ProdukAID == "" || res.AnggotaAID == "" || res.AnggotaBID == "" || res.PembayaranBID == "" {
		t.Fatalf("resource deterministic tidak boleh kosong: %+v", res)
	}
}

func TestDeterministicTOTPSecretAndResolvePassword(t *testing.T) {
	cfg := config.AppConfig{
		AESMasterKey: strings.Repeat("a", 32),
		JWTSecret:    "jwt-secret-uji-minimal-32-byte-kopdes",
	}

	sec1 := DeterministicTOTPSecret(cfg, "admin_a@kopdes-test.local")
	sec2 := DeterministicTOTPSecret(cfg, "ADMIN_A@kopdes-test.local")
	secOther := DeterministicTOTPSecret(cfg, "ketua_a@kopdes-test.local")

	if sec1 == "" || sec1 != sec2 {
		t.Fatalf("secret TOTP harus deterministik dan case-insensitive: %q vs %q", sec1, sec2)
	}
	if sec1 == secOther {
		t.Fatalf("secret untuk email berbeda harus unik")
	}

	code, err := security.GenerateTOTPCode(sec1, time.Now().UTC())
	if err != nil || len(code) != 6 {
		t.Fatalf("gagal menghasilkan kode TOTP 6 digit: code=%q err=%v", code, err)
	}

	pwd, err := ResolveSeedPassword(cfg, "")
	if err != nil || len(pwd) < 12 {
		t.Fatalf("ResolveSeedPassword gagal: pwd=%q err=%v", pwd, err)
	}
}
