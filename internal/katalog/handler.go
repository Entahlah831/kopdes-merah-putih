package katalog

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

func (h *Handler) HandleListProduk(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tab"))) {
	case "kategori":
		rows, err := h.svc.ListKategori(r.Context())
		if err != nil {
			response.WriteServiceError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, rows)
		return
	case "promo":
		rows, err := h.svc.ListPromoAktif(r.Context())
		if err != nil {
			response.WriteServiceError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, rows)
		return
	}
	lim, off := response.ParsePagination(r, 20, 100)
	includeNonaktif := strings.EqualFold(r.URL.Query().Get("include_nonaktif"), "true")
	rows, tier, err := h.svc.ListProduk(
		r.Context(),
		claims,
		r.URL.Query().Get("kategori_id"),
		r.URL.Query().Get("q"),
		includeNonaktif,
		lim,
		off,
	)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, rows, map[string]interface{}{"limit": lim, "offset": off, "tier_harga": tier})
}

func (h *Handler) HandleWriteProduk(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	var in WriteProdukInput
	if err := response.DecodeJSON(w, r, &in, 64<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	status, res, err := h.svc.HandleWrite(r.Context(), claims, in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, status, res)
}
