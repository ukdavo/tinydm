# TinyDM

A simple, self-hosted document management system. Small footprint, easy to deploy, no external dependencies.

> **Status:** All phases complete. The full REST API, document versioning, tags, custom properties, automatic metadata extraction, an immutable audit log, an HTMX admin web UI, OpenAPI 3.1 documentation, and a performance benchmark suite are all working. SQLite is the default database; PostgreSQL is available as an alternative backend. See [DEPLOYMENT.md](./DEPLOYMENT.md) for production deployment instructions and [BENCHMARKS.md](./BENCHMARKS.md) for performance baselines.

---

## Features

- **Multi-tenant hierarchy** — Tenant → Project → Bucket → Document
- **Three authentication methods** — JWT, HTTP Basic, API key
- **Role-based access control** — admin / user roles with per-resource rights
- **Document versioning** — automatic snapshot on every update; restore to any previous version
- **Tags** — add, remove, or filter documents by free-form tags
- **Custom properties** — runtime-defined key/value metadata per document
- **Automatic metadata extraction** — image dimensions (JPEG, PNG, GIF), PDF version string, Office container type (OOXML / OLE2) detected on upload
- **Immutable audit log** — every mutating request recorded async; queryable by action (with `*` wildcard), principal, resource, and date range
- **Pagination** — all REST list endpoints return a `{"data":[…], "pagination":{…}}` envelope; use `?limit=` and `?offset=` to page through large result sets; the web UI renders prev/next pager bars on every list page
- **Admin web UI** — HTMX-powered interface at `/admin/` covering tenants, projects, buckets, documents, users, API keys, and audit log; all assets embedded in the binary
- **Document & bucket management UI** — inline bucket rename, document update, name search, tag filter, tag management, custom properties panel, system metadata display, version history and one-click restore
- **Content-addressed storage** — SHA-256 keyed files; identical content is stored once
- **OpenAPI 3.1 documentation** — Swagger UI at `/api/docs`, raw spec at `/api/docs/openapi.yaml`; both embedded in the binary
- **Structured JSON logging**, health endpoint, graceful shutdown
- Single binary · Docker · docker-compose

---

## Tech stack

