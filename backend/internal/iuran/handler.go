package iuran

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

func (h *Handler) HandleCreateJenis(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	var in CreateJenisInput
	if err := response.DecodeJSON(w, r, &in, 32<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	res, err := h.svc.CreateJenisOrGenerate(r.Context(), claims, in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, res)
}

func (h *Handler) HandleListTagihan(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	scope := r.URL.Query().Get("scope")
	if strings.EqualFold(scope, "jenis") || strings.EqualFold(r.URL.Query().Get("jenis"), "true") {
		jenis, err := h.svc.ListJenisAktif(r.Context())
		if err != nil {
			response.WriteServiceError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, jenis)
		return
	}
	lim, off := response.ParsePagination(r, 20, 100)
	rows, err := h.svc.ListTagihan(r.Context(), claims, scope, r.URL.Query().Get("status"), lim, off)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, rows, map[string]interface{}{"limit": lim, "offset": off})
}

func (h *Handler) HandleKeringanan(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	var in KeringananInput
	if err := response.DecodeJSON(w, r, &in, 32<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	res, err := h.svc.ApplyKeringanan(r.Context(), claims, r.PathValue("id"), in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}
