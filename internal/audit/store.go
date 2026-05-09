// Package audit provides an immutable event log for all mutating operations
// performed against the TinyDM repository.
package audit

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// DefaultLimit is the page size used when the caller does not specify one.
const DefaultLimit = 50

// MaxLimit caps the number of events returned in a single request.
const MaxLimit = 500

// ─── Domain type ─────────────────────────────────────────────────────────────

// Event is a single immutable audit record.
type Event struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Principal string `json:"principal"`
	Action    string `json:"action"`
	Resource  string `json:"resource"`
	Detail    string `json:"detail,omitempty"`
	CreatedAt string `json:"created_at"`
}

// ─── Store ────────────────────────────────────────────────────────────────────

// Store handles reading and writing audit events.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store backed by db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Record inserts a new audit event. It is designed to be called fire-and-forget:
// a non-nil error should be logged but must not affect the request path.
func (s *Store) Record(ctx context.Context, tenantID, principal, action, resource, detail string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, tenant_id, principal, action, resource, detail)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New().String(), tenantID, principal, action, resource, detail,
	)
	return err
}

// ─── Query ────────────────────────────────────────────────────────────────────

// Filter describes the predicates for an audit log query.
// All string fields are optional — zero value means "no filter".
// Action supports a trailing '*' wildcard (e.g. "document.*" matches all
// document actions).
type Filter struct {
	TenantID  string // required
	Principal string // exact match on username / API-key identifier
	Action    string // exact match, or prefix match when ending with '*'
	Resource  string // exact match on resource ID
	From      string // lower bound on created_at (SQLite datetime string)
	To        string // upper bound on created_at
	Limit     int    // default DefaultLimit; capped at MaxLimit
	Offset    int    // for pagination
}

// List returns audit events for the given tenant, newest first.
// It also returns the total number of matching events (before limit/offset).
func (s *Store) List(ctx context.Context, f Filter) ([]*Event, int, error) {
	limit := f.Limit
	if limit <= 0 || limit > MaxLimit {
		limit = DefaultLimit
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"tenant_id = ?"}
	args := []any{f.TenantID}

	if f.Principal != "" {
		where = append(where, "principal = ?")
		args = append(args, f.Principal)
	}
	if f.Action != "" {
		if strings.HasSuffix(f.Action, "*") {
			prefix := strings.TrimSuffix(f.Action, "*")
			where = append(where, "action LIKE ?")
			args = append(args, prefix+"%")
		} else {
			where = append(where, "action = ?")
			args = append(args, f.Action)
		}
	}
	if f.Resource != "" {
		where = append(where, "resource = ?")
		args = append(args, f.Resource)
	}
	if f.From != "" {
		where = append(where, "created_at >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, "created_at <= ?")
		args = append(args, f.To)
	}

	whereClause := strings.Join(where, " AND ")

	// Total count (for pagination metadata) — same predicates, no limit/offset.
	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE `+whereClause, countArgs...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit: %w", err)
	}

	query := `SELECT id, tenant_id, principal, action, resource, detail, created_at
	          FROM audit_log
	          WHERE ` + whereClause + `
	          ORDER BY created_at DESC
	          LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	var out []*Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Principal,
			&e.Action, &e.Resource, &e.Detail, &e.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan audit event: %w", err)
		}
		out = append(out, &e)
	}
	return out, total, rows.Err()
}
