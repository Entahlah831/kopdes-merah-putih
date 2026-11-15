package pendaftaran

import (
	"io"
	"net/http"
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

func (h *Handler) HandlePublicRegister(w http.ResponseWriter, r *http.Request) {
	var in RegisterInput
	if err := response.DecodeJSON(w, r, &in, 64<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	if in.CaptchaToken == "" {
		in.CaptchaToken = strings.TrimSpace(r.Header.Get("X-Captcha-Token"))
	}
	res, err := h.svc.RegisterPublic(r.Context(), in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, res)
}

func (h *Handler) HandleUploadDokumen(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.svc.store.maxBytes+(64<<10))
	if err := r.ParseMultipartForm(h.svc.store.maxBytes); err != nil {
		response.BadRequest(w, "Form multipart tidak valid atau ukuran file melebihi batas")
		return
	}
	tipe := strings.TrimSpace(r.FormValue("tipe_dokumen"))
	file, _, err := r.FormFile("file")
	if err != nil {
		response.BadRequest(w, "Field 'file' wajib disertakan")
		return
	}
	defer func() { _ = file.Close() }()

	raw, err := io.ReadAll(io.LimitReader(file, h.svc.store.maxBytes+1))
	if err != nil {
		response.BadRequest(w, "Gagal membaca data berkas KYC")
		return
	}
	claims, _ := middleware.GetSessionClaims(r.Context())
	uploadTok := strings.TrimSpace(r.Header.Get("X-Upload-Token"))
	res, err := h.svc.UploadDokumen(r.Context(), claims, r.PathValue("id"), uploadTok, tipe, raw, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, res)
}

func (h *Handler) HandleListPendaftaran(w http.ResponseWriter, r *http.Request) {
	lim, off := response.ParsePagination(r, 20, 100)
	rows, err := h.svc.ListPendaftaran(r.Context(), r.URL.Query().Get("status"), lim, off)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, rows, map[string]interface{}{"limit": lim, "offset": off})
}

func (h *Handler) HandleVerifyDecision(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	var in VerifyDecisionInput
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

func (h *Handler) HandleGetDokumen(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	plain, mime, err := h.svc.GetDokumenDecrypted(r.Context(), claims, r.PathValue("id"), r.PathValue("tipe"), middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(plain)
}
