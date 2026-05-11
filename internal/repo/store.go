// Package repo provides CRUD operations for the TinyDM repository hierarchy:
// Tenant → Project → Bucket → Document.
package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"tinydm/internal/db"
)

// ─── Domain types ─────────────────────────────────────────────────────────────

// Tenant is the top-level isolation boundary.
type Tenant struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Project belongs to a Tenant.
type Project struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Bucket belongs to a Project.
type Bucket struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Document belongs to a Bucket.
type Document struct {
	ID          string `json:"id"`
	BucketID    string `json:"bucket_id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Checksum    string `json:"checksum"`
	StorageKey  string `json:"-"` // internal — not exposed in API responses
	Version     int    `json:"version"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// DocumentVersion is a historical snapshot of a document.
type DocumentVersion struct {
	ID          string `json:"id"`
	DocumentID  string `json:"document_id"`
	Version     int    `json:"version"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Checksum    string `json:"checksum"`
	StorageKey  string `json:"-"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

// ─── Store ────────────────────────────────────────────────────────────────────

// Store handles all repository CRUD operations.
type Store struct {
	db *db.DB
}

// NewStore creates a new repository Store backed by db.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// ─── Pagination ───────────────────────────────────────────────────────────────

const (
	DefaultPageLimit = 50
	MaxPageLimit     = 500
)

// PageOpts controls limit/offset pagination for list queries.
type PageOpts struct {
	Limit  int
	Offset int
}

// validated clamps Limit and Offset to safe values.
func (p PageOpts) validated() PageOpts {
	if p.Limit <= 0 {
		p.Limit = DefaultPageLimit
	}
	if p.Limit > MaxPageLimit {
		p.Limit = MaxPageLimit
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// ─── Tenants ──────────────────────────────────────────────────────────────────

func (s *Store) CreateTenant(ctx context.Context, name, description string) (*Tenant, error) {
	id := uuid.New().String()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, description) VALUES (?, ?, ?)`,
		id, name, description,
	); err != nil {
		return nil, wrapConstraint(err, "tenant", name)
	}
	return s.GetTenant(ctx, id)
}

func (s *Store) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, created_at, updated_at
		 FROM tenants WHERE id = ? AND deleted_at IS NULL`, id)
	return scanTenant(row)
}

// GetTenantByName looks up a tenant by its public display name (case-insensitive).
// Returns nil, nil when no matching tenant exists.
func (s *Store) GetTenantByName(ctx context.Context, name string) (*Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, created_at, updated_at
		 FROM tenants WHERE lower(name) = lower(?) AND deleted_at IS NULL
		 LIMIT 1`, name)
	return scanTenant(row)
}

func (s *Store) ListTenants(ctx context.Context, page PageOpts) ([]*Tenant, int, error) {
	page = page.validated()
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, created_at, updated_at
		 FROM tenants WHERE deleted_at IS NULL ORDER BY name
		 LIMIT ? OFFSET ?`, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	var out []*Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan tenant: %w", err)
		}
		out = append(out, &t)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateTenant(ctx context.Context, id, name, description string) (*Tenant, error) {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE tenants SET name=?, description=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND deleted_at IS NULL`,
		name, description, id,
	); err != nil {
		return nil, wrapConstraint(err, "tenant", name)
	}
	return s.GetTenant(ctx, id)
}

func (s *Store) DeleteTenant(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tenants SET deleted_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

// ─── Projects ─────────────────────────────────────────────────────────────────

func (s *Store) CreateProject(ctx context.Context, tenantID, name, description string) (*Project, error) {
	id := uuid.New().String()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (id, tenant_id, name, description) VALUES (?, ?, ?, ?)`,
		id, tenantID, name, description,
	); err != nil {
		return nil, wrapConstraint(err, "project", name)
	}
	return s.GetProject(ctx, id)
}

func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, description, created_at, updated_at
		 FROM projects WHERE id=? AND deleted_at IS NULL`, id)
	return scanProject(row)
}

func (s *Store) ListProjects(ctx context.Context, tenantID string, page PageOpts) ([]*Project, int, error) {
	page = page.validated()
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM projects WHERE tenant_id=? AND deleted_at IS NULL`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count projects: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, description, created_at, updated_at
		 FROM projects WHERE tenant_id=? AND deleted_at IS NULL ORDER BY name
		 LIMIT ? OFFSET ?`, tenantID, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan project: %w", err)
		}
		out = append(out, &p)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateProject(ctx context.Context, id, name, description string) (*Project, error) {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE projects SET name=?, description=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND deleted_at IS NULL`,
		name, description, id,
	); err != nil {
		return nil, wrapConstraint(err, "project", name)
	}
	return s.GetProject(ctx, id)
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE projects SET deleted_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

