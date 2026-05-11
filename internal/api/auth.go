package api

import (
	"net/http"

	"tinydm/internal/auth"
	"tinydm/internal/config"
)

// AuthHandler holds dependencies for authentication endpoints.
type AuthHandler struct {
	cfg   *config.Config
	store *auth.Store
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(cfg *config.Config, store *auth.Store) *AuthHandler {
	return &AuthHandler{cfg: cfg, store: store}
}

// ─── POST /api/v1/auth/login ──────────────────────────────────────────────────

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	UserType string `json:"user_type"`
}

// Login handles POST /api/v1/auth/login.
// Accepts JSON credentials and returns a signed JWT on success.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := h.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Use the same error message for "not found" and "wrong password" to
	// avoid leaking whether an account exists.
	if user == nil || !user.IsActive {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := auth.CheckPassword(user.PasswordHash, req.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.NewJWT(
		h.cfg.JWTSecret,
		h.cfg.JWTExpiryMinutes,
		user.ID,
		user.Username,
		user.UserType,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token:    token,
		UserID:   user.ID,
		Username: user.Username,
		UserType: string(user.UserType),
	})
}

// ─── GET /api/v1/auth/me ──────────────────────────────────────────────────────

type meResponse struct {
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	UserType   string `json:"user_type"`
	AuthMethod string `json:"auth_method"`
}

// Me handles GET /api/v1/auth/me — returns the current principal's details.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		UserID:     p.ID,
		Username:   p.Username,
		UserType:   string(p.UserType),
		AuthMethod: string(p.AuthMethod),
	})
}
