package api

import (
	"testing"

	"github.com/kopdes/merah-putih/internal/config"
)

func TestRateLimitRPS_ConfigMappingAndDefaults(t *testing.T) {
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
	if apiCfg.RateLimitRPS != 1500 || apiCfg.RateLimitBurst != 3000 {
		t.Fatalf("expected 1500/3000, got %v/%d", apiCfg.RateLimitRPS, apiCfg.RateLimitBurst)
	}

	defCfg, err := config.Load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("config.Load default gagal: %v", err)
	}
	defAPI := ConfigFromAppConfig(defCfg)
	if defAPI.RateLimitRPS != 30 || defAPI.RateLimitBurst != 60 {
		t.Fatalf("expected default 30/60, got %v/%d", defAPI.RateLimitRPS, defAPI.RateLimitBurst)
	}

	rps, burst := resolveRateLimits(config.AppConfig{}, DefaultConfig())
	if rps != 30 || burst != 60 {
		t.Fatalf("expected zero-value fallback 30/60, got %v/%d", rps, burst)
	}
}