// ─── Buckets ──────────────────────────────────────────────────────────────────

func (s *Store) CreateBucket(ctx context.Context, projectID, name, description string) (*Bucket, error) {
	id := uuid.New().String()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO buckets (id, project_id, name, description) VALUES (?, ?, ?, ?)`,
		id, projectID, name, description,
	); err != nil {
		return nil, wrapConstraint(err, "bucket", name)
	}
	return s.GetBucket(ctx, id)
}

func (s *Store) GetBucket(ctx context.Context, id string) (*Bucket, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, description, created_at, updated_at
		 FROM buckets WHERE id=? AND deleted_at IS NULL`, id)
	return scanBucket(row)
}

func (s *Store) ListBuckets(ctx context.Context, projectID string, page PageOpts) ([]*Bucket, int, error) {
	page = page.validated()
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM buckets WHERE project_id=? AND deleted_at IS NULL`, projectID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count buckets: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, description, created_at, updated_at
		 FROM buckets WHERE project_id=? AND deleted_at IS NULL ORDER BY name
		 LIMIT ? OFFSET ?`, projectID, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list buckets: %w", err)
	}
	defer rows.Close()
	var out []*Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Name, &b.Description, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan bucket: %w", err)
		}
		out = append(out, &b)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateBucket(ctx context.Context, id, name, description string) (*Bucket, error) {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE buckets SET name=?, description=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND deleted_at IS NULL`,
		name, description, id,
	); err != nil {
		return nil, wrapConstraint(err, "bucket", name)
	}
	return s.GetBucket(ctx, id)
}

func (s *Store) DeleteBucket(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE buckets SET deleted_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

// ─── Documents ────────────────────────────────────────────────────────────────

func (s *Store) CreateDocument(ctx context.Context, bucketID, name, contentType string, size int64, checksum, storageKey, createdBy string) (*Document, error) {
	id := uuid.New().String()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO documents (id, bucket_id, name, content_type, size, checksum, storage_key, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, bucketID, name, contentType, size, checksum, storageKey, createdBy,
	); err != nil {
		return nil, wrapConstraint(err, "document", name)
	}
	return s.GetDocument(ctx, id)
}

func (s *Store) GetDocument(ctx context.Context, id string) (*Document, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, bucket_id, name, content_type, size, checksum, storage_key, version, created_by, created_at, updated_at
		 FROM documents WHERE id=? AND deleted_at IS NULL`, id)
	return scanDocument(row)
}

func (s *Store) ListDocuments(ctx context.Context, bucketID string, page PageOpts) ([]*Document, int, error) {
	page = page.validated()
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE bucket_id=? AND deleted_at IS NULL`, bucketID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count documents: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, bucket_id, name, content_type, size, checksum, storage_key, version, created_by, created_at, updated_at
		 FROM documents WHERE bucket_id=? AND deleted_at IS NULL ORDER BY name
		 LIMIT ? OFFSET ?`, bucketID, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()
	docs, err := scanDocuments(rows)
	return docs, total, err
}

func (s *Store) SearchDocuments(ctx context.Context, bucketID, query string, page PageOpts) ([]*Document, int, error) {
	page = page.validated()
	pattern := "%" + query + "%"
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE bucket_id=? AND name LIKE ? AND deleted_at IS NULL`,
		bucketID, pattern).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count search documents: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, bucket_id, name, content_type, size, checksum, storage_key, version, created_by, created_at, updated_at
		 FROM documents WHERE bucket_id=? AND name LIKE ? AND deleted_at IS NULL ORDER BY name
		 LIMIT ? OFFSET ?`,
		bucketID, pattern, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("search documents: %w", err)
	}
	defer rows.Close()
	docs, err := scanDocuments(rows)
	return docs, total, err
}

