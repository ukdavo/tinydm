# TinyDM — Project Plan

> Living document. Update status as work progresses.
> Status key: ⬜ Not started · 🔄 In progress · ✅ Done · ⏸ Blocked

---

## Phase 1 — Foundation

Core scaffolding, data models, and configuration. Nothing user-facing yet.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.1 | Initialise Go module and directory structure | ✅ | `go.mod`, all directories |
| 1.2 | Makefile / build scripts (build, test, lint) | ✅ | build, build-all, test, lint, run, sqlc, docker-build |
| 1.3 | Configuration management (env vars + config file) | ✅ | `internal/config/config.go` |
| 1.4 | Structured logging | ✅ | `log/slog` (stdlib, Go 1.21+) wired in `main.go` |
| 1.5 | Database setup — SQLite driver + migration runner | ✅ | `internal/db/db.go` — modernc SQLite + goose |
| 1.6 | Core schema — Tenant, Project, Bucket, Document | ✅ | `001_initial_schema.sql` — all tables + indexes |
| 1.7 | sqlc code generation setup | ✅ | `sqlc.yaml` + query files for all entities |
| 1.8 | Docker build (single binary image) | ✅ | Multi-stage `Dockerfile` + `docker-compose.yml` |

---

## Phase 2 — Authentication & Authorisation

Principals, user types, and rights enforcement.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 2.1 | User model (admin / user types) | ✅ | `002_auth_schema.sql` + `auth.Store` |
| 2.2 | Group model | ✅ | groups + group_members tables |
| 2.3 | API key model (stored as hashed token) | ✅ | SHA-256 hash, prefix display, expiry, revocation |
| 2.4 | Basic authentication middleware | ✅ | `auth.Authenticator` — `Authorization: Basic` + `X-Tenant-ID` |
| 2.5 | API key authentication middleware | ✅ | `auth.Authenticator` — `X-API-Key` header |
| 2.6 | JWT session issuance & validation | ✅ | `auth.NewJWT` / `ParseJWT`; login endpoint |
| 2.7 | RBAC — Create / Read / Update / Delete rights | ✅ | `auth.Can()` + rights table with wildcard support |
| 2.8 | Rights enforcement middleware | ✅ | `auth.RequireAuth`, `auth.RequireAdmin` |

---

## Phase 3 — Repository API

REST endpoints for the full tenant → project → bucket → document hierarchy.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.1 | Tenant CRUD endpoints | ✅ | `internal/api/tenants.go` |
| 3.2 | Project CRUD endpoints | ✅ | `internal/api/projects.go` |
| 3.3 | Bucket CRUD endpoints | ✅ | `internal/api/buckets.go` |
| 3.4 | Document upload endpoint | ✅ | multipart/form-data, MIME sniff, content-addressed storage |
| 3.5 | Document download endpoint | ✅ | streaming with Content-Type + Content-Disposition |
| 3.6 | Document delete endpoint | ✅ | soft delete |
| 3.7 | Document search endpoint | ✅ | `?q=` param on list endpoint |
| 3.8 | Content-addressed file storage (SHA-256 paths) | ✅ | `internal/storage/storage.go` — done in Phase 1 |
| 3.9 | Storage abstraction interface (for future S3/NFS) | ✅ | `Store` interface in `internal/storage/storage.go` |

---

## Phase 4 — Document Versioning & Metadata

Automatic versioning on update, and rich metadata support.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 4.1 | Version model — snapshot on every update | ✅ | `document_versions` table in schema |
| 4.2 | Version history endpoint | ✅ | `GET .../documents/{id}/versions` |
| 4.3 | Version restore endpoint | ✅ | `POST .../versions/{versionID}/restore` — snapshots current, applies old content |
| 4.4 | System properties (file size, MIME type, checksum) | ✅ | Already present on `Document`; all properties surfaced via `GET /properties` |
| 4.5 | Tag support (add / remove / filter) | ✅ | `GET/PUT /tags`, `POST/DELETE /tags/{tag}`, `?tag=` filter on list |
| 4.6 | Custom properties (runtime-defined key/value) | ✅ | `GET/PUT /properties`, `PUT/DELETE /properties/{key}` |
| 4.7 | Metadata extraction — EXIF (images) | ✅ | `internal/meta` — image dimensions (width/height/format) via stdlib; full EXIF deferred to backlog |
| 4.8 | Metadata extraction — other formats (Office, PDF) | ✅ | PDF version string, Office container type (OOXML vs OLE2) from magic bytes |

---

## Phase 5 — Audit Log

Immutable record of all repository events.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 5.1 | Audit event model | ✅ | `audit_log` table in schema |
| 5.2 | Audit middleware — record all mutating requests | ✅ | `internal/audit.Middleware` — async, best-effort, action name derived from route pattern |
| 5.3 | Audit query API (filter by tenant, user, date, action) | ✅ | `GET .../audit` — filters: principal, action (with `*` wildcard), resource, from, to, limit, offset |

---

## Phase 6 — Admin Web Client

