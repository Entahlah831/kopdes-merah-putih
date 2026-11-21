package auth

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

func (h *Handler) resolveCookieSecure(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(h.svc.cfg.CookieSecure)) {
	case "true":
		return true
	case "false":
		return false
	default:
		return middleware.RequestScheme(r, h.svc.trusted) == "https"
	}
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAgeSecs int) {
	isSecure := h.resolveCookieSecure(r)
	name := middleware.SessionCookiePlainName
	if isSecure {
		name = middleware.SessionCookieHostName
	}
	// #nosec G124 -- Secure dikontrol oleh COOKIE_SECURE dan proxy terpercaya (X-Forwarded-Proto HTTPS memakai prefix __Host-)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecure,
		MaxAge:   maxAgeSecs,
	})
}

func clearSessionCookies(w http.ResponseWriter) {
	for _, item := range []struct {
		name   string
		secure bool
	}{
		{middleware.SessionCookieHostName, true},
		{middleware.SessionCookiePlainName, false},
	} {
		// #nosec G124 -- membersihkan kedua variasi cookie sesi (__Host- HTTPS dan HTTP dev)
		http.SetCookie(w, &http.Cookie{
			Name:     item.name,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   item.secure,
			MaxAge:   -1,
		})
	}
}

func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var in LoginInput
	if err := response.DecodeJSON(w, r, &in, 64<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	in.IP = middleware.GetClientIP(r, h.svc.trusted)
	in.UserAgent = r.UserAgent()
	res, err := h.svc.Login(r.Context(), in)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	h.setSessionCookie(w, r, res.SessionToken, int(h.svc.cfg.SessionAbsTTL.Seconds()))
	if !h.svc.cfg.AllowBearerAuth {
		res.SessionToken = ""
	}
	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) HandleMFASetup(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Sesi tidak ditemukan")
		return
	}
	var in MFASetupInput
	if r.ContentLength != 0 {
		if err := response.DecodeJSON(w, r, &in, 32<<10); err != nil {
			response.WriteServiceError(w, err)
			return
		}
	}
	res, err := h.svc.SetupMFA(r.Context(), claims, in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) HandleMFAVerify(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Sesi tidak ditemukan")
		return
	}
	var in MFAVerifyInput
	if err := response.DecodeJSON(w, r, &in, 32<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	res, err := h.svc.VerifyMFA(r.Context(), claims, in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Sesi tidak ditemukan")
		return
	}
	if err := h.svc.Logout(r.Context(), claims, middleware.GetClientIP(r, h.svc.trusted)); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	clearSessionCookies(w)
	response.JSON(w, http.StatusOK, map[string]bool{"logged_out": true})
}

func (h *Handler) HandlePassword(w http.ResponseWriter, r *http.Request) {
	var in PasswordInput
	if err := response.DecodeJSON(w, r, &in, 32<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	claims, _ := middleware.GetSessionClaims(r.Context())
	if err := h.svc.ChangeOrActivatePassword(r.Context(), claims, in, middleware.GetClientIP(r, h.svc.trusted)); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"password_updated": true})
}

func (h *Handler) HandleMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetSessionClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Sesi tidak ditemukan")
		return
	}
	res, err := h.svc.Me(r.Context(), claims)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	lim, off := response.ParsePagination(r, 20, 100)
	rows, err := h.svc.ListUsers(r.Context(), lim, off)
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, rows, map[string]interface{}{"limit": lim, "offset": off})
}

func (h *Handler) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	var in CreateUserInput
	if err := response.DecodeJSON(w, r, &in, 64<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	res, err := h.svc.CreateUser(r.Context(), claims, in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, res)
}

func (h *Handler) HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.GetSessionClaims(r.Context())
	var in UpdateUserInput
	if err := response.DecodeJSON(w, r, &in, 64<<10); err != nil {
		response.WriteServiceError(w, err)
		return
	}
	res, err := h.svc.UpdateUser(r.Context(), claims, r.PathValue("id"), in, middleware.GetClientIP(r, h.svc.trusted))
	if err != nil {
		response.WriteServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}
