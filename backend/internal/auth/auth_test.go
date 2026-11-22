package auth

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeIdentifier(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"  Admin@Kopdes.LOCAL ", "admin@kopdes.local"},
		{"+62 812-3456-7890", "081234567890"},
		{"628110000001", "08110000001"},
	}
	for _, tc := range cases {
		if got := normalizeIdentifier(tc.in); got != tc.want {
			t.Fatalf("normalizeIdentifier(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPasswordPolicyAndRoleMFA(t *testing.T) {
	if err := validatePasswordPolicy("pendek9"); err == nil {
		t.Fatal("expected short password error")
	}
	if err := validatePasswordPolicy("SandiKuat123!"); err != nil {
		t.Fatalf("unexpected error for valid password: %v", err)
	}
	if err := validatePasswordPolicy(strings.Repeat("a", 72)); err != nil {
		t.Fatalf("unexpected error for 72-byte password: %v", err)
	}
	if err := validatePasswordPolicy(strings.Repeat("a", 73)); err == nil {
		t.Fatal("expected >72-byte password to be rejected before bcrypt")
	}
	svc := NewService(nil, Config{
		BcryptCost:       4,
		MFARequiredRoles: []string{"admin", "bendahara"},
		SessionIdleTTL:   15 * time.Minute,
	}, nil)
	if !svc.roleRequiresMFA("ADMIN") || !svc.roleRequiresMFA("bendahara") {
		t.Fatal("expected admin and bendahara to require MFA")
	}
	if svc.roleRequiresMFA("anggota") {
		t.Fatal("anggota should not require MFA")
	}
	if got := truncateStr("abcdef", 3); got != "abc" {
		t.Fatalf("truncateStr = %q, want abc", got)
	}
}
