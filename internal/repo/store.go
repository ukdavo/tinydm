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
	db *sql.DB
}

// NewStore creates a new repository Store backed by db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
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

func (s *Store) ListTenants(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, created_at, updated_at
		 FROM tenants WHERE deleted_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	var out []*Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		out = append(out, &t)
	}
	return out, rows.Err()
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

func (s *Store) ListProjects(ctx context.Context, tenantID string) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, description, created_at, updated_at
		 FROM projects WHERE tenant_id=? AND deleted_at IS NULL ORDER BY name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		out = append(out, &p)
	}
	return out, rows.Err()
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

func (s *Store) ListBuckets(ctx context.Context, projectID string) ([]*Bucket, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, description, created_at, updated_at
		 FROM buckets WHERE project_id=? AND deleted_at IS NULL ORDER BY name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}
	defer rows.Close()
	var out []*Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Name, &b.Description, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan bucket: %w", err)
		}
		out = append(out, &b)
	}
	return out, rows.Err()
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

func (s *Store) ListDocuments(ctx context.Context, bucketID string) ([]*Document, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, bucket_id, name, content_type, size, checksum, storage_key, version, created_by, created_at, updated_at
		 FROM documents WHERE bucket_id=? AND deleted_at IS NULL ORDER BY name`, bucketID)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()
	return scanDocuments(rows)
}

func (s *Store) SearchDocuments(ctx context.Context, bucketID, query string) ([]*Document, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, bucket_id, name, content_type, size, checksum, storage_key, version, created_by, created_at, updated_at
		 FROM documents WHERE bucket_id=? AND name LIKE ? AND deleted_at IS NULL ORDER BY name`,
		bucketID, "%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("search documents: %w", err)
	}
	defer rows.Close()
	return scanDocuments(rows)
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

func (s *Store) ListDocumentVersions(ctx context.Context, docID string) ([]*DocumentVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, document_id, version, content_type, size, checksum, storage_key, created_by, created_at
		 FROM document_versions WHERE document_id=? ORDER BY version DESC`, docID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()
	var out []*DocumentVersion
	for rows.Next() {
		var v DocumentVersion
		if err := rows.Scan(&v.ID, &v.DocumentID, &v.Version, &v.ContentType,
			&v.Size, &v.Checksum, &v.StorageKey, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		out = append(out, &v)
	}
	return out, rows.Err()
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