// UpdateDocument snapshots the current version then updates the document record.
func (s *Store) UpdateDocument(ctx context.Context, id, name, contentType string, size int64, checksum, storageKey, updatedBy string) (*Document, error) {
	// Load current state for the version snapshot.
	current, err := s.GetDocument(ctx, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("document not found: %s", id)
	}

	// Snapshot the current version.
	if _, err := s.CreateDocumentVersion(ctx, current.ID, current.Version,
		current.ContentType, current.Size, current.Checksum, current.StorageKey, updatedBy); err != nil {
		return nil, fmt.Errorf("snapshot version: %w", err)
	}

	// Apply the update.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE documents
		 SET name=?, content_type=?, size=?, checksum=?, storage_key=?,
		     version=version+1, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND deleted_at IS NULL`,
		name, contentType, size, checksum, storageKey, id,
	); err != nil {
		return nil, fmt.Errorf("update document: %w", err)
	}
	return s.GetDocument(ctx, id)
}

func (s *Store) DeleteDocument(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE documents SET deleted_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

// ─── Document versions ────────────────────────────────────────────────────────

func (s *Store) CreateDocumentVersion(ctx context.Context, docID string, version int, contentType string, size int64, checksum, storageKey, createdBy string) (*DocumentVersion, error) {
	id := uuid.New().String()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO document_versions (id, document_id, version, content_type, size, checksum, storage_key, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, docID, version, contentType, size, checksum, storageKey, createdBy,
	); err != nil {
		return nil, fmt.Errorf("create document version: %w", err)
	}
	var v DocumentVersion
	row := s.db.QueryRowContext(ctx,
		`SELECT id, document_id, version, content_type, size, checksum, storage_key, created_by, created_at
		 FROM document_versions WHERE id=?`, id)
	if err := row.Scan(&v.ID, &v.DocumentID, &v.Version, &v.ContentType,
		&v.Size, &v.Checksum, &v.StorageKey, &v.CreatedBy, &v.CreatedAt); err != nil {
		return nil, fmt.Errorf("scan version: %w", err)
	}
	return &v, nil
}

func (s *Store) ListDocumentVersions(ctx context.Context, docID string, page PageOpts) ([]*DocumentVersion, int, error) {
	page = page.validated()
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM document_versions WHERE document_id=?`, docID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count versions: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, document_id, version, content_type, size, checksum, storage_key, created_by, created_at
		 FROM document_versions WHERE document_id=? ORDER BY version DESC
		 LIMIT ? OFFSET ?`, docID, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()
	var out []*DocumentVersion
	for rows.Next() {
		var v DocumentVersion
		if err := rows.Scan(&v.ID, &v.DocumentID, &v.Version, &v.ContentType,
			&v.Size, &v.Checksum, &v.StorageKey, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan version: %w", err)
		}
		out = append(out, &v)
	}
	return out, total, rows.Err()
}

// RenameDocument updates only the document name without snapshotting a version.
func (s *Store) RenameDocument(ctx context.Context, id, name string) (*Document, error) {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE documents SET name=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND deleted_at IS NULL`,
		name, id,
	); err != nil {
		return nil, fmt.Errorf("rename document: %w", err)
	}
	return s.GetDocument(ctx, id)
}

// CountDocumentsInBucket returns the number of non-deleted documents in a bucket.
func (s *Store) CountDocumentsInBucket(ctx context.Context, bucketID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE bucket_id=? AND deleted_at IS NULL`,
		bucketID).Scan(&n)
	return n, err
}

// CountBucketsInProject returns the number of non-deleted buckets in a project.
func (s *Store) CountBucketsInProject(ctx context.Context, projectID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM buckets WHERE project_id = ? AND deleted_at IS NULL`, projectID).Scan(&n)
	return n, err
}

// CountDocumentsInProject returns the number of non-deleted documents across all
// buckets in a project.
func (s *Store) CountDocumentsInProject(ctx context.Context, projectID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM documents d
		JOIN buckets b ON d.bucket_id = b.id
		WHERE b.project_id = ? AND d.deleted_at IS NULL`, projectID).Scan(&n)
	return n, err
}

// SumDocumentSizeInBucket returns the total byte size of all non-deleted documents
// in a bucket. Returns 0 for an empty bucket.
func (s *Store) SumDocumentSizeInBucket(ctx context.Context, bucketID string) (int64, error) {
	var total int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size), 0) FROM documents WHERE bucket_id = ? AND deleted_at IS NULL`,
		bucketID).Scan(&total)
	return total, err
}

// ─── Counts (used by the admin dashboard) ────────────────────────────────────

