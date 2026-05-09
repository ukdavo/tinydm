// Package api contains all HTTP handler types and shared response helpers.
package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"tinydm/internal/repo"
)

// writeJSON encodes v as JSON and writes it to w with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON encode error", "error", err)
	}
}

// writeError writes a standard JSON error body.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decode reads a JSON body from r into dst.
// The body is capped at maxJSONBytes to prevent memory exhaustion.
func decode(r *http.Request, dst any) error {
	return json.NewDecoder(io.LimitReader(r.Body, maxJSONBytes)).Decode(dst)
}

// ─── Pagination ───────────────────────────────────────────────────────────────

// pageParams reads ?limit= and ?offset= from the request, clamping to safe values.
func pageParams(r *http.Request) repo.PageOpts {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	p := repo.PageOpts{Limit: limit, Offset: offset}
	// validated() is unexported; apply the same logic inline.
	if p.Limit <= 0 {
		p.Limit = repo.DefaultPageLimit
	}
	if p.Limit > repo.MaxPageLimit {
		p.Limit = repo.MaxPageLimit
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// PagedMeta carries the pagination envelope sent to API callers.
type PagedMeta struct {
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

// pagedResponse is the top-level envelope for any paginated list response.
type pagedResponse struct {
	Data       any       `json:"data"`
	Pagination PagedMeta `json:"pagination"`
}

// writePaged writes a paginated JSON response with the data envelope.
func writePaged(w http.ResponseWriter, data any, total, limit, offset int) {
	writeJSON(w, http.StatusOK, pagedResponse{
		Data: data,
		Pagination: PagedMeta{
			Total:   total,
			Limit:   limit,
			Offset:  offset,
			HasMore: offset+limit < total,
		},
	})
}
