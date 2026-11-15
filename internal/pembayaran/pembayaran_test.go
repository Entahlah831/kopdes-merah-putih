package pembayaran

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
)

func TestPickRandomUniqueCode(t *testing.T) {
	code, err := pickRandomUniqueCode(3, []int32{1, 2, 3})
	if err != nil || code < 1 || code > 999 || code <= 3 {
		t.Fatalf("unexpected 3-digit code: %d (%v)", code, err)
	}
	usedAll := make([]int32, 998)
	for i := int32(1); i <= 998; i++ {
		usedAll[i-1] = i
	}
	onlyLeft, err := pickRandomUniqueCode(3, usedAll)
	if err != nil || onlyLeft != 999 {
		t.Fatalf("expected fallback 999, got %d (%v)", onlyLeft, err)
	}
}

func TestValidateProofImageAndPaymentsGuard(t *testing.T) {
	pngBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	ext, hash, err := validateProofImage(pngBytes, 1<<20)
	if err != nil || ext != ".png" || len(hash) != 64 {
		t.Fatalf("valid png failed: ext=%q len=%d err=%v", ext, len(hash), err)
	}
	if _, _, err := validateProofImage([]byte("not an image"), 1<<20); err == nil {
		t.Fatal("expected rejection of fake image")
	}
	if _, _, _, err := evaluateApprovalTx(context.Background(), nil, db.GetPembayaranForUpdateRow{}, VerifyInput{NominalDibayar: 0}); err == nil {
		t.Fatal("expected error for zero nominal_dibayar")
	}
	svc := &Service{cfg: Config{PaymentsEnabled: false}}
	_, _, err = svc.CreatePembayaran(context.Background(), &middleware.SessionClaims{UserID: uuid.New()}, CreatePembayaranInput{}, "", "127.0.0.1")
	var de *response.DomainError
	if !errors.As(err, &de) || de.Status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 PAYMENTS_DISABLED, got %v", err)
	}
}
