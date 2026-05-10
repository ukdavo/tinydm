package api

import (
	"errors"
	"net/http"

	"tinydm/internal/auth"
	"tinydm/internal/repo"
)

// TenantHandler handles all tenant-scoped HTTP requests.
type TenantHandler struct {
	store     *repo.Store
	authStore *auth.Store
}

// NewTenantHandler creates a new TenantHandler.
func NewTenantHandler(store *repo.Store, authStore *auth.Store) *TenantHandler {
	return &TenantHandler{store: store, authStore: authStore}
}

// List handles GET /api/v1/tenants
func (h *TenantHandler) List(w http.ResponseWriter, r *http.Request) {
	page := pageParams(r)
	tenants, total, err := h.store.ListTenants(r.Context(), page)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if tenants == nil {
		tenants = []*repo.Tenant{}
	}
	writePaged(w, tenants, total, page.Limit, page.Offset)
}

// createTenantResponse is returned by POST /api/v1/tenants. It includes the
// newly created tenant plus one-time credentials for the auto-provisioned
// domain admin. The plaintext password is not stored — the caller must record
// it immediately.
type createTenantResponse struct {
	*repo.Tenant
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"` // one-time plaintext; never re-retrievable
}

// Create handles POST /api/v1/tenants
func (h *TenantHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	tenant, err := h.store.CreateTenant(r.Context(), body.Name, body.Description)
	if err != nil {
		var conflict *repo.ErrConflict
		if errors.As(err, &conflict) {
			writeError(w, http.StatusConflict, conflict.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Automatically provision a domain admin for the new tenant.
	// The plaintext password is generated here and returned once — it is never
	// persisted in plain text, so the caller must record it now.
	adminUser, adminPassword, err := h.authStore.CreateDomainAdmin(r.Context(), tenant.ID)
	if err != nil {
		// Tenant was created; log but don't fail — the caller can create an
		// admin user manually via the users API if this step fails.
		writeError(w, http.StatusInternalServerError, "tenant created but failed to provision domain admin")
		return
	}

	writeJSON(w, http.StatusCreated, createTenantResponse{
		Tenant:        tenant,
		AdminUsername: adminUser.Username,
		AdminPassword: adminPassword,
	})
}

// Get handles GET /api/v1/tenants/{tenantID}
func (h *TenantHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, tenantFromCtx(r))
}

// Update handles PUT /api/v1/tenants/{tenantID}
func (h *TenantHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromCtx(r)
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" {
		body.Name = tenant.Name
	}

	updated, err := h.store.UpdateTenant(r.Context(), tenant.ID, body.Name, body.Description)
	if err != nil {
		var conflict *repo.ErrConflict
		if errors.As(err, &conflict) {
			writeError(w, http.StatusConflict, conflict.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// Delete handles DELETE /api/v1/tenants/{tenantID}
func (h *TenantHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromCtx(r)
	if err := h.store.DeleteTenant(r.Context(), tenant.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
