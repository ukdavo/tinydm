# TinyDM — Deployment Guide

This guide covers every supported deployment method: running directly from a pre-built binary, building from source, Docker, and Docker Compose. It also covers production hardening, reverse proxy setup, backup, and upgrades.

---

## Contents

1. [Prerequisites](#prerequisites)
2. [Running from source](#running-from-source)
3. [Pre-built binary](#pre-built-binary)
4. [Docker](#docker)
5. [Docker Compose — SQLite (default)](#docker-compose--sqlite-default)
6. [Docker Compose — PostgreSQL](#docker-compose--postgresql)
7. [Configuration reference](#configuration-reference)
8. [First-run bootstrap](#first-run-bootstrap)
9. [Production checklist](#production-checklist)
10. [Reverse proxy](#reverse-proxy)
11. [Backup and restore](#backup-and-restore)
12. [Upgrading](#upgrading)
13. [Health check](#health-check)

---

## Prerequisites

TinyDM is a single statically-linked binary with no external runtime dependencies.

| Deployment method | Requirements |
|---|---|
| Pre-built binary | None (Linux, macOS, Windows) |
| Build from source | Go 1.21 or later |
| Docker | Docker Engine 20.10 or later |
| Docker Compose | Docker Compose v2 (`docker compose`, not `docker-compose`) |

---

## Running from source

```bash
git clone https://github.com/ukdavo/tinydm.git
cd tinydm
go mod tidy

TINYDM_JWT_SECRET=$(openssl rand -hex 32) \
TINYDM_BOOTSTRAP_ADMIN_PASS=changeme \
go run ./cmd/tinydm
```

The server starts on `http://localhost:8080`. On first run the bootstrap tenant (`Default`) and admin user (`admin`) are created automatically.

---

## Pre-built binary

Download the latest release archive for your platform from the [GitHub Releases page](https://github.com/ukdavo/tinydm/releases), then extract and run:

```bash
# Linux / macOS example
tar -xzf tinydm-<version>-linux-amd64.tar.gz
chmod +x tinydm-linux-amd64

TINYDM_JWT_SECRET=$(openssl rand -hex 32) \
TINYDM_BOOTSTRAP_ADMIN_PASS=changeme \
./tinydm-linux-amd64
```

Available platform archives:

| Archive | Platform |
|---|---|
| `tinydm-<version>-linux-amd64.tar.gz` | Linux x86-64 |
| `tinydm-<version>-linux-arm64.tar.gz` | Linux ARM64 (Raspberry Pi, AWS Graviton, …) |
| `tinydm-<version>-darwin-amd64.tar.gz` | macOS Intel |
| `tinydm-<version>-darwin-arm64.tar.gz` | macOS Apple Silicon |
| `tinydm-<version>-windows-amd64.zip` | Windows x86-64 |
| `tinydm-<version>-windows-arm64.zip` | Windows ARM64 |

### Running as a systemd service (Linux)

Create `/etc/systemd/system/tinydm.service`:

```ini
[Unit]
Description=TinyDM document management server
After=network.target

[Service]
Type=simple
User=tinydm
Group=tinydm
WorkingDirectory=/opt/tinydm
ExecStart=/opt/tinydm/tinydm
Restart=on-failure
RestartSec=5

# Configuration
Environment=TINYDM_JWT_SECRET=<your-secret>
Environment=TINYDM_DB_PATH=/var/lib/tinydm/tinydm.db
Environment=TINYDM_STORAGE_PATH=/var/lib/tinydm/content
Environment=TINYDM_SECURE_COOKIES=true

# Harden the service
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/tinydm

[Install]
WantedBy=multi-user.target
```

```bash
# Create a dedicated user and data directory
sudo useradd --system --no-create-home tinydm
sudo mkdir -p /var/lib/tinydm
sudo chown tinydm:tinydm /var/lib/tinydm

# Install binary
sudo cp tinydm-linux-amd64 /opt/tinydm/tinydm

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable --now tinydm
sudo journalctl -u tinydm -f
```

---

## Docker

### Build the image

```bash
docker build -t tinydm:latest .
```

### Run with SQLite (default)

```bash
docker run -d \
  --name tinydm \
  -p 8080:8080 \
  -v tinydm-data:/data \
  -e TINYDM_JWT_SECRET=$(openssl rand -hex 32) \
  -e TINYDM_DB_PATH=/data/tinydm.db \
  -e TINYDM_STORAGE_PATH=/data/content \
  -e TINYDM_BOOTSTRAP_ADMIN_PASS=changeme \
  --restart unless-stopped \
  tinydm:latest
```

### Run with PostgreSQL

```bash
docker run -d \
  --name tinydm \
  -p 8080:8080 \
  -v tinydm-storage:/data \
  -e TINYDM_JWT_SECRET=$(openssl rand -hex 32) \
  -e TINYDM_DB_DRIVER=postgres \
  -e TINYDM_DB_DSN="host=your-pg-host user=tinydm password=secret dbname=tinydm sslmode=require" \
  -e TINYDM_STORAGE_PATH=/data/content \
  -e TINYDM_BOOTSTRAP_ADMIN_PASS=changeme \
  --restart unless-stopped \
  tinydm:latest
```

---

## Docker Compose — SQLite (default)

The default profile uses SQLite with all data persisted in a named Docker volume.

**`docker-compose.yml`** (edit `TINYDM_JWT_SECRET` before running):

```yaml
services:
  tinydm:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - tinydm-data:/data
    environment:
      TINYDM_JWT_SECRET: "change-me-before-production"
      TINYDM_DB_DRIVER: "sqlite"
      TINYDM_DB_PATH: "/data/tinydm.db"
      TINYDM_STORAGE_PATH: "/data/content"
    restart: unless-stopped

volumes:
  tinydm-data:
```

```bash
# Start
docker compose up -d

# View logs
docker compose logs -f

# Stop
docker compose down
```

### Using an environment file

Instead of embedding secrets in `docker-compose.yml`, create a `.env` file (and add it to `.gitignore`):

```bash
# .env
TINYDM_JWT_SECRET=your-very-long-random-secret
TINYDM_BOOTSTRAP_ADMIN_PASS=changeme
```

Then reference variables in `docker-compose.yml`:

```yaml
environment:
  TINYDM_JWT_SECRET: "${TINYDM_JWT_SECRET}"
  TINYDM_BOOTSTRAP_ADMIN_PASS: "${TINYDM_BOOTSTRAP_ADMIN_PASS}"
```

---

## Docker Compose — PostgreSQL

The included `docker-compose.yml` ships a `postgres` profile that starts a Postgres 16 container alongside TinyDM.

```bash
# Start TinyDM + PostgreSQL
docker compose --profile postgres up -d

# Tail logs
docker compose --profile postgres logs -f

# Stop all
docker compose --profile postgres down
```

This starts two services:

- **`postgres`** — PostgreSQL 16 (Alpine), health-checked before TinyDM starts.
- **`tinydm-pg`** — TinyDM configured with `TINYDM_DB_DRIVER=postgres` and a DSN pointing at the `postgres` service.

Both database data and file content are stored in named volumes (`postgres-data` and `tinydm-storage`).

### Using an external PostgreSQL database

If you already have a PostgreSQL instance, skip the `postgres` service and set `TINYDM_DB_DSN` directly:

```bash
docker run -d \
  --name tinydm \
  -p 8080:8080 \
  -e TINYDM_JWT_SECRET=... \
  -e TINYDM_DB_DRIVER=postgres \
  -e TINYDM_DB_DSN="host=db.example.com port=5432 user=tinydm password=secret dbname=tinydm sslmode=require" \
  tinydm:latest
```

TinyDM runs all pending migrations automatically on startup — no manual schema setup is required.

---

## Configuration reference

All configuration is via environment variables. No configuration file is required.

### Server

| Variable | Default | Description |
|---|---|---|
| `TINYDM_HOST` | `0.0.0.0` | Listen address |
| `TINYDM_PORT` | `8080` | Listen port |

### Database

| Variable | Default | Description |
|---|---|---|
| `TINYDM_DB_DRIVER` | `sqlite` | Backend: `sqlite` or `postgres` |
| `TINYDM_DB_PATH` | `tinydm.db` | SQLite database file path (when `DB_DRIVER=sqlite`) |
| `TINYDM_DB_DSN` | _(empty)_ | PostgreSQL connection string (required when `DB_DRIVER=postgres`) |

**PostgreSQL DSN format:**

```
host=<host> port=5432 user=<user> password=<pass> dbname=<db> sslmode=require
```

Or as a URL:

```
postgres://user:pass@host:5432/dbname?sslmode=require
```

### File storage

| Variable | Default | Description |
|---|---|---|
| `TINYDM_STORAGE_BACKEND` | `local` | Storage driver: `local`, `s3`, `azure`, or `gcs` |
| `TINYDM_STORAGE_PATH` | `data/content` | Directory for content-addressed file storage (used when `STORAGE_BACKEND=local`) |

**S3** (set `TINYDM_STORAGE_BACKEND=s3`):

| Variable | Default | Description |
|---|---|---|
| `TINYDM_S3_BUCKET` | _(required)_ | S3 bucket name |
| `TINYDM_S3_REGION` | `us-east-1` | S3 region |
| `TINYDM_S3_KEY_ID` | _(required)_ | AWS access key ID |
| `TINYDM_S3_SECRET` | _(required)_ | AWS secret access key |
| `TINYDM_S3_ENDPOINT` | _(empty)_ | Endpoint override — use e.g. `http://localhost:9000` for MinIO |

**Azure Blob Storage** (set `TINYDM_STORAGE_BACKEND=azure`):

| Variable | Default | Description |
|---|---|---|
| `TINYDM_AZURE_ACCOUNT` | _(required)_ | Storage account name |
| `TINYDM_AZURE_KEY` | _(required)_ | Storage account key |
| `TINYDM_AZURE_CONTAINER` | _(required)_ | Blob container name |
| `TINYDM_AZURE_ENDPOINT` | _(empty)_ | Endpoint override — use e.g. `http://localhost:10000` for Azurite |

**Google Cloud Storage** (set `TINYDM_STORAGE_BACKEND=gcs`):

| Variable | Default | Description |
|---|---|---|
| `TINYDM_GCS_BUCKET` | _(required)_ | GCS bucket name |
| `TINYDM_GCS_PROJECT` | _(empty)_ | GCP project ID |
| `TINYDM_GCS_CREDENTIALS_FILE` | _(empty)_ | Path to service account JSON; empty = Application Default Credentials |

### Authentication

| Variable | Default | Description |
|---|---|---|
| `TINYDM_JWT_SECRET` | _(required)_ | HMAC-SHA256 secret for signing JWTs. Use a long random string — minimum 32 characters. |
| `TINYDM_JWT_EXPIRY_MINUTES` | `60` | JWT lifetime in minutes |
| `TINYDM_SECURE_COOKIES` | `false` | Set `true` when serving over HTTPS to mark session cookies `Secure` |

### Bootstrap

These variables are only used on the very first startup when the database has no users. They are silently ignored on subsequent starts.

| Variable | Default | Description |
|---|---|---|
| `TINYDM_BOOTSTRAP_TENANT_ID` | `default` | ID of the initial tenant |
| `TINYDM_BOOTSTRAP_TENANT_NAME` | `Default` | Display name of the initial tenant |
| `TINYDM_BOOTSTRAP_ADMIN_USER` | `superadmin` | Username of the initial superadmin |
| `TINYDM_BOOTSTRAP_ADMIN_EMAIL` | _(empty)_ | Email of the initial admin |
| `TINYDM_BOOTSTRAP_ADMIN_PASS` | _(empty)_ | Password of the initial admin. **Bootstrap is skipped if this is not set.** |

---

## First-run bootstrap

On the very first startup, if `TINYDM_BOOTSTRAP_ADMIN_PASS` is set and the database contains no users, TinyDM:

1. Creates the bootstrap tenant (using `TINYDM_BOOTSTRAP_TENANT_ID` and `TINYDM_BOOTSTRAP_TENANT_NAME`).
2. Creates a superadmin account with the supplied credentials.
3. Creates a domain admin account for the bootstrap tenant (username: `admin@<tenant_id>`).

This is a one-time idempotent operation. To sign in via the admin UI, use:

- **Tenant name:** whatever you set for `TINYDM_BOOTSTRAP_TENANT_NAME` (default: `Default`)
- **Username:** `TINYDM_BOOTSTRAP_ADMIN_USER` (default: `superadmin`)
- **Password:** `TINYDM_BOOTSTRAP_ADMIN_PASS`

After the first login, change the admin password via the Users section of the admin UI or by creating a new admin account and revoking the original.

---

## Production checklist

### 1 — Generate a strong JWT secret

```bash
openssl rand -hex 32
# e.g. a3f2b1c8d9e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0
```

Set this as `TINYDM_JWT_SECRET`. Never commit it to version control.

### 2 — Enable secure cookies

If serving over HTTPS (which you should be in production), set:

```
TINYDM_SECURE_COOKIES=true
```

This marks session cookies with the `Secure` flag so they are never sent over plain HTTP.

### 3 — Put TinyDM behind a reverse proxy

Expose TinyDM through nginx or Caddy for TLS termination, HTTP/2, and rate limiting. See the [Reverse proxy](#reverse-proxy) section below.

### 4 — Restrict the storage path

Ensure `TINYDM_STORAGE_PATH` is on a volume with sufficient space for your expected document volume. Content-addressed storage means identical files are stored once, but plan capacity conservatively.

### 5 — Set up automated backups

See the [Backup and restore](#backup-and-restore) section. Schedule backups before any upgrade.

### 6 — Configure log forwarding

TinyDM logs to stdout as JSON. Forward logs to your log aggregator (Datadog, Loki, CloudWatch, etc.) via your container runtime or a log shipper.

### 7 — Limit external network exposure

Bind TinyDM to `127.0.0.1` if it sits behind a reverse proxy on the same host:

```
TINYDM_HOST=127.0.0.1
```

---

## Reverse proxy

### Nginx

```nginx
server {
    listen 80;
    server_name docs.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name docs.example.com;

    ssl_certificate     /etc/letsencrypt/live/docs.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/docs.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;

    # Increase for large file uploads (TinyDM enforces 512 MB internally)
    client_max_body_size 512m;

    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;

        # Allow streaming downloads without buffering
        proxy_buffering    off;
        proxy_read_timeout 300s;
    }
}
```

### Caddy

```caddyfile
docs.example.com {
    reverse_proxy 127.0.0.1:8080 {
        flush_interval -1   # streaming downloads
    }

    # Caddy handles TLS automatically via Let's Encrypt
}
```

After configuring a reverse proxy with HTTPS, remember to set `TINYDM_SECURE_COOKIES=true`.

---

## Backup and restore

### SQLite

TinyDM's SQLite database is a single file. The safest way to back it up while the server is running is the SQLite online backup command, which produces a consistent snapshot regardless of concurrent writes:

```bash
# Create a consistent backup of the live database
sqlite3 /var/lib/tinydm/tinydm.db ".backup /backups/tinydm-$(date +%Y%m%d-%H%M%S).db"
```

Alternatively, stop TinyDM and copy the file directly:

```bash
systemctl stop tinydm
cp /var/lib/tinydm/tinydm.db /backups/tinydm-$(date +%Y%m%d-%H%M%S).db
systemctl start tinydm
```

### PostgreSQL

Use `pg_dump` for consistent backups:

```bash
pg_dump -h localhost -U tinydm -Fc tinydm > /backups/tinydm-$(date +%Y%m%d-%H%M%S).pgdump
```

Restore with:

```bash
pg_restore -h localhost -U tinydm -d tinydm /backups/tinydm-20260101-120000.pgdump
```

### File content store

The content store (`TINYDM_STORAGE_PATH`) holds the actual file bytes in a SHA-256-keyed directory tree. Back it up with `rsync`:

```bash
rsync -av --delete /var/lib/tinydm/content/ /backups/content/
```

Because the store is content-addressed and append-only, `rsync` only transfers new or changed files. Backup the database and the content store together to keep them consistent.

### Backup schedule example (cron)

```cron
# Daily backup at 02:00, keeping 30 days
0 2 * * * sqlite3 /var/lib/tinydm/tinydm.db ".backup /backups/tinydm-$(date +\%Y\%m\%d).db" && find /backups -name "tinydm-*.db" -mtime +30 -delete
0 2 * * * rsync -a --delete /var/lib/tinydm/content/ /backups/content/
```

---

## Upgrading

TinyDM runs database migrations automatically on startup, so upgrades are straightforward:

### Binary / systemd

```bash
# Download the new binary
curl -L https://github.com/ukdavo/tinydm/releases/latest/download/tinydm-linux-amd64.tar.gz | tar -xz

# Replace the binary (stop first to avoid partial writes)
sudo systemctl stop tinydm
sudo cp tinydm-linux-amd64 /opt/tinydm/tinydm
sudo systemctl start tinydm

# Confirm the new version
curl http://localhost:8080/health
```

### Docker

```bash
# Pull / build the new image
docker pull tinydm:latest   # if using a registry
# or: docker build -t tinydm:latest .

# Replace the container
docker stop tinydm
docker rm tinydm
docker run -d --name tinydm ... tinydm:latest   # same run command as before
```

### Docker Compose

```bash
docker compose pull          # or: docker compose build
docker compose up -d         # recreates changed containers
```

### Notes

- **Migrations run automatically.** There is no manual `migrate` step.
- **Always back up before upgrading**, especially across minor versions.
- **Downgrading** is supported as long as no new migration files exist in the older version. If a migration was applied that the old binary doesn't know about, the old binary will fail the migration check on startup.

---

## Health check

`GET /health` returns a JSON response and requires no authentication. Use it for load balancer health checks, Docker health checks, or uptime monitoring.

```bash
curl http://localhost:8080/health
```

**Healthy response (200):**

```json
{ "status": "ok", "version": "v1.2.0", "commit": "abc1234" }
```

**Degraded response (503):** returned when the database cannot be reached.

```json
{ "status": "degraded", "version": "v1.2.0", "commit": "abc1234" }
```

### Docker health check

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1
```

### Kubernetes liveness / readiness probe

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 30
readinessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
```

---

## Clustering (Multi-Node Active-Active)

TinyDM supports running multiple nodes simultaneously against a shared PostgreSQL database and a shared object store (S3, Azure Blob, or GCS). Nodes coordinate via PostgreSQL advisory locks and a heartbeat table.

### Prerequisites

| Requirement | Notes |
|-------------|-------|
| PostgreSQL ≥ 14 | SQLite is single-node only; switch to `TINYDM_DB_DRIVER=postgres` |
| Shared object store | S3 / MinIO, Azure Blob, or GCS — all nodes must point at the same bucket/container |
| Reverse proxy | nginx (or any L7 proxy) routes requests across nodes |

### Node identity

Set a unique, stable identifier for each node:

```
TINYDM_NODE_ID=tinydm-1
```

Defaults to the system hostname if not set. Node IDs are recorded in the `cluster_nodes` table with a heartbeat timestamp updated every 5 seconds.

### Environment variables (cluster-specific)

| Variable | Default | Description |
|----------|---------|-------------|
| `TINYDM_NODE_ID` | hostname | Stable node identifier shown in `/health` responses and `cluster_nodes` |
| `TINYDM_DB_DRIVER` | `sqlite` | Set to `postgres` for multi-node |
| `TINYDM_DB_DSN` | — | PostgreSQL connection string (required when driver is `postgres`) |

All storage backends are supported. See [Storage Backends](#storage-backends) for the relevant environment variables.

### Quick start with docker-compose

The repo includes `docker-compose.cluster.yml` which starts 3 TinyDM nodes, PostgreSQL, MinIO, and nginx:

```bash
# First-time only: bootstrap the admin account on node 1.
BOOTSTRAP_PASS=yourpassword docker compose -f docker-compose.cluster.yml up --build

# Subsequent starts (bootstrap vars are ignored once users exist).
docker compose -f docker-compose.cluster.yml up
```

The cluster is reachable at `http://localhost:80`. The MinIO console is at `http://localhost:9001`.

**⚠ Change `TINYDM_JWT_SECRET` before deploying to production.** The default value in the compose file is a placeholder.

### nginx upstream configuration

`nginx.cluster.conf` (also in the repo) configures `least_conn` load balancing with automatic failover:

```nginx
upstream tinydm {
    least_conn;
    server tinydm-1:8080 fail_timeout=10s max_fails=3;
    server tinydm-2:8080 fail_timeout=10s max_fails=3;
    server tinydm-3:8080 fail_timeout=10s max_fails=3;
    keepalive 32;
}
```

TinyDM tokens are stateless JWTs, so no sticky sessions are required.

### Leader election

One node at a time holds the PostgreSQL advisory lock `leaderLockID`. The current leader is identified in the `/health` response. The leader role migrates automatically when a node stops — the advisory lock is released when the database connection closes, and another node picks it up within one heartbeat interval (≤ 5 s).

### Enhanced `/health` response (cluster mode)

```json
{
  "status": "ok",
  "version": "v1.2.0",
  "commit": "abc1234",
  "node_id": "tinydm-2",
  "db": "ok",
  "storage": "ok"
}
```

HTTP 503 is returned if either `db` or `storage` reports `"error"`, allowing the load balancer to route around degraded nodes.

### Rolling upgrade procedure

1. Pull the new image / build the new binary.
2. Take one node out of the nginx upstream (or let `max_fails` handle it naturally).
3. Stop the node, deploy the new version, start it — migrations run automatically on startup.
4. Verify `/health` returns `"status": "ok"` on the upgraded node.
5. Repeat for each remaining node.

Zero-downtime is achieved because the database schema is always backward-compatible (additive migrations only) and the shared object store requires no migration.
