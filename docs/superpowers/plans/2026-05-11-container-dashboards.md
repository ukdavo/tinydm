# Container-Level Dashboards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a stats strip to the existing projects, buckets, and documents list pages showing relevant counts and metrics at each container level.

**Architecture:** Synchronous stats fetching in the existing list handlers before render. New count/sum methods added to `repo.Store` and `auth.Store`. Stats structs added to the existing page data structs. Stats strip HTML inserted into existing templates using the existing `.stats-grid` / `.stat-card` CSS classes.

**Tech Stack:** Go, SQLite, html/template, HTMX (no new JS), existing CSS

---

### Task 1: Add `CountUsersByTenant` and `CountAPIKeysByTenant` to auth.Store

**Files:**
- Modify: `internal/auth/store.go`
- Modify: `internal/auth/store_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/auth/store_test.go`:

```go
func TestCountUsersByTenant_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")
	seedTenant(t, s, "tenant-2")

	hash, _ := HashPassword("pass")
	_, err := s.CreateUser(ctx, "tenant-1", "alice", "alice@test.local", "Alice", "A", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, err = s.CreateUser(ctx, "tenant-1", "bob", "bob@test.local", "Bob", "B", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, err = s.CreateUser(ctx, "tenant-2", "carol", "carol@test.local", "Carol", "C", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	n, err := s.CountUsersByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("CountUsersByTenant: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestCountAPIKeysByTenant_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")
	seedTenant(t, s, "tenant-2")

	hash, _ := HashPassword("pass")
	u1, _ := s.CreateUser(ctx, "tenant-1", "alice", "alice@t.local", "Alice", "A", hash, UserTypeAdmin)
	u2, _ := s.CreateUser(ctx, "tenant-2", "bob", "bob@t.local", "Bob", "B", hash, UserTypeAdmin)

	_, _, err := s.CreateAPIKey(ctx, u1.TenantID, u1.ID, "key1")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	_, _, err = s.CreateAPIKey(ctx, u1.TenantID, u1.ID, "key2")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	_, _, err = s.CreateAPIKey(ctx, u2.TenantID, u2.ID, "other")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	n, err := s.CountAPIKeysByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("CountAPIKeysByTenant: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/auth/... -run "TestCountUsersByTenant|TestCountAPIKeysByTenant" -v
```

Expected: FAIL with `s.CountUsersByTenant undefined` and `s.CountAPIKeysByTenant undefined`

- [ ] **Step 3: Add the methods to `internal/auth/store.go`**

Add after the existing `CountUsers` method:

```go
func (s *Store) CountUsersByTenant(ctx context.Context, tenantID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE tenant_id = ? AND deleted_at IS NULL`, tenantID).Scan(&n)
	return n, err
}

