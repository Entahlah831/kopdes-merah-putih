package pembayaran

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	if strings.EqualFold(r.URL.Query().Get("rekening"), "true") {
		rows, err := h.svc.ListRekening(r.Context())
		if err != nil {
			response.WriteServiceError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, rows)
		return
	}
	if buktiID := strings.TrimSpace(r.URL.Query().Get("bukti_id")); buktiID != "" {
		h.serveBuktiFile(w, r, claims, buktiID)
		return
	}
	if refundBuktiID := strings.TrimSpace(r.URL.Query().Get("refund_bukti_id")); refundBuktiID != "" {
		h.serveRefundBuktiFile(w, r, claims, refundBuktiID)
		return
	}
	if idStr := strings.TrimSpace(r.URL.Query().Get("id")); idStr != "" {
		res, err := h.svc.GetDetail(r.Context(), claims, idStr)
		if err != nil {
			response.WriteServiceError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, res)
		return
	}
	lim, off := response.ParsePagination(r, 20, 100)
	rows, err := h.svc.List(r.Context(), claims, r.URL.Query().Get("user_id"), r.URL.Query().Get("status"), lim, off)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, rows, map[string]interface{}{"limit": lim, "offset": off})
}

func (h *Handler) serveBuktiFile(w http.ResponseWriter, r *http.Request, claims *middleware.SessionClaims, idStr string) {
	raw, mime, err := h.svc.ReadBukti(r.Context(), claims, idStr)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	// #nosec G705 -- byte bukti bayar disajikan dengan Content-Type tervalidasi magic bytes dan X-Content-Type-Options: nosniff
	_, _ = w.Write(raw)
}

func (h *Handler) HandlePost(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	ip := middleware.GetClientIP(r, h.svc.trusted)
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		h.handleUploadBuktiMultipart(w, r, claims, ip)
		return
	}
	var in CreatePembayaranInput
	if err := response.DecodeJSON(w, r, &in, 64<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	if strings.EqualFold(strings.TrimSpace(in.Action), "create_rekening") {
		rek, err := h.svc.CreateRekening(r.Context(), claims, in, ip)
		if err != nil {
			response.WriteServiceError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, rek)
		return
	}
	res, replayed, err := h.svc.CreatePembayaran(r.Context(), claims, in, strings.TrimSpace(r.Header.Get("Idempotency-Key")), ip)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	response.JSON(w, status, res, map[string]interface{}{"idempotent_replay": replayed})
}

func (h *Handler) handleUploadBuktiMultipart(w http.ResponseWriter, r *http.Request, claims *middleware.SessionClaims, ip string) {
	maxBytes := h.svc.cfg.UploadMaxBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1<<20))
	// #nosec G120 -- ukuran body sudah dibatasi ketat dengan http.MaxBytesReader pada baris sebelumnya
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		response.BadRequest(w, "Form upload bukti tidak valid atau melebihi batas ukuran")
		return
	}
	bayarID := strings.TrimSpace(r.FormValue("pembayaran_id"))
	catatan := strings.TrimSpace(r.FormValue("catatan_pembayar"))
	f, _, err := r.FormFile("file")
	if err != nil {
		response.BadRequest(w, "Field file bukti wajib disertakan")
		return
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		response.BadRequest(w, "Gagal membaca berkas bukti")
		return
	}
	res, err := h.svc.AttachBukti(r.Context(), claims, bayarID, raw, catatan, ip)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) HandleVerify(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	var in VerifyInput
	if err := response.DecodeJSON(w, r, &in, 32<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	res, err := h.svc.VerifyDecision(r.Context(), claims, r.PathValue("id"), in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) serveRefundBuktiFile(w http.ResponseWriter, r *http.Request, claims *middleware.SessionClaims, idStr string) {
	raw, mime, err := h.svc.ReadRefundBukti(r.Context(), claims, idStr)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	// #nosec G705 -- byte bukti refund disajikan dengan Content-Type tervalidasi magic bytes dan X-Content-Type-Options: nosniff
	_, _ = w.Write(raw)
}

func (h *Handler) HandleRefund(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	var in RefundInput
	var rawFile []byte
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		var okForm bool
		if in, rawFile, okForm = h.parseRefundMultipart(w, r); !okForm {
			return
		}
	} else if err := response.DecodeJSON(w, r, &in, 32<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	status, res, err := h.svc.ProcessRefund(r.Context(), claims, r.PathValue("id"), in, rawFile, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, status, res)
}

func (h *Handler) parseRefundMultipart(w http.ResponseWriter, r *http.Request) (RefundInput, []byte, bool) {
	var in RefundInput
	maxBytes := h.svc.cfg.UploadMaxBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1<<20))
	// #nosec G120 -- ukuran body sudah dibatasi ketat dengan http.MaxBytesReader pada baris sebelumnya
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		response.BadRequest(w, "Form refund tidak valid atau melebihi batas ukuran")
		return in, nil, false
	}
	in.Aksi = r.FormValue("aksi")
	in.Metode = r.FormValue("metode")
	in.Alasan = r.FormValue("alasan")
	in.CatatanPelaksana = r.FormValue("catatan_pelaksana")
	in.MFACode = r.FormValue("mfa_code")
	in.Breakglass = strings.EqualFold(strings.TrimSpace(r.FormValue("breakglass")), "true")
	if v := strings.TrimSpace(r.FormValue("nominal")); v != "" {
		nom, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.BadRequest(w, "nominal tidak valid")
			return in, nil, false
		}
		in.Nominal = nom
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		return in, nil, true
	}
	defer func() { _ = f.Close() }()
	rawFile, rErr := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if rErr != nil {
		response.BadRequest(w, "Gagal membaca berkas bukti refund")
		return in, nil, false
	}
	return in, rawFile, true
}
