package testseed

import (
	"strings"
	"testing"

	"github.com/kopdes/merah-putih/internal/config"
)

func TestAccountsAndResolvePassword(t *testing.T) {
	accs := Accounts()
	if len(accs) != 5 {
		t.Fatalf("jumlah akun seeded=%d, ingin 5", len(accs))
	}
	cfg := config.AppConfig{AESMasterKey: strings.Repeat("a", 32)}
	pwd, err := ResolveSeedPassword(cfg, "")
	if err != nil || len(pwd) < 12 {
		t.Fatalf("ResolveSeedPassword gagal: pwd=%q err=%v", pwd, err)
	}
}