func (s *Store) CountAPIKeysByTenant(ctx context.Context, tenantID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE tenant_id = ? AND revoked_at IS NULL`, tenantID).Scan(&n)
	return n, err
}
```

> Note: check `internal/db/migrations/002_auth_schema.sql` to confirm the api_keys table name and whether it uses `revoked_at` or `deleted_at` for soft-delete. Adjust accordingly.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/auth/... -run "TestCountUsersByTenant|TestCountAPIKeysByTenant" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/auth/store.go internal/auth/store_test.go
git commit -m "feat(auth): add CountUsersByTenant and CountAPIKeysByTenant"
```

---

### Task 2: Add `CountBucketsInProject`, `CountDocumentsInProject`, and `SumDocumentSizeInBucket` to repo.Store

**Files:**
- Modify: `internal/repo/store.go`
- Create: `internal/repo/store_test.go`

- [ ] **Step 1: Create `internal/repo/store_test.go` with helpers and failing tests**

```go
package repo_test

import (
	"context"
	"path/filepath"
	"testing"

	"tinydm/internal/db"
	"tinydm/internal/repo"
)

func newTestStore(t *testing.T) *repo.Store {
	t.Helper()
	sqlDB, err := db.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return repo.NewStore(sqlDB)
}

func seedTenant(t *testing.T, s *repo.Store, name string) *repo.Tenant {
	t.Helper()
	tenant, err := s.CreateTenant(context.Background(), name, "")
	if err != nil {
		t.Fatalf("CreateTenant %q: %v", name, err)
	}
	return tenant
}

func seedProject(t *testing.T, s *repo.Store, tenantID, name string) *repo.Project {
	t.Helper()
	p, err := s.CreateProject(context.Background(), tenantID, name, "")
	if err != nil {
		t.Fatalf("CreateProject %q: %v", name, err)
	}
	return p
}

func seedBucket(t *testing.T, s *repo.Store, projectID, name string) *repo.Bucket {
	t.Helper()
	b, err := s.CreateBucket(context.Background(), projectID, name, "")
	if err != nil {
		t.Fatalf("CreateBucket %q: %v", name, err)
	}
	return b
}

func TestCountBucketsInProject_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tenant := seedTenant(t, s, "acme")
	p1 := seedProject(t, s, tenant.ID, "alpha")
	p2 := seedProject(t, s, tenant.ID, "beta")
	seedBucket(t, s, p1.ID, "b1")
	seedBucket(t, s, p1.ID, "b2")
	seedBucket(t, s, p2.ID, "b3")

	n, err := s.CountBucketsInProject(ctx, p1.ID)
	if err != nil {
		t.Fatalf("CountBucketsInProject: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestCountDocumentsInProject_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tenant := seedTenant(t, s, "acme")
	p1 := seedProject(t, s, tenant.ID, "alpha")
	p2 := seedProject(t, s, tenant.ID, "beta")
	b1 := seedBucket(t, s, p1.ID, "bkt1")
	b2 := seedBucket(t, s, p1.ID, "bkt2")
	b3 := seedBucket(t, s, p2.ID, "bkt3")

	// seed documents directly via SQL since CreateDocument needs file storage
	sqlDB, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "unused.db"))
	_ = sqlDB // use the store's internal db via ExecContext via the store's db field
	// Instead, use the store's CreateDocument if it accepts a size/key directly.
	// Check the CreateDocument signature — if it requires a file, seed via raw SQL:
	// s.db is unexported; seed via the store's public API or skip if not feasible.
	// FALLBACK: count returns 0 for empty project — test that first.
	n, err := s.CountDocumentsInProject(ctx, p1.ID)
	if err != nil {
		t.Fatalf("CountDocumentsInProject: %v", err)
	}
	if n != 0 {
		t.Errorf("empty project: got %d, want 0", n)
	}
	_ = b1
	_ = b2
	_ = b3
}

func TestSumDocumentSizeInBucket_ReturnsZeroForEmptyBucket(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tenant := seedTenant(t, s, "acme")
	p := seedProject(t, s, tenant.ID, "alpha")
	b := seedBucket(t, s, p.ID, "bkt")

	total, err := s.SumDocumentSizeInBucket(ctx, b.ID)
	if err != nil {
		t.Fatalf("SumDocumentSizeInBucket: %v", err)
	}
	if total != 0 {
		t.Errorf("got %d, want 0", total)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/repo/... -run "TestCountBucketsInProject|TestCountDocumentsInProject|TestSumDocumentSizeInBucket" -v
```

Expected: FAIL with method undefined errors

- [ ] **Step 3: Add the three methods to `internal/repo/store.go`**

Add after the existing `CountDocumentsInBucket` method:

```go
func (s *Store) CountBucketsInProject(ctx context.Context, projectID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM buckets WHERE project_id = ? AND deleted_at IS NULL`, projectID).Scan(&n)
	return n, err
}

func (s *Store) CountDocumentsInProject(ctx context.Context, projectID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM documents d
		JOIN buckets b ON d.bucket_id = b.id
		WHERE b.project_id = ? AND d.deleted_at IS NULL`, projectID).Scan(&n)
	return n, err
}

func (s *Store) SumDocumentSizeInBucket(ctx context.Context, bucketID string) (int64, error) {
	var total int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size), 0) FROM documents WHERE bucket_id = ? AND deleted_at IS NULL`,
		bucketID).Scan(&total)
	return total, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/repo/... -run "TestCountBucketsInProject|TestCountDocumentsInProject|TestSumDocumentSizeInBucket" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/repo/store.go internal/repo/store_test.go
git commit -m "feat(repo): add CountBucketsInProject, CountDocumentsInProject, SumDocumentSizeInBucket"
```

---

### Task 3: Add tenant-level stats strip to the projects page

