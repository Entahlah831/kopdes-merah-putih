package anggota

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

func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	if strings.EqualFold(r.URL.Query().Get("format"), "csv") {
		csvBytes, err := h.svc.ExportCSV(r.Context(), statusFilter)
		if err != nil {
			response.WriteServiceError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="anggota.csv"`)
		_, _ = w.Write(csvBytes)
		return
	}
	lim, off := response.ParsePagination(r, 20, 100)
	rows, err := h.svc.List(r.Context(), statusFilter, lim, off)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, rows, map[string]interface{}{"limit": lim, "offset": off})
}

func (h *Handler) HandleGetByID(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	unmask := strings.EqualFold(r.URL.Query().Get("unmask"), "true")
	res, err := h.svc.GetDetail(r.Context(), claims, r.PathValue("id"), unmask, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) HandleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	var in UpdateStatusInput
	if err := response.DecodeJSON(w, r, &in, 32<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	res, err := h.svc.UpdateStatus(r.Context(), claims, r.PathValue("id"), in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) HandleImportCSV(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	dryRun := strings.EqualFold(r.URL.Query().Get("dry_run"), "true")
	var csvText string
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			response.BadRequest(w, "Form multipart CSV tidak valid")
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			response.BadRequest(w, "Field file CSV wajib disertakan")
			return
		}
		defer func() { _ = f.Close() }()
		raw, err := io.ReadAll(io.LimitReader(f, 2<<20))
		if err != nil {
			response.BadRequest(w, "Gagal membaca berkas CSV")
			return
		}
		csvText = string(raw)
	} else {
		var body struct {
			CSV string `json:"csv"`
		}
		if err := response.DecodeJSON(w, r, &body, 2<<20); err != nil {
			response.WriteServiceError(w, err)
			return
		}
		csvText = body.CSV
	}
	res, err := h.svc.ImportCSV(r.Context(), claims, csvText, dryRun, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}
