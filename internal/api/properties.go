package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/repo"
)

// PropertyHandler handles custom property CRUD for documents.
// Keys prefixed with "sys." are reserved for system metadata and cannot be
// modified through this handler.
type PropertyHandler struct {
	store *repo.Store
}

// NewPropertyHandler creates a new PropertyHandler.
func NewPropertyHandler(store *repo.Store) *PropertyHandler {
	return &PropertyHandler{store: store}
}

// List handles GET .../documents/{documentID}/properties
// Returns all properties (system + custom) as a JSON object.
func (h *PropertyHandler) List(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	props, err := h.store.GetDocumentProperties(r.Context(), doc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, props)
}

// Replace handles PUT .../documents/{documentID}/properties
// Accepts a JSON object and atomically replaces all user-defined properties.
// Keys prefixed with "sys." are ignored.
func (h *PropertyHandler) Replace(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	var body map[string]string
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Strip reserved keys.
	filtered := make(map[string]string, len(body))
	for k, v := range body {
		if !strings.HasPrefix(k, "sys.") {
			filtered[k] = v
		}
	}
	if err := h.store.SetDocumentProperties(r.Context(), doc.ID, filtered); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	props, err := h.store.GetDocumentProperties(r.Context(), doc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, props)
}

// Set handles PUT .../documents/{documentID}/properties/{key}
// Upserts a single key/value pair. The "sys." namespace is reserved.
func (h *PropertyHandler) Set(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	key := chi.URLParam(r, "key")
	if strings.HasPrefix(key, "sys.") {
		writeError(w, http.StatusForbidden, `property keys prefixed with "sys." are reserved`)
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.store.SetDocumentProperty(r.Context(), doc.ID, key, body.Value); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{key: body.Value})
}

// Delete handles DELETE .../documents/{documentID}/properties/{key}
func (h *PropertyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	key := chi.URLParam(r, "key")
	if strings.HasPrefix(key, "sys.") {
		writeError(w, http.StatusForbidden, `property keys prefixed with "sys." are reserved`)
		return
	}
	if err := h.store.DeleteDocumentProperty(r.Context(), doc.ID, key); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
