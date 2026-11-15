package main

import (
	"context"
	"strings"
	"testing"

	"github.com/kopdes/merah-putih/internal/config"
)

func TestDispatchCommand_UnknownAndValidation(t *testing.T) {
	ctx := context.Background()
	cfg := config.AppConfig{}

	if err := dispatchCommand(ctx, nil, cfg, []string{"unknown", "cmd"}); err == nil {
		t.Fatal("expected error for unknown command")
	}
	err := dispatchCommand(ctx, nil, cfg, []string{"admin", "create", "--email", "a@b.c", "--password", "short"})
	if err == nil || !strings.Contains(err.Error(), "10") {
		t.Fatalf("expected weak password validation error, got: %v", err)
	}
	err = dispatchCommand(ctx, nil, cfg, []string{"iuran", "generate", "--periode", "invalid"})
	if err == nil {
		t.Fatal("expected invalid periode error")
	}
}

func TestValidRoleID(t *testing.T) {
	if !validRoleID.MatchString(config.DefaultDBAppRole) {
		t.Fatalf("default DB role %q invalid", config.DefaultDBAppRole)
	}
	for _, bad := range []string{"", "1bad", "kopdes-app; DROP TABLE", "a b"} {
		if validRoleID.MatchString(bad) {
			t.Fatalf("expected %q to fail role regex", bad)
		}
	}
}
