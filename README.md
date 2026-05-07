# TinyDM

A simple, self-hosted document management system. Small footprint, easy to deploy, no external dependencies.

> **Status:** Active development — Phase 2 of 7 complete. Core auth is working; the document API is coming in Phase 3.

---

## Features

**Available now**
- Multi-tenant repository structure: Tenant → Project → Bucket → Document
- JWT authentication (login endpoint)
- Basic authentication (`Authorization: Basic` + `X-Tenant-ID`)
- API key authentication (`X-API-Key`)
- Role-based access control (admin / user, per-resource rights)
- Automatic database migrations on startup
- Content-addressed local file storage (SHA-256, deduplication built-in)
- Health endpoint
- Structured JSON logging

**Coming soon**
- Document upload, download, search, delete
- Automatic document versioning
- Rich metadata — system properties, tags, custom key/value pairs
- Metadata extraction (EXIF, Office, PDF)
- Full audit log
- Simple admin web UI (HTMX)
- OpenAPI documentation

---

## Tech stack

| Concern | Choice |
|---|---|
| Language | Go 1.21+ |
| HTTP router | [Chi](https://github.com/go-chi/chi) |
| Database | SQLite (default) via [modernc/sqlite](https://gitlab.com/cznic/sqlite) — no CGO |
| Migrations | [Goose](https://github.com/pressly/goose) (embedded SQL files) |
| Auth | bcrypt · HS256 JWT · SHA-256 API keys |
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

The server starts on `http://localhost:8080`. On first run, a default tenant and admin user are created automatically (see [Bootstrap](#bootstrap)).

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
| `TINYDM_JWT_SECRET` | _(required)_ | Secret used to sign JWTs — set a long random string in production |
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

### Authentication

TinyDM supports three authentication methods on every protected endpoint.

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

### Endpoints

#### Auth

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/auth/login` | None | Exchange credentials for a JWT |
| `GET` | `/api/v1/auth/me` | Required | Returns the current principal |

#### System

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/health` | None | Liveness / readiness check |

> More endpoints (tenants, projects, buckets, documents) are added in Phase 3.

---

## Development

### Useful make targets

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

### Project structure

```
tinydm/
├── cmd/tinydm/         Entry point (main.go)
├── internal/
│   ├── api/            HTTP handlers and response helpers
│   ├── auth/           Authentication, JWT, API keys, RBAC middleware
│   ├── config/         Environment-variable configuration
│   ├── db/
│   │   ├── migrations/ Goose SQL migration files
│   │   ├── queries/    sqlc SQL query files
│   │   └── gen/        Generated Go code (run: make sqlc)
│   └── storage/        Content-addressed file storage abstraction
├── web/
│   ├── static/         Embedded static assets (CSS, JS)
│   └── templates/      Go HTML templates
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── PLAN.md             Living project plan with per-task status
└── SPEC.md             Full project specification
```

### Generating DB code

The `internal/db/gen` package is generated from the SQL query files by [sqlc](https://sqlc.dev). After modifying any file in `internal/db/queries/` or `internal/db/migrations/`, regenerate with:

```bash
# Install sqlc (once)
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
| 3 | Repository API — tenant/project/bucket/document CRUD | 🔄 Next |
| 4 | Document versioning & metadata | ⬜ |
| 5 | Audit log | ⬜ |
| 6 | Admin web UI | ⬜ |
| 7 | Hardening, tests, release | ⬜ |

---

## Licence

MIT
