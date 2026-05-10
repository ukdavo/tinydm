package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/cluster"
	"tinydm/internal/repo"
)

// TagHandler handles tag CRUD for documents.
type TagHandler struct {
	store  *repo.Store
	locker cluster.Locker
}

// NewTagHandler creates a new TagHandler.
func NewTagHandler(store *repo.Store, locker cluster.Locker) *TagHandler {
	return &TagHandler{store: store, locker: locker}
}

// List handles GET .../documents/{documentID}/tags
// Returns the sorted list of tags as a JSON array.
func (h *TagHandler) List(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	tags, err := h.store.ListDocumentTags(r.Context(), doc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, http.StatusOK, tags)
}

// Replace handles PUT .../documents/{documentID}/tags
// Accepts {"tags": ["a", "b"]} and atomically replaces all tags.
func (h *TagHandler) Replace(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)

	unlock, err := h.locker.Lock(r.Context(), "doc:"+doc.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "could not acquire lock")
		return
	}
	defer unlock()

	var body struct {
		Tags []string `json:"tags"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Normalise: trim whitespace, drop empty strings.
	clean := make([]string, 0, len(body.Tags))
	for _, t := range body.Tags {
		t = strings.TrimSpace(t)
		if t != "" {
			clean = append(clean, t)
		}
	}
	if err := h.store.SetDocumentTags(r.Context(), doc.ID, clean); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tags, err := h.store.ListDocumentTags(r.Context(), doc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, http.StatusOK, tags)
}

// Add handles POST .../documents/{documentID}/tags/{tag}
// Adds a single tag (idempotent).
func (h *TagHandler) Add(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	tag := strings.TrimSpace(chi.URLParam(r, "tag"))
	if tag == "" {
		writeError(w, http.StatusBadRequest, "tag must not be empty")
		return
	}
	if err := h.store.AddDocumentTag(r.Context(), doc.ID, tag); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tags, err := h.store.ListDocumentTags(r.Context(), doc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

// Remove handles DELETE .../documents/{documentID}/tags/{tag}
func (h *TagHandler) Remove(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	tag := chi.URLParam(r, "tag")
	if err := h.store.RemoveDocumentTag(r.Context(), doc.ID, tag); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
