package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"tinydm/internal/auth"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
)

// DocumentHandler handles all document-scoped HTTP requests.
type DocumentHandler struct {
	store     *repo.Store
	authStore *auth.Store
	storage   storage.Store
}

// NewDocumentHandler creates a new DocumentHandler.
func NewDocumentHandler(store *repo.Store, authStore *auth.Store, storage storage.Store) *DocumentHandler {
	return &DocumentHandler{store: store, authStore: authStore, storage: storage}
}

// List handles GET /.../{bucketID}/documents
func (h *DocumentHandler) List(w http.ResponseWriter, r *http.Request) {
	bucket := bucketFromCtx(r)

	// If a search query is present, delegate to search.
	if q := r.URL.Query().Get("q"); q != "" {
		docs, err := h.store.SearchDocuments(r.Context(), bucket.ID, q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if docs == nil {
			docs = []*repo.Document{}
		}
		writeJSON(w, http.StatusOK, docs)
		return
	}

	docs, err := h.store.ListDocuments(r.Context(), bucket.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if docs == nil {
		docs = []*repo.Document{}
	}
	writeJSON(w, http.StatusOK, docs)
}

// Get handles GET /.../{bucketID}/documents/{documentID}
func (h *DocumentHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, documentFromCtx(r))
}

// Upload handles POST /.../{bucketID}/documents
// Expects multipart/form-data with a "file" field.
// An optional "name" field overrides the original filename.
func (h *DocumentHandler) Upload(w http.ResponseWriter, r *http.Request) {
	bucket := bucketFromCtx(r)
	p, _ := auth.PrincipalFromContext(r.Context())

	// Accept up to 32 MB in memory; remainder spills to temp files.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `"file" field is required`)
		return
	}
	defer file.Close()

	name := r.FormValue("name")
	if name == "" {
		name = header.Filename
	}
	if name == "" {
		writeError(w, http.StatusBadRequest, "file name could not be determined")
		return
	}

	// Detect content type: sniff the first 512 bytes then seek back.
	var sniff [512]byte
	n, _ := file.Read(sniff[:])
	contentType := http.DetectContentType(sniff[:n])
	// Honour the client-supplied type if it's more specific.
	if ct := header.Header.Get("Content-Type"); ct != "" && ct != "application/octet-stream" {
		contentType = ct
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "could not read file")
		return
	}

	key, size, checksum, err := h.storage.Put(r.Context(), file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store file")
		return
	}

	doc, err := h.store.CreateDocument(r.Context(), bucket.ID, name, contentType, size, checksum, key, p.ID)
	if err != nil {
		var conflict *repo.ErrConflict
		if errors.As(err, &conflict) {
			writeError(w, http.StatusConflict, conflict.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create document record")
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

// Download handles GET /.../{documentID}/content
// Streams the raw file bytes with appropriate headers.
func (h *DocumentHandler) Download(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)

	rc, err := h.storage.Get(r.Context(), doc.StorageKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "content not found")
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, doc.Name))
	w.Header().Set("Content-Length", strconv.FormatInt(doc.Size, 10))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, rc) //nolint:errcheck // client disconnect is not actionable
}

// Update handles PUT /.../{documentID}
// Accepts multipart/form-data. A new "file" field replaces the content and
// snapshots the previous version automatically. The "name" field is optional.
func (h *DocumentHandler) Update(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	p, _ := auth.PrincipalFromContext(r.Context())

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}

	// Keep existing values as defaults.
	name := r.FormValue("name")
	if name == "" {
		name = doc.Name
	}
	contentType := doc.ContentType
	size := doc.Size
	checksum := doc.Checksum
	storageKey := doc.StorageKey

	// Replace content if a new file was supplied.
	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()

		var sniff [512]byte
		n, _ := file.Read(sniff[:])
		ct := http.DetectContentType(sniff[:n])
		if hct := header.Header.Get("Content-Type"); hct != "" && hct != "application/octet-stream" {
			ct = hct
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, "could not read file")
			return
		}

		key, sz, cs, err := h.storage.Put(r.Context(), file)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to store file")
			return
		}
		contentType = ct
		size = sz
		checksum = cs
		storageKey = key
	}

	updated, err := h.store.UpdateDocument(r.Context(), doc.ID, name, contentType, size, checksum, storageKey, p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update document")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// Delete handles DELETE /.../{documentID}
func (h *DocumentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	if err := h.store.DeleteDocument(r.Context(), doc.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListVersions handles GET /.../{documentID}/versions
func (h *DocumentHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	versions, err := h.store.ListDocumentVersions(r.Context(), doc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if versions == nil {
		versions = []*repo.DocumentVersion{}
	}
	writeJSON(w, http.StatusOK, versions)
}
