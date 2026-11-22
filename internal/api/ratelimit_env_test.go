package api

import (
	"testing"

	"github.com/kopdes/merah-putih/internal/config"
)

func TestRateLimitRPS_MappingAndHybridScaleGuard(t *testing.T) {
	appCfg, err := config.Load(func(k string) (string, bool) {
		switch k {
		case "APP_ENV":
			return "test", true
		case "RATE_LIMIT_RPS":
			return "1500", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("config.Load gagal: %v", err)
	}
	apiCfg := ConfigFromAppConfig(appCfg)
	if apiCfg.RateLimitRPS != 1500 {
		t.Fatalf("expected RateLimitRPS=1500, got %v", apiCfg.RateLimitRPS)
	}
	if apiCfg.RateLimitBurst != 3000 {
		t.Fatalf("expected RateLimitBurst=3000, got %d", apiCfg.RateLimitBurst)
	}
	if got := testHybridScale(apiCfg); got != 50 {
		t.Fatalf("expected testHybridScale=50 in test mode, got %d", got)
	}

	prodCfg := apiCfg
	prodCfg.AppEnv = "production"
	if got := testHybridScale(prodCfg); got != 1 {
		t.Fatalf("expected testHybridScale=1 in production mode even when RPS=1500, got %d", got)
	}
}
