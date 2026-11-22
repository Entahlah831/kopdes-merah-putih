package pengaturan

import (
	"net/http"

	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	var in UpdateInput
	if err := response.DecodeJSON(w, r, &in, 64<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	row, err := h.svc.UpdatePengaturan(r.Context(), claims, in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, row)
}

func (h *Handler) HandleListAuditLog(w http.ResponseWriter, r *http.Request) {
	lim, off := response.ParsePagination(r, 20, 100)
	rows, err := h.svc.ListAuditLog(r.Context(), r.URL.Query().Get("entitas"), r.URL.Query().Get("aksi"), lim, off)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, rows, map[string]interface{}{"limit": lim, "offset": off})
}