// CountTenants returns the number of non-deleted tenants.
func (s *Store) CountTenants(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL`).Scan(&n)
	return n, err
}

// CountProjects returns the number of non-deleted projects for a tenant.
// If tenantID is empty, counts across all tenants.
func (s *Store) CountProjects(ctx context.Context, tenantID string) (int, error) {
	var n int
	var err error
	if tenantID == "" {
		err = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM projects WHERE deleted_at IS NULL`).Scan(&n)
	} else {
		err = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM projects WHERE tenant_id = ? AND deleted_at IS NULL`,
			tenantID).Scan(&n)
	}
	return n, err
}

// CountBuckets returns the number of non-deleted buckets for a tenant's projects.
// If tenantID is empty, counts across all tenants.
func (s *Store) CountBuckets(ctx context.Context, tenantID string) (int, error) {
	var n int
	var err error
	if tenantID == "" {
		err = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM buckets WHERE deleted_at IS NULL`).Scan(&n)
	} else {
		err = s.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM buckets b
			JOIN projects p ON b.project_id = p.id
			WHERE p.tenant_id = ? AND b.deleted_at IS NULL`, tenantID).Scan(&n)
	}
	return n, err
}

// CountDocuments returns the number of non-deleted documents for a tenant.
// If tenantID is empty, counts across all tenants.
func (s *Store) CountDocuments(ctx context.Context, tenantID string) (int, error) {
	var n int
	var err error
	if tenantID == "" {
		err = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM documents WHERE deleted_at IS NULL`).Scan(&n)
	} else {
		err = s.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM documents d
			JOIN buckets b ON d.bucket_id = b.id
			JOIN projects p ON b.project_id = p.id
			WHERE p.tenant_id = ? AND d.deleted_at IS NULL`, tenantID).Scan(&n)
	}
	return n, err
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// ErrConflict is returned when a unique constraint is violated.
type ErrConflict struct{ Msg string }

func (e *ErrConflict) Error() string { return e.Msg }

func wrapConstraint(err error, resource, name string) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return &ErrConflict{Msg: fmt.Sprintf("%s %q already exists", resource, name)}
	}
	return err
}

func scanTenant(row *sql.Row) (*Tenant, error) {
	var t Tenant
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan tenant: %w", err)
	}
	return &t, nil
}

func scanProject(row *sql.Row) (*Project, error) {
	var p Project
	if err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan project: %w", err)
	}
	return &p, nil
}

func scanBucket(row *sql.Row) (*Bucket, error) {
	var b Bucket
	if err := row.Scan(&b.ID, &b.ProjectID, &b.Name, &b.Description, &b.CreatedAt, &b.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan bucket: %w", err)
	}
	return &b, nil
}

func scanDocument(row *sql.Row) (*Document, error) {
	var d Document
	if err := row.Scan(&d.ID, &d.BucketID, &d.Name, &d.ContentType,
		&d.Size, &d.Checksum, &d.StorageKey, &d.Version, &d.CreatedBy,
		&d.CreatedAt, &d.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan document: %w", err)
	}
	return &d, nil
}

func scanDocuments(rows *sql.Rows) ([]*Document, error) {
	var out []*Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.BucketID, &d.Name, &d.ContentType,
			&d.Size, &d.Checksum, &d.StorageKey, &d.Version, &d.CreatedBy,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// ─── Version restore ──────────────────────────────────────────────────────────

// GetDocumentVersion returns a single version record, verifying it belongs to docID.
func (s *Store) GetDocumentVersion(ctx context.Context, versionID, docID string) (*DocumentVersion, error) {
	var v DocumentVersion
	err := s.db.QueryRowContext(ctx,
		`SELECT id, document_id, version, content_type, size, checksum, storage_key, created_by, created_at
		 FROM document_versions WHERE id=? AND document_id=?`, versionID, docID,
	).Scan(&v.ID, &v.DocumentID, &v.Version, &v.ContentType,
		&v.Size, &v.Checksum, &v.StorageKey, &v.CreatedBy, &v.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get document version: %w", err)
	}
	return &v, nil
}

// RestoreDocumentVersion snapshots the current document state then replaces its
// content with the values from the specified version record.
func (s *Store) RestoreDocumentVersion(ctx context.Context, docID, versionID, restoredBy string) (*Document, error) {
	v, err := s.GetDocumentVersion(ctx, versionID, docID)
	if err != nil || v == nil {
		return nil, fmt.Errorf("version not found: %s", versionID)
	}
	current, err := s.GetDocument(ctx, docID)
	if err != nil || current == nil {
		return nil, fmt.Errorf("document not found: %s", docID)
	}
	// UpdateDocument snapshots current state automatically before applying changes.
	return s.UpdateDocument(ctx, docID, current.Name,
		v.ContentType, v.Size, v.Checksum, v.StorageKey, restoredBy)
}

// ─── Tags ─────────────────────────────────────────────────────────────────────

// ListDocumentTags returns all tags for the given document, sorted.
func (s *Store) ListDocumentTags(ctx context.Context, docID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tag FROM document_tags WHERE document_id=? ORDER BY tag`, docID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// AddDocumentTag inserts a tag for the document (no-op if it already exists).
func (s *Store) AddDocumentTag(ctx context.Context, docID, tag string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO document_tags (document_id, tag) VALUES (?, ?) ON CONFLICT DO NOTHING`, docID, tag)
	return err
}

// RemoveDocumentTag deletes a single tag from the document.
func (s *Store) RemoveDocumentTag(ctx context.Context, docID, tag string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM document_tags WHERE document_id=? AND tag=?`, docID, tag)
	return err
}

