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
