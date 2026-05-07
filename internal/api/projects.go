package api

import (
	"errors"
	"net/http"

	"tinydm/internal/auth"
	"tinydm/internal/repo"
)

// ProjectHandler handles all project-scoped HTTP requests.
type ProjectHandler struct {
	store     *repo.Store
	authStore *auth.Store
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(store *repo.Store, authStore *auth.Store) *ProjectHandler {
	return &ProjectHandler{store: store, authStore: authStore}
}

// List handles GET /api/v1/tenants/{tenantID}/projects
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromCtx(r)
	projects, err := h.store.ListProjects(r.Context(), tenant.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if projects == nil {
		projects = []*repo.Project{}
	}
	writeJSON(w, http.StatusOK, projects)
}

// Create handles POST /api/v1/tenants/{tenantID}/projects
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	project, err := h.store.CreateProject(r.Context(), tenant.ID, body.Name, body.Description)
	if err != nil {
		var conflict *repo.ErrConflict
		if errors.As(err, &conflict) {
			writeError(w, http.StatusConflict, conflict.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

// Get handles GET /api/v1/tenants/{tenantID}/projects/{projectID}
func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, projectFromCtx(r))
}

// Update handles PUT /api/v1/tenants/{tenantID}/projects/{projectID}
func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	project := projectFromCtx(r)
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" {
		body.Name = project.Name
	}

	updated, err := h.store.UpdateProject(r.Context(), project.ID, body.Name, body.Description)
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

// Delete handles DELETE /api/v1/tenants/{tenantID}/projects/{projectID}
func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	project := projectFromCtx(r)
	if err := h.store.DeleteProject(r.Context(), project.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