Simple HTMX-based UI for administrators.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 6.1 | Go `html/template` + HTMX base layout | ✅ | `internal/web/web.go` — clone-and-parse, FuncMap, session middleware |
| 6.2 | Embed static assets in binary | ✅ | `internal/web/embed.go` — `//go:embed static templates` |
| 6.3 | Login page | ✅ | Cookie-based JWT session; POST `/admin/login`, GET `/admin/logout` |
| 6.4 | Dashboard — system overview | ✅ | Tenant/user/project/bucket/document counts + recent audit events |
| 6.5 | Tenant / project / bucket browser | ✅ | HTMX inline create/delete; breadcrumb nav; row-level partial swaps |
| 6.6 | Document list, upload, download, delete | ✅ | Multipart upload, streaming download, storage cleanup on delete |
| 6.7 | User management | ✅ | Create, activate/deactivate, delete; role badge; user-row partial |
| 6.8 | API key management | ✅ | Generate (plaintext shown once), revoke; apikey-row partial |
| 6.9 | Audit log viewer | ✅ | Filter bar (action, principal, date, limit) with HTMX live update |

---

## Phase 7 — Document & Bucket Management UI

Full CRUD, search, and document lifecycle management in the admin web UI.
The REST API already supports all of these operations; this phase exposes them visually.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 7.1 | Bucket edit — rename and update description | ✅ | Inline edit row; `GET .../edit`, `GET .../row`, `PUT .../buckets/{id}`; row-level HTMX swap |
| 7.2 | Document update — rename and/or replace content | ✅ | Edit form on document row; `PUT .../documents/{id}` with optional file re-upload; rename-only = no snapshot, content replace = snapshot |
| 7.3 | Document name search — live filter on document list | ✅ | Search input on documents page; `GET .../documents/rows?q=` HTMX partial; debounced `input delay:300ms` |
| 7.4 | Tag filter — filter document list by tag | ✅ | Tag input on documents page; `GET .../documents/rows?tag=` HTMX partial; combinable with `?q=` |
| 7.5 | Document detail page | ✅ | `GET /admin/documents/{id}` — full detail page with breadcrumb, metadata grid, tags, properties, versions |
| 7.6 | Tag management UI | ✅ | Tag chips with add form and remove button; `POST/DELETE .../tags/{tag}`; `doc-tags-inner` HTMX partial |
| 7.7 | Custom properties UI | ✅ | Key/value table with inline add and per-row delete; `POST/DELETE .../properties/{key}`; HTMX partial updates |
| 7.8 | System metadata display | ✅ | Read-only "Extracted Metadata" panel for `sys.*` properties; only shown when metadata exists |
| 7.9 | Version history and restore | ✅ | Version table on detail page; `POST .../versions/{versionID}/restore` with hx-confirm dialog |
| 7.10 | Integration test — `test_phase7.sh` | ✅ | 50+ assertions across all nine UI sections; curl + cookie-jar pattern matching `test_phase6.sh` |

---

## Phase 8 — Hardening & Release

Testing, security, packaging, and documentation.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 8.1 | Unit tests — auth, storage, metadata | ✅ | `auth/{password,token,rbac,context}_test.go`, `meta/extractor_test.go`, `storage/storage_test.go` |
| 8.2 | Integration tests — full API flows | ✅ | `api/{server,auth,tenants,documents}_test.go` — full HTTP flows via `httptest.Server` |
| 8.3 | Security review (auth, input validation, path traversal) | ✅ | 6 issues fixed; see `SECURITY.md` |
| 8.4 | Cross-platform builds (macOS, Linux, Windows) | ✅ | `make build-all` (6 targets); `make dist` for archives; CI + release workflows in `.github/workflows/` |
| 8.5 | PostgreSQL support (alternative to SQLite) | ✅ | `db.DB` wrapper with `?`→`$N` rebind; `TINYDM_DB_DRIVER=postgres` + `TINYDM_DB_DSN`; separate `migrations_pg/`; docker-compose postgres profile |
| 8.6 | API documentation (OpenAPI / Swagger) | ✅ | OpenAPI 3.1 spec embedded in binary; Swagger UI at `/api/docs`; raw spec at `/api/docs/openapi.yaml` |
| 8.7 | Deployment guide (binary, Docker, docker-compose) | ✅ | `DEPLOYMENT.md` — binary/systemd, Docker, Compose (SQLite + PostgreSQL), reverse proxy, backup, upgrade |
| 8.8 | Performance baseline testing | ⬜ | |

---

## Backlog — Future Features

Items from the spec not in scope for the initial release.

| Feature | Notes |
|---------|-------|
| Document locking | Pessimistic lock with owner + expiry |
| Explicit versioning | Named/tagged versions beyond auto-snapshots |
| Full text indexing | Likely SQLite FTS5 or external engine |
| Multiple content stores | S3, NFS — storage interface already planned |
| OAuth | SSO / social login support |
| Associations / relations | Links between documents |
| Full EXIF extraction | Requires e.g. `github.com/rwcarlsen/goexif`; hook in `internal/meta` already present |
| Office metadata (author, title, page count) | Requires OOXML/OLE2 parser; container type already detected |

---

## Decision Log

Record of key technical decisions made during the project.

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-05-07 | Language: Go | Single binary, cross-platform, small footprint, performant |
| 2026-05-07 | Framework: Chi | Lightweight, idiomatic, minimal overhead |
| 2026-05-07 | Database: SQLite default, PostgreSQL optional | Zero-dependency default; abstracted for easy swap |
| 2026-05-07 | DB access: sqlc | Type-safe SQL without ORM magic |
| 2026-05-07 | File storage: content-addressed local FS | Deduplication, simple versioning, abstracted for S3/NFS later |
| 2026-05-07 | Admin UI: HTMX + Go templates | No build step, embedded in binary, fits "simple admin" requirement |
| 2026-05-07 | Auth: bcrypt + JWT + opaque API tokens | Standard, secure, no external dependencies |