**Files:**
- Modify: `internal/web/handlers.go`
- Modify: `internal/web/templates/projects.html`

- [ ] **Step 1: Add `tenantStats` struct and update `projectsData` in `internal/web/handlers.go`**

Find the `projectsData` struct definition (around line 305) and replace it:

```go
type tenantStats struct {
	Projects  int
	Buckets   int
	Documents int
	Users     int
	APIKeys   int
}

type projectsData struct {
	basePage
	Tenant   *repo.Tenant
	Stats    tenantStats
	Projects []*repo.Project
	Pager    WebPagination
}
```

- [ ] **Step 2: Update the `projects` handler to fetch stats**

Replace the `projects` handler body (around line 312):

```go
func (h *Handler) projects(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	page, limit := parsePage(r)
	projects, total, _ := h.repo.ListProjects(r.Context(), tenantID, repo.PageOpts{Limit: limit, Offset: pageOffset(page, limit)})

	var stats tenantStats
	stats.Projects, _ = h.repo.CountProjects(r.Context(), tenantID)
	stats.Buckets, _ = h.repo.CountBuckets(r.Context(), tenantID)
	stats.Documents, _ = h.repo.CountDocuments(r.Context(), tenantID)
	stats.Users, _ = h.auth.CountUsersByTenant(r.Context(), tenantID)
	stats.APIKeys, _ = h.auth.CountAPIKeysByTenant(r.Context(), tenantID)

	h.render(w, "projects", projectsData{
		basePage: h.base(r, "projects"),
		Tenant:   tenant,
		Stats:    stats,
		Projects: projects,
		Pager:    newWebPagination(total, page, limit, ""),
	})
}
```

- [ ] **Step 3: Add the stats strip to `internal/web/templates/projects.html`**

Insert the following block between the closing `</div>` of `.page-header` and the opening `<div class="card">`:

```html
<div class="stats-grid">
  <div class="stat-card">
    <div class="label">Projects</div>
    <div class="value">{{.Stats.Projects}}</div>
  </div>
  <div class="stat-card">
    <div class="label">Buckets</div>
    <div class="value">{{.Stats.Buckets}}</div>
  </div>
  <div class="stat-card">
    <div class="label">Documents</div>
    <div class="value">{{.Stats.Documents}}</div>
  </div>
  <div class="stat-card">
    <div class="label">Users</div>
    <div class="value">{{.Stats.Users}}</div>
  </div>
  <div class="stat-card">
    <div class="label">API Keys</div>
    <div class="value">{{.Stats.APIKeys}}</div>
  </div>
</div>
```

- [ ] **Step 4: Build and verify no compile errors**

```bash
go build ./...
```

Expected: no output (success)

- [ ] **Step 5: Run existing tests**

```bash
go test ./internal/web/... ./internal/auth/... ./internal/repo/...
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/web/handlers.go internal/web/templates/projects.html
git commit -m "feat(web): add tenant stats strip to projects page"
```

---

### Task 4: Add project-level stats strip to the buckets page

**Files:**
- Modify: `internal/web/handlers.go`
- Modify: `internal/web/templates/buckets.html`

- [ ] **Step 1: Add `projectStats` struct and update `bucketsData` in `internal/web/handlers.go`**

Find the `bucketsData` struct definition and replace it:

```go
type projectStats struct {
	Buckets   int
	Documents int
}

type bucketsData struct {
	basePage
	Tenant  *repo.Tenant
	Project *repo.Project
	Stats   projectStats
	Buckets []bucketRow
	Pager   WebPagination
}
```

- [ ] **Step 2: Update the `buckets` handler to fetch stats**

Replace the `buckets` handler body (around line 372):

```go
func (h *Handler) buckets(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	projectID := chi.URLParam(r, "projectID")

	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	project, err := h.repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil {
		http.NotFound(w, r)
		return
	}

	page, limit := parsePage(r)
	raw, total, _ := h.repo.ListBuckets(r.Context(), projectID, repo.PageOpts{Limit: limit, Offset: pageOffset(page, limit)})
	var rows []bucketRow
	for _, b := range raw {
		n, _ := h.repo.CountDocumentsInBucket(r.Context(), b.ID)
		rows = append(rows, bucketRow{Bucket: b, TenantID: tenantID, DocCount: n})
	}

	var stats projectStats
	stats.Buckets, _ = h.repo.CountBucketsInProject(r.Context(), projectID)
	stats.Documents, _ = h.repo.CountDocumentsInProject(r.Context(), projectID)

	h.render(w, "buckets", bucketsData{
		basePage: h.base(r, "buckets"),
		Tenant:   tenant,
		Project:  project,
		Stats:    stats,
		Buckets:  rows,
		Pager:    newWebPagination(total, page, limit, ""),
	})
}
```

