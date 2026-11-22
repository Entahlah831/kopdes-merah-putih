package main

import "testing"

func TestSanitizeLogValue(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"normal text", "normal text"},
		{"line1\nFORGED_LOG", "line1FORGED_LOG"},
		{"line1\r\nFORGED_LOG\r", "line1FORGED_LOG"},
	}

	for _, tc := range cases {
		if got := sanitizeLogValue(tc.in); got != tc.want {
			t.Fatalf("sanitizeLogValue(%q)=%q, ingin %q", tc.in, got, tc.want)
		}
	}
}

func TestValidSQLIdentifier(t *testing.T) {
	valid := []string{"kopdes_app", "kopdes_role_1", "_internal"}
	for _, id := range valid {
		if !validSQLIdentifier.MatchString(id) {
			t.Fatalf("identifier valid %q ditolak", id)
		}
	}

	invalid := []string{
		"",
		"123abc",
		"kopdes-app",
		"kopdes_app; DROP TABLE users;--",
		"kopdes app",
	}
	for _, id := range invalid {
		if validSQLIdentifier.MatchString(id) {
			t.Fatalf("identifier tidak valid %q harus ditolak", id)
		}
	}
}
