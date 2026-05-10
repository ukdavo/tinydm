package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"tinydm/internal/auth"
	"tinydm/internal/cluster"
	"tinydm/internal/meta"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
)

// headerBytes is the number of bytes read ahead for content-type sniffing and
// metadata extraction. 64 KiB covers image config headers, PDF magic bytes, and
// Office signatures while keeping memory overhead low.
const headerBytes = 65536

// DocumentHandler handles all document-scoped HTTP requests.
type DocumentHandler struct {
	store     *repo.Store
	authStore *auth.Store
	storage   storage.Store
	locker    cluster.Locker
}

// NewDocumentHandler creates a new DocumentHandler.
func NewDocumentHandler(store *repo.Store, authStore *auth.Store, storage storage.Store, locker cluster.Locker) *DocumentHandler {
	return &DocumentHandler{store: store, authStore: authStore, storage: storage, locker: locker}
}

// List handles GET /.../{bucketID}/documents
// Supports ?q= (name search) and ?tag= (tag filter). The two filters are
// mutually exclusive; ?tag= takes precedence.
func (h *DocumentHandler) List(w http.ResponseWriter, r *http.Request) {
	bucket := bucketFromCtx(r)
	page := pageParams(r)

	// Tag filter.
	if tag := r.URL.Query().Get("tag"); tag != "" {
		docs, total, err := h.store.ListDocumentsByTag(r.Context(), bucket.ID, tag, page)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if docs == nil {
			docs = []*repo.Document{}
		}
		writePaged(w, docs, total, page.Limit, page.Offset)
		return
	}

	// Name search.
	if q := r.URL.Query().Get("q"); q != "" {
		docs, total, err := h.store.SearchDocuments(r.Context(), bucket.ID, q, page)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if docs == nil {
			docs = []*repo.Document{}
		}
		writePaged(w, docs, total, page.Limit, page.Offset)
		return
	}

	docs, total, err := h.store.ListDocuments(r.Context(), bucket.ID, page)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if docs == nil {
		docs = []*repo.Document{}
	}
	writePaged(w, docs, total, page.Limit, page.Offset)
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

	// Acquire a cluster-wide lock on the bucket to serialise concurrent
	// document creation and prevent name-collision races across nodes.
	unlock, err := h.locker.Lock(r.Context(), "bucket:"+bucket.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "could not acquire lock")
		return
	}
	defer unlock()

	// Hard cap on total upload size before touching the multipart parser.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

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
	name = sanitizeFilename(name)

	// Read a leading slice for content-type sniffing and metadata extraction,
	// then seek back so the full file is stored.
	// Always use stdlib detection — never trust the client-supplied Content-Type
	// as it can be forged to misrepresent file types.
	hdr := make([]byte, headerBytes)
	n, _ := file.Read(hdr)
	hdr = hdr[:n]

	contentType := http.DetectContentType(hdr)
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

	// Extract and persist metadata properties (best-effort).
	if props := meta.Extract(contentType, hdr); len(props) > 0 {
		_ = h.store.MergeDocumentProperties(r.Context(), doc.ID, props)
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

	// Acquire a cluster-wide lock on this document to prevent concurrent
	// updates from racing across nodes.
	unlock, err := h.locker.Lock(r.Context(), "doc:"+doc.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "could not acquire lock")
		return
	}
	defer unlock()

	// Hard cap on total upload size before touching the multipart parser.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}

	// Keep existing values as defaults.
	name := r.FormValue("name")
	if name == "" {
		name = doc.Name
	}
	if name != doc.Name {
		name = sanitizeFilename(name)
	}
	contentType := doc.ContentType
	size := doc.Size
	checksum := doc.Checksum
	storageKey := doc.StorageKey
	var metaProps map[string]string

	// Replace content if a new file was supplied.
	file, _, err := r.FormFile("file")
	if err == nil {
		defer file.Close()

		hdr := make([]byte, headerBytes)
		n, _ := file.Read(hdr)
		hdr = hdr[:n]

		// Always detect from content — do not trust the client-supplied type.
		ct := http.DetectContentType(hdr)
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, "could not read file")
			return
		}

		key, sz, cs, storeErr := h.storage.Put(r.Context(), file)
		if storeErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to store file")
			return
		}
		contentType = ct
		size = sz
		checksum = cs
		storageKey = key
		metaProps = meta.Extract(contentType, hdr)
	}

	updated, err := h.store.UpdateDocument(r.Context(), doc.ID, name, contentType, size, checksum, storageKey, p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update document")
		return
	}

	// Merge extracted metadata (best-effort, only when a new file was provided).
	if len(metaProps) > 0 {
		_ = h.store.MergeDocumentProperties(r.Context(), updated.ID, metaProps)
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
	page := pageParams(r)
	versions, total, err := h.store.ListDocumentVersions(r.Context(), doc.ID, page)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if versions == nil {
		versions = []*repo.DocumentVersion{}
	}
	writePaged(w, versions, total, page.Limit, page.Offset)
}

// RestoreVersion handles POST /.../{documentID}/versions/{versionID}/restore
// Snapshots the current document state then restores the named version's content.
func (h *DocumentHandler) RestoreVersion(w http.ResponseWriter, r *http.Request) {
	doc := documentFromCtx(r)
	version := versionFromCtx(r)
	p, _ := auth.PrincipalFromContext(r.Context())

	// Lock the document for the duration of the restore to prevent racing
	// updates from another node.
	unlock, err := h.locker.Lock(r.Context(), "doc:"+doc.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "could not acquire lock")
		return
	}
	defer unlock()

	updated, err := h.store.RestoreDocumentVersion(r.Context(), doc.ID, version.ID, p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore version")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