// SetDocumentTags replaces all tags for the document atomically.
func (s *Store) SetDocumentTags(ctx context.Context, docID string, tags []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM document_tags WHERE document_id=?`, docID); err != nil {
		return err
	}
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO document_tags (document_id, tag) VALUES (?, ?) ON CONFLICT DO NOTHING`, docID, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListDocumentsByTag returns documents in a bucket that carry the given tag.
func (s *Store) ListDocumentsByTag(ctx context.Context, bucketID, tag string, page PageOpts) ([]*Document, int, error) {
	page = page.validated()
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents d
		 JOIN document_tags dt ON dt.document_id = d.id AND dt.tag = ?
		 WHERE d.bucket_id=? AND d.deleted_at IS NULL`,
		tag, bucketID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count documents by tag: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT d.id, d.bucket_id, d.name, d.content_type, d.size, d.checksum,
		        d.storage_key, d.version, d.created_by, d.created_at, d.updated_at
		 FROM documents d
		 JOIN document_tags dt ON dt.document_id = d.id AND dt.tag = ?
		 WHERE d.bucket_id=? AND d.deleted_at IS NULL ORDER BY d.name
		 LIMIT ? OFFSET ?`,
		tag, bucketID, page.Limit, page.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list documents by tag: %w", err)
	}
	defer rows.Close()
	docs, err := scanDocuments(rows)
	return docs, total, err
}

// ─── Custom properties ────────────────────────────────────────────────────────

// GetDocumentProperties returns all custom properties as a map.
func (s *Store) GetDocumentProperties(ctx context.Context, docID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, value FROM document_properties WHERE document_id=? ORDER BY key`, docID)
	if err != nil {
		return nil, fmt.Errorf("get properties: %w", err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan property: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetDocumentProperty upserts a single key/value property.
func (s *Store) SetDocumentProperty(ctx context.Context, docID, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO document_properties (document_id, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(document_id, key) DO UPDATE SET value=excluded.value`,
		docID, key, value)
	return err
}

// DeleteDocumentProperty removes a single property key.
func (s *Store) DeleteDocumentProperty(ctx context.Context, docID, key string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM document_properties WHERE document_id=? AND key=?`, docID, key)
	return err
}

// SetDocumentProperties replaces all custom properties atomically.
// Keys prefixed with "sys." are reserved for system metadata and are skipped.
func (s *Store) SetDocumentProperties(ctx context.Context, docID string, props map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	// Remove only non-system properties.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM document_properties WHERE document_id=? AND key NOT LIKE 'sys.%'`, docID); err != nil {
		return err
	}
	for k, v := range props {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO document_properties (document_id, key, value) VALUES (?, ?, ?)
			 ON CONFLICT(document_id, key) DO UPDATE SET value=excluded.value`,
			docID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MergeDocumentProperties upserts the given key/value pairs without touching
// existing keys that are absent from props.
func (s *Store) MergeDocumentProperties(ctx context.Context, docID string, props map[string]string) error {
	if len(props) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range props {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO document_properties (document_id, key, value) VALUES (?, ?, ?)
			 ON CONFLICT(document_id, key) DO UPDATE SET value=excluded.value`,
			docID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}
