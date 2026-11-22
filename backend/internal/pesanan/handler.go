package pesanan

import (
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

func (h *Handler) HandleCheckout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	var in CheckoutInput
	if err := response.DecodeJSON(w, r, &in, 64<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	res, replayed, err := h.svc.Checkout(r.Context(), claims, in, idemKey, middleware.GetClientIP(r, h.svc.trusted))
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

func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
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

func (h *Handler) HandleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	var in struct {
		Status  string `json:"status"`
		Catatan string `json:"catatan"`
	}
	if err := response.DecodeJSON(w, r, &in, 16<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	res, err := h.svc.UpdateStatus(r.Context(), claims, r.PathValue("id"), in.Status, in.Catatan, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) HandleDaftarAntar(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListDaftarAntar(r.Context(), r.URL.Query().Get("tanggal"))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	if strings.EqualFold(r.URL.Query().Get("format"), "csv") {
		raw, err := ExportDaftarAntarCSV(items)
		if err != nil {
			response.WriteServiceError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="daftar_antar.csv"`)
		_, _ = w.Write(raw)
		return
	}
	response.JSON(w, http.StatusOK, items)
}
