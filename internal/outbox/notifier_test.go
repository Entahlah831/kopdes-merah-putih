package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/outbox"
)

type MockGateway struct {
	FailWhatsApp bool
	FailSMS      bool
	WACalls      int
	SMSCalls     int
	LastWAPhone  string
	LastWAMsg    string
	LastSMSPhone string
	LastSMSMsg   string
}

func (m *MockGateway) SendWhatsApp(ctx context.Context, phone, message string) error {
	m.WACalls++
	m.LastWAPhone = phone
	m.LastWAMsg = message
	if m.FailWhatsApp {
		return errors.New("whatsapp api connection timeout")
	}
	return nil
}

func (m *MockGateway) SendSMS(ctx context.Context, phone, message string) error {
	m.SMSCalls++
	m.LastSMSPhone = phone
	m.LastSMSMsg = message
	if m.FailSMS {
		return errors.New("sms provider balance exhausted")
	}
	return nil
}

func TestWhatsAppSMSNotifier_DirectChannelsAndFallback(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.New()
	targetPhone := "+6281234567890"

	// 1. Direct WhatsApp Success
	gw := &MockGateway{}
	notifier := outbox.NewWhatsAppSMSNotifier(gw)

	payloadWA := `{"channel":"whatsapp","phone":"+6281234567890","message":"Kode OTP Anda: 123456"}`
	err := notifier.SendNotification(ctx, tenantID, targetPhone, "OTP", payloadWA)
	if err != nil {
		t.Fatalf("Send WhatsApp direct failed: %v", err)
	}
	if gw.WACalls != 1 || gw.SMSCalls != 0 {
		t.Fatalf("Expected 1 WA call, 0 SMS calls, got WA=%d, SMS=%d", gw.WACalls, gw.SMSCalls)
	}

	// 2. Direct SMS Success
	gw = &MockGateway{}
	notifier = outbox.NewWhatsAppSMSNotifier(gw)

	payloadSMS := `{"channel":"sms","phone":"+6281234567890","message":"Tagihan iuran Anda Rp 50.000 jatuh tempo"}`
	err = notifier.SendNotification(ctx, tenantID, targetPhone, "Tagihan", payloadSMS)
	if err != nil {
		t.Fatalf("Send SMS direct failed: %v", err)
	}
	if gw.SMSCalls != 1 || gw.WACalls != 0 {
		t.Fatalf("Expected 1 SMS call, 0 WA calls, got WA=%d, SMS=%d", gw.WACalls, gw.SMSCalls)
	}

	// 3. Fallback Auto: WhatsApp Gagal -> Otomatis Fallback ke SMS Sukses
	gw = &MockGateway{FailWhatsApp: true}
	notifier = outbox.NewWhatsAppSMSNotifier(gw)

	payloadAuto := `{"channel":"auto","phone":"+6281234567890","message":"Pemberitahuan pencairan SHU"}`
	err = notifier.SendNotification(ctx, tenantID, targetPhone, "SHU", payloadAuto)
	if err != nil {
		t.Fatalf("Fallback WA -> SMS harus berhasil, got error: %v", err)
	}
	if gw.WACalls != 1 || gw.SMSCalls != 1 {
		t.Fatalf("Fallback harus mencoba WA 1x lalu SMS 1x, got WA=%d, SMS=%d", gw.WACalls, gw.SMSCalls)
	}
	if gw.LastSMSPhone != targetPhone || !strings.Contains(gw.LastSMSMsg, "SHU") {
		t.Fatalf("Payload SMS fallback tidak cocok: phone=%s, msg=%s", gw.LastSMSPhone, gw.LastSMSMsg)
	}

	// 4. Keduanya Gagal -> Kembalikan Compound Error
	gw = &MockGateway{FailWhatsApp: true, FailSMS: true}
	notifier = outbox.NewWhatsAppSMSNotifier(gw)

	err = notifier.SendNotification(ctx, tenantID, targetPhone, "Penting", payloadAuto)
	if err == nil {
		t.Fatalf("Harus gagal jika WA dan SMS keduanya gagal!")
	}
	if !strings.Contains(err.Error(), "WhatsApp gagal") || !strings.Contains(err.Error(), "fallback SMS juga gagal") {
		t.Fatalf("Pesan error harus merinci kegagalan kedua jalur, got: %v", err)
	}

	// 5. Validasi Redaksi Nomor HP (UU PDP)
	redacted := outbox.RedactPhoneNumber("+6281234567890")
	if redacted != "+62****890" {
		t.Fatalf("Redaksi nomor HP salah, expected '+62****890', got '%s'", redacted)
	}

	t.Logf("Sukses: Notifier WhatsApp/SMS direct, fallback, error handling, dan redaksi nomor terbukti andal.")
}

