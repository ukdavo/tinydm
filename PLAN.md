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
| 2.1 | User model (admin / user types) | ⬜ | |
| 2.2 | Group model | ⬜ | |
| 2.3 | API key model (stored as hashed token) | ⬜ | |
| 2.4 | Basic authentication middleware | ⬜ | |
| 2.5 | API key authentication middleware | ⬜ | |
| 2.6 | JWT session issuance & validation | ⬜ | |
| 2.7 | RBAC — Create / Read / Update / Delete rights | ⬜ | |
| 2.8 | Rights enforcement middleware | ⬜ | |

---

## Phase 3 — Repository API

REST endpoints for the full tenant → project → bucket → document hierarchy.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.1 | Tenant CRUD endpoints | ⬜ | |
| 3.2 | Project CRUD endpoints | ⬜ | |
| 3.3 | Bucket CRUD endpoints | ⬜ | |
| 3.4 | Document upload endpoint | ⬜ | |
| 3.5 | Document download endpoint | ⬜ | |
| 3.6 | Document delete endpoint | ⬜ | |
| 3.7 | Document search endpoint | ⬜ | |
| 3.8 | Content-addressed file storage (SHA-256 paths) | ✅ | `internal/storage/storage.go` — done in Phase 1 |
| 3.9 | Storage abstraction interface (for future S3/NFS) | ✅ | `Store` interface in `internal/storage/storage.go` |

---

## Phase 4 — Document Versioning & Metadata

Automatic versioning on update, and rich metadata support.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 4.1 | Version model — snapshot on every update | ✅ | `document_versions` table in schema |
| 4.2 | Version history endpoint | ⬜ | |
| 4.3 | Version restore endpoint | ⬜ | |
| 4.4 | System properties (file size, MIME type, checksum) | ⬜ | |
| 4.5 | Tag support (add / remove / filter) | ⬜ | |
| 4.6 | Custom properties (runtime-defined key/value) | ⬜ | |
| 4.7 | Metadata extraction — EXIF (images) | ⬜ | |
| 4.8 | Metadata extraction — other formats (Office, PDF) | ⬜ | |

---

## Phase 5 — Audit Log

Immutable record of all repository events.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 5.1 | Audit event model | ✅ | `audit_log` table in schema |
| 5.2 | Audit middleware — record all mutating requests | ⬜ | |
| 5.3 | Audit query API (filter by tenant, user, date, action) | ⬜ | |

---

## Phase 6 — Admin Web Client

Simple HTMX-based UI for administrators.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 6.1 | Go `html/template` + HTMX base layout | ⬜ | |
| 6.2 | Embed static assets in binary | ⬜ | |
| 6.3 | Login page | ⬜ | |
| 6.4 | Dashboard — system overview | ⬜ | |
| 6.5 | Tenant / project / bucket browser | ⬜ | |
| 6.6 | Document list, upload, download, delete | ⬜ | |
| 6.7 | User & group management | ⬜ | |
| 6.8 | API key management | ⬜ | |
| 6.9 | Audit log viewer | ⬜ | |

---

## Phase 7 — Hardening & Release

Testing, security, packaging, and documentation.

| # | Task | Status | Notes |
|---|------|--------|-------|
| 7.1 | Unit tests — auth, storage, metadata | ⬜ | |
| 7.2 | Integration tests — full API flows | ⬜ | |
| 7.3 | Security review (auth, input validation, path traversal) | ⬜ | |
| 7.4 | Cross-platform builds (macOS, Linux, Windows) | ⬜ | |
| 7.5 | PostgreSQL support (alternative to SQLite) | ⬜ | |
| 7.6 | API documentation (OpenAPI / Swagger) | ⬜ | |
| 7.7 | Deployment guide (binary, Docker, docker-compose) | ⬜ | |
| 7.8 | Performance baseline testing | ⬜ | |

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
