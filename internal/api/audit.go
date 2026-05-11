package api

import (
	"net/http"
	"strconv"

	"tinydm/internal/audit"
)

// AuditHandler serves the audit log query API.
type AuditHandler struct {
	store *audit.Store
}

// NewAuditHandler creates a new AuditHandler.
func NewAuditHandler(store *audit.Store) *AuditHandler {
	return &AuditHandler{store: store}
}

// List handles GET /api/v1/audit
//
// Supported query parameters:
//
//	principal  — filter by username or API-key identifier (exact)
//	action     — filter by action string; append '*' for prefix match (e.g. "document.*")
//	resource   — filter by resource ID (exact)
//	from       — lower bound on created_at  (SQLite datetime, e.g. "2025-01-01 00:00:00")
//	to         — upper bound on created_at
//	limit      — page size (default 50, max 500)
//	offset     — pagination offset (default 0)
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	// Clamp here so pagination metadata reflects the actual values used by the store.
	if limit <= 0 || limit > audit.MaxLimit {
		limit = audit.DefaultLimit
	}
	if offset < 0 {
		offset = 0
	}

	f := audit.Filter{
		Principal: r.URL.Query().Get("principal"),
		Action:    r.URL.Query().Get("action"),
		Resource:  r.URL.Query().Get("resource"),
		From:      r.URL.Query().Get("from"),
		To:        r.URL.Query().Get("to"),
		Limit:     limit,
		Offset:    offset,
	}

	events, total, err := h.store.List(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if events == nil {
		events = []*audit.Event{}
	}
	writePaged(w, events, total, limit, offset)
}