func TestWhatsAppSMSNotifier_IntegratedWithOutboxWorker(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// 1. Setup Koperasi Uji
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer conn.Release()

	_, _ = conn.Exec(ctx, "RESET ROLE;")

	wilayahID := uuid.New()
	koperasiID := uuid.New()
	kodeWil := fmt.Sprintf("%d", time.Now().UnixNano())[:18]
	_, _ = conn.Exec(ctx, `
		INSERT INTO wilayah (id, kode_kemendagri, kode_bps, nama, tingkat)
		VALUES ($1, $2, $2, 'Desa Notifikasi Outbox', 'desa_kelurahan')
		ON CONFLICT DO NOTHING;
	`, wilayahID, kodeWil)

	kodeKop := fmt.Sprintf("KOP-NOTIF-%s", uuid.New().String()[:12])
	_, err = conn.Exec(ctx, `
		INSERT INTO koperasi (id, kode_koperasi, nama, jenis_kopdes, wilayah_desa_id, alamat)
		VALUES ($1, $2, 'Koperasi Notif Test', 'desa', $3, 'Jl. Notif No. 1');
	`, koperasiID, kodeKop, wilayahID)
	if err != nil {
		t.Fatalf("Seed koperasi failed: %v", err)
	}

	// 2. Enqueue Notifikasi WhatsApp ke outbox_event dalam transaksi bisnis
	gw := &MockGateway{FailWhatsApp: false}
	notifier := outbox.NewWhatsAppSMSNotifier(gw)
	worker := outbox.NewWorker(pool, notifier)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx failed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true);", koperasiID.String())
	if err != nil {
		t.Fatalf("Set tenant context failed: %v", err)
	}

	targetPhone := "+6289988776655"
	targetMsg := "Selamat! Pembayaran QRIS Anda Rp 150.000 telah disetujui."
	eventID, err := outbox.EnqueueNotification(ctx, tx, koperasiID, outbox.ChannelWhatsApp, targetPhone, "Pembayaran QRIS", targetMsg)
	if err != nil {
		t.Fatalf("EnqueueNotification failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit tx failed: %v", err)
	}

	// 3. Jalankan ProcessBatch Outbox Worker
	processed, failed, err := worker.ProcessBatch(ctx, 10)
	if err != nil {
		t.Fatalf("ProcessBatch failed: %v", err)
	}
	if processed == 0 {
		t.Fatalf("Worker harus memproses minimal 1 event notifikasi, processed=%d, failed=%d", processed, failed)
	}

	// 4. Verifikasi bahwa gateway WhatsApp menerima pesan yang tepat
	if gw.WACalls != 1 || gw.LastWAPhone != targetPhone || gw.LastWAMsg != targetMsg {
		t.Fatalf("Gateway WhatsApp tidak menerima payload yang sesuai: calls=%d, phone=%s, msg=%s", gw.WACalls, gw.LastWAPhone, gw.LastWAMsg)
	}

	// 5. Verifikasi di DB bahwa status event kini 'selesai'
	var status string
	var diprosesPada *time.Time
	txVerify, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin txVerify failed: %v", err)
	}
	defer txVerify.Rollback(ctx)
	_, _ = txVerify.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true);", koperasiID.String())

	err = txVerify.QueryRow(ctx, "SELECT status, diproses_pada FROM outbox_event WHERE id = $1;", eventID).Scan(&status, &diprosesPada)
	if err != nil {
		t.Fatalf("Query outbox_event status failed: %v", err)
	}
	if status != "selesai" || diprosesPada == nil {
		t.Fatalf("Status outbox_event harus 'selesai' dengan diproses_pada terisi, got status=%s, diproses_pada=%v", status, diprosesPada)
	}

	t.Logf("Sukses: Enqueue notifikasi outbox dan dispatching via worker & WhatsApp gateway 100%% terverifikasi!")
}
