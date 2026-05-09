package api

import (
	"net/http"

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
