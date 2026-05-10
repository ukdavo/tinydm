package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/auth"
)

// UserHandler serves user and API-key management endpoints.
type UserHandler struct {
	store *auth.Store
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(store *auth.Store) *UserHandler {
	return &UserHandler{store: store}
}

// ListUsers handles GET /api/v1/tenants/{tenantID}/users
//
// Supported query parameters:
//
//	limit   — page size (default 50, max 500)
//	offset  — pagination offset (default 0)
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromCtx(r)
	page := pageParams(r)

	users, total, err := h.store.ListUsers(r.Context(), tenant.ID, page.Limit, page.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if users == nil {
		users = []*auth.User{}
	}
	// Strip password hashes before returning.
	type safeUser struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
		Username string `json:"username"`
		Email    string `json:"email"`
		UserType string `json:"user_type"`
		IsActive bool   `json:"is_active"`
	}
	safe := make([]safeUser, len(users))
	for i, u := range users {
		safe[i] = safeUser{
			ID:       u.ID,
			TenantID: u.TenantID,
			Username: u.Username,
			Email:    u.Email,
			UserType: string(u.UserType),
			IsActive: u.IsActive,
		}
	}
	writePaged(w, safe, total, page.Limit, page.Offset)
}

// ListAPIKeys handles GET /api/v1/tenants/{tenantID}/apikeys
//
// Supported query parameters:
//
//	limit   — page size (default 50, max 500)
//	offset  — pagination offset (default 0)
func (h *UserHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromCtx(r)
	page := pageParams(r)

	keys, total, err := h.store.ListAPIKeys(r.Context(), tenant.ID, page.Limit, page.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if keys == nil {
		keys = []*auth.APIKey{}
	}
	// Strip key hashes; expose only safe fields.
	type safeKey struct {
		ID        string `json:"id"`
		TenantID  string `json:"tenant_id"`
		Name      string `json:"name"`
		KeyPrefix string `json:"key_prefix"`
	}
	safe := make([]safeKey, len(keys))
	for i, k := range keys {
		safe[i] = safeKey{
			ID:        k.ID,
			TenantID:  k.TenantID,
			Name:      k.Name,
			KeyPrefix: k.KeyPrefix,
		}
	}
	writePaged(w, safe, total, page.Limit, page.Offset)
}

// CreateAPIKey handles POST /api/v1/tenants/{tenantID}/apikeys
func (h *UserHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromCtx(r)
	p, _ := auth.PrincipalFromContext(r.Context())

	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	plaintext, hash, prefix, err := auth.GenerateAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key generation failed")
		return
	}

	uid := p.ID
	key, err := h.store.CreateAPIKey(r.Context(), tenant.ID, &uid, body.Name, hash, prefix, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         key.ID,
		"tenant_id":  key.TenantID,
		"name":       key.Name,
		"key_prefix": key.KeyPrefix,
		"key":        plaintext, // one-time plaintext; not stored
	})
}

// RevokeAPIKey handles POST /api/v1/tenants/{tenantID}/apikeys/{keyID}/revoke
func (h *UserHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromCtx(r)
	id := chi.URLParam(r, "keyID")

	if err := h.store.RevokeAPIKey(r.Context(), tenant.ID, id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
