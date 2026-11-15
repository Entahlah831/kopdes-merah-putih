package laporan

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kopdes/merah-putih/internal/api/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	jenis := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("jenis")))
	if jenis == "" {
		jenis = "anggota"
	}
	data, hdr, rows, err := h.svc.GetReport(
		r.Context(),
		jenis,
		r.URL.Query().Get("dari"),
		r.URL.Query().Get("sampai"),
	)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	if strings.EqualFold(r.URL.Query().Get("format"), "csv") {
		raw, err := RenderCSV(hdr, rows)
		if err != nil {
			response.WriteServiceError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="laporan_%s.csv"`, jenis))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// #nosec G705 -- output CSV non-HTML disajikan sebagai attachment dengan X-Content-Type-Options: nosniff
		_, _ = w.Write(raw)
		return
	}
	response.JSON(w, http.StatusOK, data, map[string]interface{}{"jenis": jenis})
}
