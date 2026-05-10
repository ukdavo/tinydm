# Container-Level Dashboards Design

**Date:** 2026-05-11
**Status:** Approved

## Overview

Add stats strips to the existing list pages at the tenant, project, and bucket levels. Each strip sits between the breadcrumb and the list card, using the same `.stats-grid` / `.stat-card` CSS already used on the global dashboard. No new routes. No new templates. Stats are fetched synchronously in the existing handler before render.

The global dashboard (`/admin/`) is unchanged.

## Stats at Each Level

### Tenant (projects page — `/admin/tenants/{tenantID}/projects`)
- Projects count
- Buckets count (all buckets in tenant)
- Documents count (all documents in tenant)
- Users count
- API Keys count

### Project (buckets page — `/admin/tenants/{tenantID}/projects/{projectID}/buckets`)
- Buckets count (within the project)
- Documents count (across all buckets in the project)

### Bucket (documents page — `/admin/tenants/{tenantID}/projects/{projectID}/buckets/{bucketID}/documents`)
- Documents count
- Total storage size (human-readable, e.g. "1.4 GB")
- Last upload (relative time, e.g. "2h ago")

## Data Layer — New Store Methods

### `internal/auth`

```go
CountUsersByTenant(ctx context.Context, tenantID string) (int, error)
CountAPIKeysByTenant(ctx context.Context, tenantID string) (int, error)
```

Both are `SELECT COUNT(*) FROM <table> WHERE tenant_id = ? AND deleted_at IS NULL`.

### `internal/repo`

```go
CountBucketsInProject(ctx context.Context, projectID string) (int, error)
CountDocumentsInProject(ctx context.Context, projectID string) (int, error)
SumDocumentSizeInBucket(ctx context.Context, bucketID string) (int64, error)
```

- `CountBucketsInProject`: `SELECT COUNT(*) FROM buckets WHERE project_id = ? AND deleted_at IS NULL`
- `CountDocumentsInProject`: `SELECT COUNT(*) FROM documents d JOIN buckets b ON d.bucket_id = b.id WHERE b.project_id = ? AND d.deleted_at IS NULL`
- `SumDocumentSizeInBucket`: `SELECT COALESCE(SUM(size), 0) FROM documents WHERE bucket_id = ? AND deleted_at IS NULL`

## Handler Changes

### Tenant dashboard — `projectsData` struct

Add:
```go
type tenantStats struct {
    Projects  int
    Buckets   int
    Documents int
    Users     int
    APIKeys   int
}
```

The `projects` handler fetches all five counts and includes `tenantStats` in `projectsData`.

### Project dashboard — `bucketsData` struct

Add:
```go
type projectStats struct {
    Buckets   int
    Documents int
}
```

The `buckets` handler fetches both counts and includes `projectStats` in `bucketsData`.

### Bucket dashboard — `documentsData` struct

Add:
```go
type bucketSummaryStats struct {
    Documents  int
    TotalSize  string // formatted: "1.4 GB", "840 KB", etc.
    LastUpload string // relative: "2h ago", "just now", or "—" if empty
}
```

The `documents` handler fetches doc count, sum size, and the most-recent `created_at` timestamp. Size formatting and relative time are done in the handler (not in the template).

## Template Changes

Each template gains a `{{template "stats-strip" .Stats}}` (or inline block) between the breadcrumb and the main card. Uses existing CSS classes:

```html
<div class="stats-grid">
  <div class="stat-card">
    <div class="label">Projects</div>
    <div class="value">{{.Stats.Projects}}</div>
  </div>
  ...
</div>
```

No CSS changes required.

## Testing

- New store methods get unit tests in `auth/store_test.go` and `repo/` (following existing patterns).
- Existing web handler tests continue to pass (stats are additive — no behaviour change).
- No new web handler tests required; the stats strip is pure read-only data.

## Out of Scope

- Content-type breakdown on bucket dashboard (GROUP BY query — can be added later)
- Changes to the global dashboard
- Real-time or cached stats