| Concern | Choice |
|---|---|
| Language | Go 1.21+ |
| HTTP router | [Chi](https://github.com/go-chi/chi) |
| Database | SQLite (default, no CGO) via [modernc/sqlite](https://gitlab.com/cznic/sqlite) · PostgreSQL via [pgx/v5](https://github.com/jackc/pgx) |
| Migrations | [Goose](https://github.com/pressly/goose) (embedded SQL files, per-driver sets) |
| Auth | bcrypt · HS256 JWT · SHA-256 API keys |
| Metadata | stdlib `image/*` packages (zero extra deps) |
| Admin UI | HTMX 2 + Go `html/template` — embedded in binary |
| Packaging | Single binary · Docker · docker-compose (SQLite or PostgreSQL profile) |

---

## Quick start

### Prerequisites

- Go 1.21 or later

### Run from source

```bash
git clone https://github.com/ukdavo/tinydm.git
cd tinydm
go mod tidy

TINYDM_JWT_SECRET=your-secret-here \
TINYDM_BOOTSTRAP_ADMIN_PASS=changeme \
go run ./cmd/tinydm
```

The server starts on `http://localhost:8080`. On first run a default tenant (`default`) and an admin user (`admin`) are created automatically.

Open `http://localhost:8080/admin/` in a browser to reach the admin UI. Sign in with tenant name `Default`, username `admin`, and the password you set above.

### Run with Docker

```bash
docker build -t tinydm .

docker run --rm \
  -p 8080:8080 \
  -v $(pwd)/data:/data \
  -e TINYDM_JWT_SECRET=your-secret-here \
  -e TINYDM_BOOTSTRAP_ADMIN_PASS=changeme \
  tinydm
```

Or with Docker Compose:

```bash
# SQLite (default)
docker compose up

# PostgreSQL (starts postgres + tinydm-pg services)
docker compose --profile postgres up
```

---

## Admin web UI

Navigate to `http://localhost:8080/admin/` after starting the server.

| Section | What you can do |
|---|---|
| Dashboard | System-wide counts and recent audit events |
| Tenants | Create and delete tenants; drill into projects |
| Projects | Create and delete projects within a tenant |
| Buckets | Create and delete buckets within a project |
| Documents | Upload, download, and delete files; truncated checksum shown |
| Users | Create users, set role (admin / user), activate / deactivate, delete |
| API Keys | Generate keys (plaintext shown once), revoke |
| Audit Log | Filter by action (supports `*` wildcard), principal, and date range |

All table edits use HTMX inline row swaps — no full page reloads.

---

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `TINYDM_HOST` | `0.0.0.0` | Listen address |
| `TINYDM_PORT` | `8080` | Listen port |
| `TINYDM_DB_DRIVER` | `sqlite` | Database backend: `sqlite` or `postgres` |
| `TINYDM_DB_PATH` | `tinydm.db` | SQLite database file path (used when `DB_DRIVER=sqlite`) |
| `TINYDM_DB_DSN` | _(empty)_ | PostgreSQL connection string (required when `DB_DRIVER=postgres`) e.g. `host=localhost user=tinydm dbname=tinydm sslmode=disable` |
| `TINYDM_STORAGE_PATH` | `data/content` | Directory for stored file content |
| `TINYDM_JWT_SECRET` | _(required)_ | Secret used to sign JWTs — use a long random string in production |
| `TINYDM_JWT_EXPIRY_MINUTES` | `60` | JWT lifetime in minutes |
| `TINYDM_SECURE_COOKIES` | `false` | Set `true` when serving over HTTPS to mark session cookies Secure |
| `TINYDM_BOOTSTRAP_TENANT_ID` | `default` | Tenant ID created on first run |
| `TINYDM_BOOTSTRAP_TENANT_NAME` | `Default` | Tenant display name created on first run |
| `TINYDM_BOOTSTRAP_ADMIN_USER` | `admin` | Admin username created on first run |
| `TINYDM_BOOTSTRAP_ADMIN_EMAIL` | _(empty)_ | Admin email created on first run |
| `TINYDM_BOOTSTRAP_ADMIN_PASS` | _(empty)_ | Admin password on first run — **bootstrap is skipped if unset** |

### PostgreSQL

Switch backends by setting two variables:

```bash
TINYDM_DB_DRIVER=postgres \
TINYDM_DB_DSN="host=localhost user=tinydm password=tinydm dbname=tinydm sslmode=disable" \
TINYDM_JWT_SECRET=your-secret-here \
go run ./cmd/tinydm
```

Both drivers are compiled into every binary. No rebuild is required to switch. Migrations run automatically on startup from an embedded, driver-specific SQL set (`migrations/` for SQLite, `migrations_pg/` for PostgreSQL).

### Bootstrap

On the very first startup, if `TINYDM_BOOTSTRAP_ADMIN_PASS` is set and the database contains no users, TinyDM will:

1. Create the bootstrap tenant (using `TINYDM_BOOTSTRAP_TENANT_ID`)
2. Create an admin user with the supplied credentials

This is a one-time operation — subsequent starts skip it silently.

---

## API

All endpoints except `/health`, `POST /api/v1/auth/login`, and the `/admin/` web UI pages require API authentication.

### Authentication

TinyDM supports three methods on every protected endpoint.

**JWT (recommended)**

```bash
# 1. Obtain a token
curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"default","username":"admin","password":"changeme"}'

# 2. Use the token
curl http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Bearer <token>"
```

**Basic auth**

```bash
curl http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Basic $(echo -n 'admin:changeme' | base64)" \
  -H "X-Tenant-ID: default"
```

**API key**

```bash
curl http://localhost:8080/api/v1/auth/me \
  -H "X-API-Key: tdm_<your-api-key>"
```

---

### Pagination

All list endpoints (`GET` requests that return collections) support offset-based pagination via query parameters and always return a JSON envelope:

```json
{
  "data": [ … ],
  "pagination": {
    "total":    142,
    "limit":    50,
    "offset":   0,
    "has_more": true
  }
}
```

| Parameter | Default | Max | Description |
|---|---|---|---|
| `limit` | `50` | `500` | Number of items to return |
| `offset` | `0` | — | Number of items to skip |

`pagination.total` always reflects the full unfiltered count for the current query, regardless of `limit`. `has_more` is `true` when `offset + limit < total`.

**Example — walking through all documents in a bucket:**

```bash
# Page 1
curl "$BASE/documents?limit=20&offset=0"

# Page 2
curl "$BASE/documents?limit=20&offset=20"

# Page 3
curl "$BASE/documents?limit=20&offset=40"
```

**Example — last page detection:**

```bash
resp=$(curl "$BASE/documents?limit=20&offset=40")
has_more=$(echo "$resp" | jq '.pagination.has_more')
# false → you are on the last page
```

Paginated endpoints: tenants, projects, buckets, documents (including `?q=` search and `?tag=` filter), document versions, users, API keys, and audit events.

Tags and custom properties are not paginated — they return bare arrays/objects because the number of tags or properties per document is inherently small.

---

### Endpoints

#### API documentation

Interactive documentation is embedded in the binary — no separate tool required.

| URL | Description |
|---|---|
| `/api/docs` | Swagger UI (OpenAPI 3.1 interactive explorer) |
| `/api/docs/openapi.yaml` | Raw OpenAPI 3.1 spec (YAML) |

#### System

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/health` | None | Liveness / readiness check — returns `{"status":"ok"}` |
| `POST` | `/api/v1/auth/login` | None | Exchange credentials for a JWT — body: `{"tenant_id":"…","username":"…","password":"…"}` |
| `GET` | `/api/v1/auth/me` | Required | Returns the authenticated principal (ID, tenant, role) |

#### Tenants _(admin only for write operations)_

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/tenants` | List all tenants _(paginated)_ |
| `POST` | `/api/v1/tenants` | Create a tenant |
| `GET` | `/api/v1/tenants/{tenantID}` | Get a tenant |
| `PUT` | `/api/v1/tenants/{tenantID}` | Update a tenant |
| `DELETE` | `/api/v1/tenants/{tenantID}` | Soft-delete a tenant |

#### Projects _(admin only for write operations)_

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/tenants/{tenantID}/projects` | List projects _(paginated)_ |
| `POST` | `/api/v1/tenants/{tenantID}/projects` | Create a project |
| `GET` | `…/projects/{projectID}` | Get a project |
| `PUT` | `…/projects/{projectID}` | Update a project |
| `DELETE` | `…/projects/{projectID}` | Soft-delete a project |

#### Buckets _(admin only for write operations)_

| Method | Path | Description |
|---|---|---|
| `GET` | `…/projects/{projectID}/buckets` | List buckets _(paginated)_ |
| `POST` | `…/projects/{projectID}/buckets` | Create a bucket |
| `GET` | `…/buckets/{bucketID}` | Get a bucket |
| `PUT` | `…/buckets/{bucketID}` | Update a bucket |
| `DELETE` | `…/buckets/{bucketID}` | Soft-delete a bucket |

#### Documents

| Method | Path | Description |
|---|---|---|
| `GET` | `…/buckets/{bucketID}/documents` | List documents _(paginated)_. Supports `?q=` (name search) and `?tag=` (tag filter); both combinable with `?limit=`/`?offset=` |
| `POST` | `…/buckets/{bucketID}/documents` | Upload a document (`multipart/form-data`, field `file`; optional field `name`) |
| `GET` | `…/documents/{documentID}` | Get document metadata |
| `PUT` | `…/documents/{documentID}` | Update name and/or replace content (snapshots current version first) |
| `DELETE` | `…/documents/{documentID}` | Soft-delete a document |
| `GET` | `…/documents/{documentID}/content` | Download the raw file |

#### Versions

| Method | Path | Description |
|---|---|---|
| `GET` | `…/documents/{documentID}/versions` | List all version snapshots, newest first _(paginated)_ |
| `POST` | `…/documents/{documentID}/versions/{versionID}/restore` | Restore a previous version (snapshots current state first) |

#### Users & API keys _(admin only)_

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/tenants/{tenantID}/users` | List users _(paginated)_. Password hashes are never returned. |
| `POST` | `/api/v1/tenants/{tenantID}/users` | Create a user — body: `{"username":"…","email":"…","password":"…","user_type":"admin\|user"}` |
| `PUT` | `/api/v1/tenants/{tenantID}/users/{userID}/active` | Activate or deactivate a user — body: `{"active":true}` |
| `DELETE` | `/api/v1/tenants/{tenantID}/users/{userID}` | Delete a user |
| `GET` | `/api/v1/tenants/{tenantID}/apikeys` | List API keys _(paginated)_. Hashes and full key values are never returned; only `key_prefix` is exposed. |
| `POST` | `/api/v1/tenants/{tenantID}/apikeys` | Generate an API key — plaintext returned once only; body: `{"name":"…","expires_at":"…"}` |
| `DELETE` | `/api/v1/tenants/{tenantID}/apikeys/{keyID}` | Revoke an API key |

#### Tags

| Method | Path | Description |
|---|---|---|
| `GET` | `…/documents/{documentID}/tags` | List tags (sorted) |
| `PUT` | `…/documents/{documentID}/tags` | Replace all tags — body: `{"tags":["a","b"]}` |
| `POST` | `…/documents/{documentID}/tags/{tag}` | Add a single tag (idempotent) |
| `DELETE` | `…/documents/{documentID}/tags/{tag}` | Remove a single tag |

#### Properties

Custom key/value metadata. Keys prefixed with `sys.` are reserved for system use.

| Method | Path | Description |
|---|---|---|
| `GET` | `…/documents/{documentID}/properties` | Get all properties as a JSON object |
| `PUT` | `…/documents/{documentID}/properties` | Replace all user-defined properties — body: `{"key":"value", …}` |
| `PUT` | `…/documents/{documentID}/properties/{key}` | Upsert a single property — body: `{"value":"…"}` |
| `DELETE` | `…/documents/{documentID}/properties/{key}` | Delete a single property |

#### Audit log

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/tenants/{tenantID}/audit` | Required | Query audit events _(paginated)_. Filters: `action` (supports `*` wildcard), `principal`, `resource`, `from`, `to`. Paging: `limit` (default 50, max 500), `offset` |

---

### Automatic metadata extraction

When a document is uploaded or its content is replaced, TinyDM automatically extracts metadata and stores it as document properties:

| Format | Extracted properties |
|---|---|
| JPEG, PNG, GIF | `image.width`, `image.height`, `image.format` |
| PDF | `pdf.version` |
| OOXML (`.docx`, `.xlsx`, `.pptx`) | `office.container` = `ooxml` |
| Legacy Office (`.doc`, `.xls`, `.ppt`) | `office.container` = `ole2` |

These properties are visible via `GET …/documents/{id}/properties` and can be combined with user-defined properties.

---

## Development

### Make targets

```bash
make build        # compile for the current platform
make build-all    # cross-compile (Linux, macOS, Windows — amd64 + arm64)
make dist         # build all platforms and package into compressed archives
make run          # go run ./cmd/tinydm
make test         # go test ./... -race -timeout 60s
make bench        # go test ./... -bench=. -benchmem -benchtime=3s (no unit tests)
make lint         # golangci-lint run
make sqlc         # regenerate DB code from SQL queries (requires sqlc)
make docker-build # build Docker image
make docker-run   # run latest Docker image with a local data volume
make clean        # remove bin/ and local *.db files
```

### Integration tests

Shell scripts cover the full stack end-to-end. Use `run_tests.sh` to run all suites in one pass, or invoke individual scripts:

```bash
./run.sh &        # start the server
sleep 2

./run_tests.sh    # run all suites; prints a grand pass/fail summary

# Or run individual suites:
./test_phase4.sh        # versioning, tags, properties, metadata extraction
./test_phase5.sh        # audit log recording, filtering, access control
./test_phase6.sh        # admin web UI — login, dashboard, CRUD, audit log viewer
./test_phase7.sh        # document & bucket management UI, search, tag filter, versions
./test_pagination.sh    # REST API pagination — envelope shape, limit/offset, has_more,
                        #   out-of-bounds offsets, default limits, search+filter envelopes
```

All scripts accept an optional `BASE_URL` argument (default `http://localhost:8080`) and forward `TINYDM_ADMIN_USER` / `TINYDM_ADMIN_PASS` environment variables. They require `curl` and `python3`. Each request is printed to stderr with method, path, and HTTP status as it runs.

### Project structure

```
tinydm/
├── cmd/tinydm/         Entry point (main.go)
├── internal/
│   ├── api/            HTTP handlers, response helpers, route registration, security middleware
│   ├── audit/          Audit log store + recording middleware
│   ├── auth/           Authentication, JWT, API keys, RBAC middleware
│   ├── config/         Environment-variable configuration
│   ├── db/
│   │   ├── migrations/    Goose SQL migration files (SQLite)
│   │   ├── migrations_pg/ Goose SQL migration files (PostgreSQL)
│   │   └── queries/       sqlc SQL query files
│   ├── meta/           Automatic metadata extraction (image, PDF, Office)
│   ├── repo/           Repository store — CRUD for all domain types
│   ├── storage/        Content-addressed file storage abstraction
│   └── web/            HTMX admin UI — handler, templates, static assets
│       ├── static/     Embedded CSS
│       └── templates/  Embedded HTML templates (base layout + 8 pages)
├── run_tests.sh        Run all integration test suites in one pass
├── test_phase4.sh      Phase 4 integration tests — versioning, tags, properties, metadata
├── test_phase5.sh      Phase 5 integration tests — audit log
├── test_phase6.sh      Phase 6 integration tests — admin web UI
├── test_phase7.sh      Phase 7 integration tests — document & bucket management UI
├── test_pagination.sh  Pagination integration tests — envelope shape, limit/offset, has_more
├── Dockerfile
├── docker-compose.yml  SQLite (default) + postgres profile
├── Makefile
├── BENCHMARKS.md       Benchmark methodology, how to run, baseline results template
├── DEPLOYMENT.md       Production deployment guide (binary, Docker, Compose, nginx, backup)
├── PLAN.md             Living project plan with per-task status
└── SPEC.md             Full project specification
```

### Generating DB code

The `internal/db/gen` package is generated from the SQL query files by [sqlc](https://sqlc.dev). After modifying any file under `internal/db/`, regenerate with:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
make sqlc
```

---

## Roadmap

See [PLAN.md](./PLAN.md) for the full task-level breakdown, [DEPLOYMENT.md](./DEPLOYMENT.md) for production deployment instructions, and [BENCHMARKS.md](./BENCHMARKS.md) for performance baseline results.

| Phase | Scope | Status |
|---|---|---|
| 1 | Foundation — scaffold, DB, storage, config | ✅ Done |
| 2 | Authentication & authorisation | ✅ Done |
| 3 | Repository API — tenant / project / bucket / document CRUD | ✅ Done |
| 4 | Document versioning, tags, custom properties, metadata extraction | ✅ Done |
| 5 | Audit log | ✅ Done |
| 6 | Admin web UI (HTMX) | ✅ Done |
| 7 | Document & bucket management UI — search, tags, properties, versions; REST + web UI pagination | ✅ Done |
| 8 | Hardening — unit & integration tests, security review, cross-platform builds, PostgreSQL, OpenAPI docs, deployment guide, performance benchmarks | ✅ Done |
| — | Document locking — pessimistic lock with owner + expiry | ⬜ Backlog |
| — | Full-text search — SQLite FTS5 or external engine | ⬜ Backlog |
| — | Cloud storage backends — S3, NFS (storage interface already abstracted) | ⬜ Backlog |
| — | OAuth / SSO — social login support | ⬜ Backlog |

---

## Licence

MIT