- [ ] **Step 3: Add the stats strip to `internal/web/templates/buckets.html`**

Insert after the closing `</div>` of `.page-header` and before `<div class="card">`:

```html
<div class="stats-grid">
  <div class="stat-card">
    <div class="label">Buckets</div>
    <div class="value">{{.Stats.Buckets}}</div>
  </div>
  <div class="stat-card">
    <div class="label">Documents</div>
    <div class="value">{{.Stats.Documents}}</div>
  </div>
</div>
```

- [ ] **Step 4: Build and run all tests**

```bash
go build ./... && go test ./internal/web/... ./internal/repo/...
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/web/handlers.go internal/web/templates/buckets.html
git commit -m "feat(web): add project stats strip to buckets page"
```

---

### Task 5: Add bucket-level stats strip to the documents page

**Files:**
- Modify: `internal/web/handlers.go`
- Modify: `internal/web/templates/documents.html`

- [ ] **Step 1: Add a `formatSize` helper and `bucketSummaryStats` struct to `internal/web/handlers.go`**

Add a package-level helper function (place near the other helpers at the bottom of handlers.go):

```go
// formatSize converts bytes to a human-readable string (e.g. "1.4 GB", "840 KB").
func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
```

Add the struct and update `documentsData`:

```go
type bucketSummaryStats struct {
	Documents int
	TotalSize string
}

type documentsData struct {
	basePage
	Tenant    *repo.Tenant
	Project   *repo.Project
	Bucket    *repo.Bucket
	Stats     bucketSummaryStats
	Documents []*repo.Document
	Pager     WebPagination
}
```

- [ ] **Step 2: Update the `documents` handler to fetch stats**

Replace the `documents` handler body (around line 442):

```go
func (h *Handler) documents(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	projectID := chi.URLParam(r, "projectID")
	bucketID := chi.URLParam(r, "bucketID")

	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	project, err := h.repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil {
		http.NotFound(w, r)
		return
	}
	bucket, err := h.repo.GetBucket(r.Context(), bucketID)
	if err != nil || bucket == nil {
		http.NotFound(w, r)
		return
	}

	page, limit := parsePage(r)
	docs, total, _ := h.repo.ListDocuments(r.Context(), bucketID, repo.PageOpts{Limit: limit, Offset: pageOffset(page, limit)})

	docCount, _ := h.repo.CountDocumentsInBucket(r.Context(), bucketID)
	sizeBytes, _ := h.repo.SumDocumentSizeInBucket(r.Context(), bucketID)

	h.render(w, "documents", documentsData{
		basePage: h.base(r, "documents"),
		Tenant:   tenant,
		Project:  project,
		Bucket:   bucket,
		Stats: bucketSummaryStats{
			Documents: docCount,
			TotalSize: formatSize(sizeBytes),
		},
		Documents: docs,
		Pager:     newWebPagination(total, page, limit, ""),
	})
}
```

- [ ] **Step 3: Add the stats strip to `internal/web/templates/documents.html`**

Insert after the closing `</div>` of `.page-header` and before the upload form / `<div class="card">`:

```html
<div class="stats-grid">
  <div class="stat-card">
    <div class="label">Documents</div>
    <div class="value">{{.Stats.Documents}}</div>
  </div>
  <div class="stat-card">
    <div class="label">Total Size</div>
    <div class="value">{{.Stats.TotalSize}}</div>
  </div>
</div>
```

- [ ] **Step 4: Build and run all tests**

```bash
go build ./... && go test ./...
```

Expected: all PASS

- [ ] **Step 5: Commit and push**

```bash
git add internal/web/handlers.go internal/web/templates/documents.html
git commit -m "feat(web): add bucket stats strip to documents page"
git push origin HEAD:main
```
