package api

import (
	"errors"
	"net/http"

	"tinydm/internal/auth"
	"tinydm/internal/repo"
)

// BucketHandler handles all bucket-scoped HTTP requests.
type BucketHandler struct {
	store     *repo.Store
	authStore *auth.Store
}

// NewBucketHandler creates a new BucketHandler.
func NewBucketHandler(store *repo.Store, authStore *auth.Store) *BucketHandler {
	return &BucketHandler{store: store, authStore: authStore}
}

// List handles GET /api/v1/tenants/{tenantID}/projects/{projectID}/buckets
func (h *BucketHandler) List(w http.ResponseWriter, r *http.Request) {
	project := projectFromCtx(r)
	buckets, err := h.store.ListBuckets(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if buckets == nil {
		buckets = []*repo.Bucket{}
	}
	writeJSON(w, http.StatusOK, buckets)
}

// Create handles POST /api/v1/tenants/{tenantID}/projects/{projectID}/buckets
func (h *BucketHandler) Create(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	bucket, err := h.store.CreateBucket(r.Context(), project.ID, body.Name, body.Description)
	if err != nil {
		var conflict *repo.ErrConflict
		if errors.As(err, &conflict) {
			writeError(w, http.StatusConflict, conflict.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, bucket)
}

// Get handles GET /api/v1/tenants/{tenantID}/projects/{projectID}/buckets/{bucketID}
func (h *BucketHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, bucketFromCtx(r))
}

// Update handles PUT /api/v1/tenants/{tenantID}/projects/{projectID}/buckets/{bucketID}
func (h *BucketHandler) Update(w http.ResponseWriter, r *http.Request) {
	bucket := bucketFromCtx(r)
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" {
		body.Name = bucket.Name
	}

	updated, err := h.store.UpdateBucket(r.Context(), bucket.ID, body.Name, body.Description)
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

// Delete handles DELETE /api/v1/tenants/{tenantID}/projects/{projectID}/buckets/{bucketID}
func (h *BucketHandler) Delete(w http.ResponseWriter, r *http.Request) {
	bucket := bucketFromCtx(r)
	if err := h.store.DeleteBucket(r.Context(), bucket.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
