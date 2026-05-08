# TinyDM

A simple, self-hosted document management system. Small footprint, easy to deploy, no external dependencies.

> **Status:** Phases 1–4 complete. Authentication, the full repository API, document versioning, tags, custom properties, and automatic metadata extraction are all working.

---

## Features

- **Multi-tenant hierarchy** — Tenant → Project → Bucket → Document
- **Three authentication methods** — JWT, HTTP Basic, API key
- **Role-based access control** — admin / user roles with per-resource rights
- **Document versioning** — automatic snapshot on every update; restore to any previous version
- **Tags** — add, remove, or filter documents by free-form tags
- **Custom properties** — runtime-defined key/value metadata per document
- **Automatic metadata extraction** — image dimensions (JPEG, PNG, GIF), PDF version string, Office container type (OOXML / OLE2) detected on upload
- **Content-addressed storage** — SHA-256 keyed files; identical content is stored once
- **Structured JSON logging**, health endpoint, graceful shutdown
- Single binary · Docker · docker-compose

**Coming in later phases**
- Audit log with filterable query API (Phase 5)
- HTMX admin web UI (Phase 6)
- Unit + integration tests, OpenAPI docs, PostgreSQL support (Phase 7)

---

## Tech stack

| Concern | Choice |
|---|---|
| Language | Go 1.21+ |
| HTTP router | [Chi](https://github.com/go-chi/chi) |
| Database | SQLite (default) via [modernc/sqlite](https://gitlab.com/cznic/sqlite) — no CGO |
| Migrations | [Goose](https://github.com/pressly/goose) (embedded SQL files) |
| Auth | bcrypt · HS256 JWT · SHA-256 API keys |
| Metadata | stdlib `image/*` packages (zero extra deps) |
| Admin UI | HTMX + Go `html/template` _(Phase 6)_ |
| Packaging | Single binary · Docker |

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

Or use the helper script (loads `.env` automatically):

```bash
cp .env.example .env   # edit JWT_SECRET and admin password
./run.sh
```

The server starts on `http://localhost:8080`. On first run a default tenant and admin user are created automatically (see [Bootstrap](#bootstrap)).

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
# Edit docker-compose.yml to set TINYDM_JWT_SECRET
docker compose up
```

---

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `TINYDM_HOST` | `0.0.0.0` | Listen address |
| `TINYDM_PORT` | `8080` | Listen port |
| `TINYDM_DB_PATH` | `tinydm.db` | SQLite database file path |
| `TINYDM_STORAGE_PATH` | `data/content` | Directory for stored file content |
| `TINYDM_JWT_SECRET` | _(required)_ | Secret used to sign JWTs — use a long random string in production |
| `TINYDM_JWT_EXPIRY_MINUTES` | `60` | JWT lifetime in minutes |
| `TINYDM_BOOTSTRAP_TENANT_ID` | `default` | Tenant ID created on first run |
| `TINYDM_BOOTSTRAP_TENANT_NAME` | `Default` | Tenant display name created on first run |
| `TINYDM_BOOTSTRAP_ADMIN_USER` | `admin` | Admin username created on first run |
| `TINYDM_BOOTSTRAP_ADMIN_EMAIL` | _(empty)_ | Admin email created on first run |
| `TINYDM_BOOTSTRAP_ADMIN_PASS` | _(empty)_ | Admin password on first run — **bootstrap is skipped if unset** |

### Bootstrap

On the very first startup, if `TINYDM_BOOTSTRAP_ADMIN_PASS` is set and the database contains no users, TinyDM will:

1. Create the bootstrap tenant (using `TINYDM_BOOTSTRAP_TENANT_ID`)
2. Create an admin user with the supplied credentials

This is a one-time operation — subsequent starts skip it silently.

---

## API

All endpoints except `/health` and `POST /api/v1/auth/login` require authentication.

### Authentication

TinyDM supports three methods on every protected endpoint.

**JWT (recommended)**

```bash
# 1. Obtain a token
curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"changeme"}'

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

### Endpoints

#### System

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/health` | None | Liveness / readiness check |
| `POST` | `/api/v1/auth/login` | None | Exchange credentials for a JWT |
| `GET` | `/api/v1/auth/me` | Required | Returns the current principal |

#### Tenants _(admin only for write operations)_

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/tenants` | List all tenants |
| `POST` | `/api/v1/tenants` | Create a tenant |
| `GET` | `/api/v1/tenants/{tenantID}` | Get a tenant |
| `PUT` | `/api/v1/tenants/{tenantID}` | Update a tenant |
| `DELETE` | `/api/v1/tenants/{tenantID}` | Soft-delete a tenant |

#### Projects _(admin only for write operations)_

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/tenants/{tenantID}/projects` | List projects |
| `POST` | `/api/v1/tenants/{tenantID}/projects` | Create a project |
| `GET` | `…/projects/{projectID}` | Get a project |
| `PUT` | `…/projects/{projectID}` | Update a project |
| `DELETE` | `…/projects/{projectID}` | Soft-delete a project |

#### Buckets _(admin only for write operations)_

| Method | Path | Description |
|---|---|---|
| `GET` | `…/projects/{projectID}/buckets` | List buckets |
| `POST` | `…/projects/{projectID}/buckets` | Create a bucket |
| `GET` | `…/buckets/{bucketID}` | Get a bucket |
| `PUT` | `…/buckets/{bucketID}` | Update a bucket |
| `DELETE` | `…/buckets/{bucketID}` | Soft-delete a bucket |

#### Documents

| Method | Path | Description |
|---|---|---|
| `GET` | `…/buckets/{bucketID}/documents` | List documents. Supports `?q=` (name search) and `?tag=` (tag filter) |
| `POST` | `…/buckets/{bucketID}/documents` | Upload a document (`multipart/form-data`, field `file`; optional field `name`) |
| `GET` | `…/documents/{documentID}` | Get document metadata |
| `PUT` | `…/documents/{documentID}` | Update name and/or replace content (snapshots current version first) |
| `DELETE` | `…/documents/{documentID}` | Soft-delete a document |
| `GET` | `…/documents/{documentID}/content` | Download the raw file |

#### Versions

| Method | Path | Description |
|---|---|---|
| `GET` | `…/documents/{documentID}/versions` | List all version snapshots (newest first) |
| `POST` | `…/documents/{documentID}/versions/{versionID}/restore` | Restore a previous version (snapshots current state first) |

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
make run          # go run ./cmd/tinydm
make test         # go test ./... -race
make lint         # golangci-lint run
make sqlc         # regenerate DB code from SQL queries (requires sqlc)
make docker-build # build Docker image
make clean        # remove bin/ and local *.db files
```

### Integration tests

A shell script covers the Phase 4 features end-to-end. Start the server first, then run:

```bash
./run.sh &
sleep 2
./test_phase4.sh
```

Requires `curl` and `python3`. Cleans up all created test data on completion.

### Project structure

```
tinydm/
├── cmd/tinydm/         Entry point (main.go)
├── internal/
│   ├── api/            HTTP handlers, response helpers, route registration
│   ├── auth/           Authentication, JWT, API keys, RBAC middleware
│   ├── config/         Environment-variable configuration
│   ├── db/
│   │   ├── migrations/ Goose SQL migration files
│   │   └── queries/    sqlc SQL query files
│   ├── meta/           Automatic metadata extraction (image, PDF, Office)
│   ├── repo/           Repository store — CRUD for all domain types
│   └── storage/        Content-addressed file storage abstraction
├── test_phase4.sh      Phase 4 integration test script
├── Dockerfile
├── docker-compose.yml
├── Makefile
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

See [PLAN.md](./PLAN.md) for the full task-level breakdown.

| Phase | Scope | Status |
|---|---|---|
| 1 | Foundation — scaffold, DB, storage, config | ✅ Done |
| 2 | Authentication & authorisation | ✅ Done |
| 3 | Repository API — tenant / project / bucket / document CRUD | ✅ Done |
| 4 | Document versioning, tags, custom properties, metadata extraction | ✅ Done |
| 5 | Audit log | ⬜ Next |
| 6 | Admin web UI (HTMX) | ⬜ |
| 7 | Hardening, tests, OpenAPI, release | ⬜ |

---

## Licence

MIT
