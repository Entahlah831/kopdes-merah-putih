package main

import (
	"testing"

	"github.com/kopdes/merah-putih/internal/config"
)

func TestSeedTestAccounts_RefusesNonTestEnvironmentOrDatabase(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.AppConfig
	}{
		{
			name: "ProductionEnv",
			cfg:  config.AppConfig{AppEnv: "production", DBName: "kopdes_test"},
		},
		{
			name: "DevelopmentEnv",
			cfg:  config.AppConfig{AppEnv: "development", DBName: "kopdes_test"},
		},
		{
			name: "NonTestDatabase",
			cfg:  config.AppConfig{AppEnv: "test", DBName: "kopdes_prod"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSeedGuard(tc.cfg); err == nil {
				t.Fatalf("Expected validateSeedGuard to reject %s (%+v), got nil", tc.name, tc.cfg)
			}
		})
	}

	okCfg := config.AppConfig{AppEnv: "test", DBName: "kopdes_test"}
	if err := validateSeedGuard(okCfg); err != nil {
		t.Fatalf("Expected test env + *_test DB to be accepted, got: %v", err)
	}
}
