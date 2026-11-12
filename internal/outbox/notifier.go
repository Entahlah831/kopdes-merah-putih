package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ChannelTipe mendefinisikan kanal pengiriman notifikasi
type ChannelTipe string

const (
	ChannelWhatsApp ChannelTipe = "whatsapp"
	ChannelSMS      ChannelTipe = "sms"
	ChannelAuto     ChannelTipe = "auto" // Mencoba WhatsApp dahulu, fallback ke SMS bila gagal
)

// NotificationPayload merepresentasikan isi pesan terstruktur dalam outbox_event
type NotificationPayload struct {
	Channel  ChannelTipe            `json:"channel"`
	Phone    string                 `json:"phone"`
	Subject  string                 `json:"subject"`
	Message  string                 `json:"message"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// GatewayProvider mendefinisikan antarmuka vendor komunikasi eksternal (WA / SMS)
type GatewayProvider interface {
	SendWhatsApp(ctx context.Context, phone, message string) error
	SendSMS(ctx context.Context, phone, message string) error
}

// WhatsAppSMSNotifier mengimplementasikan Notifier dengan dukungan fallback cerdas WhatsApp -> SMS
type WhatsAppSMSNotifier struct {
	gateway GatewayProvider
}

// NewWhatsAppSMSNotifier membuat instance notifier berbasis WhatsApp dan SMS
func NewWhatsAppSMSNotifier(gateway GatewayProvider) *WhatsAppSMSNotifier {
	return &WhatsAppSMSNotifier{gateway: gateway}
}

// SendNotification mengurai payload pesan dan mengeksekusi pengiriman melalui gateway
func (n *WhatsAppSMSNotifier) SendNotification(ctx context.Context, koperasiID uuid.UUID, recipient, subject, body string) error {
	var payload NotificationPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		// Fallback jika body berupa string pesan langsung
		payload = NotificationPayload{
			Channel: ChannelAuto,
			Phone:   recipient,
			Subject: subject,
			Message: body,
		}
	}

	phone := strings.TrimSpace(payload.Phone)
	if phone == "" {
		phone = strings.TrimSpace(recipient)
	}
	if phone == "" {
		return errors.New("nomor telepon penerima wajib diisi")
	}

	redactedPhone := RedactPhoneNumber(phone)

	if n.gateway == nil {
		log.Printf("[OUTBOX-NOTIFIER-STUB] Mengirim notifikasi ke %s via %s: %s", redactedPhone, payload.Channel, payload.Message)
		return nil
	}

	switch payload.Channel {
	case ChannelWhatsApp:
		return n.gateway.SendWhatsApp(ctx, phone, payload.Message)
	case ChannelSMS:
		return n.gateway.SendSMS(ctx, phone, payload.Message)
	case ChannelAuto, "":
		// 1. Coba kirim via WhatsApp
		waErr := n.gateway.SendWhatsApp(ctx, phone, payload.Message)
		if waErr == nil {
			return nil
		}

		// 2. Jika WhatsApp gagal, lakukan fallback ke SMS
		log.Printf("[OUTBOX-NOTIFIER] Pengiriman WhatsApp ke %s gagal (%v), melakukan fallback ke SMS...", redactedPhone, waErr)
		smsErr := n.gateway.SendSMS(ctx, phone, payload.Message)
		if smsErr != nil {
			return fmt.Errorf("pengiriman WhatsApp gagal (%v) dan fallback SMS juga gagal: %w", waErr, smsErr)
		}
		return nil
	default:
		return fmt.Errorf("kanal notifikasi '%s' tidak didukung", payload.Channel)
	}
}

// EnqueueNotification mendaftarkan event notifikasi ke tabel outbox_event dalam transaksi bisnis (atomik)
func EnqueueNotification(ctx context.Context, tx pgx.Tx, koperasiID uuid.UUID, channel ChannelTipe, phone, subject, message string) (uuid.UUID, error) {
	eventID := uuid.New()
	payload := NotificationPayload{
		Channel: channel,
		Phone:   phone,
		Subject: subject,
		Message: message,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, err
	}

	topik := fmt.Sprintf("notifikasi.%s", channel)
	if channel == ChannelAuto || channel == "" {
		topik = "notifikasi.wa_sms"
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_event (
			id, koperasi_id, topik, agregat_tipe, agregat_id,
			payload, status, jumlah_percobaan, tersedia_pada, created_at
		) VALUES (
			$1, $2, $3, 'notifikasi', $1,
			$4, 'menunggu', 0, NOW(), clock_timestamp()
		);
	`, eventID, koperasiID, topik, payloadBytes)
	if err != nil {
		return uuid.Nil, fmt.Errorf("gagal enqueue notifikasi outbox: %w", err)
	}

	return eventID, nil
}

// RedactPhoneNumber menyamarkan nomor telepon demi kepatuhan perlindungan data pribadi (UU PDP)
func RedactPhoneNumber(phone string) string {
	phone = strings.TrimSpace(phone)
	if len(phone) <= 6 {
		return "***"
	}
	return phone[:3] + "****" + phone[len(phone)-3:]
}
