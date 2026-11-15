package laporan

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/kopdes/merah-putih/internal/api/response"
)

func TestDateRangeReportTypeAndCSV(t *testing.T) {
	d1, d2, err := parseOptionalDateRange("2026-01-01", "2026-12-31")
	if err != nil || !d1.Valid || !d2.Valid {
		t.Fatalf("valid date range failed: %v", err)
	}
	if _, _, err := parseOptionalDateRange("2026/01/01", ""); err == nil {
		t.Fatal("expected error on bad dari format")
	}

	svc := NewService(nil, "")
	_, _, _, err = svc.GetReport(context.Background(), "tidak_ada", "", "")
	var de *response.DomainError
	if !errors.As(err, &de) || de.Status != http.StatusBadRequest {
		t.Fatalf("expected 400 INVALID_REPORT_TYPE, got %v", err)
	}

	raw, err := RenderCSV([]string{"kolom1", "kolom2"}, [][]string{{"a", "b"}, {"1", "2"}})
	if err != nil || !strings.Contains(string(raw), "kolom1,kolom2") {
		t.Fatalf("RenderCSV unexpected output: %q (%v)", string(raw), err)
	}
}
