# TinyDM Cluster Setup

TinyDM runs as an **active-active cluster**: every node serves API and web traffic equally, with coordination handled entirely through PostgreSQL advisory locks and a shared heartbeat table. No inter-node communication is required.

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| PostgreSQL ≥ 14 | SQLite is single-node only |
| Shared object store | All nodes must point at the same S3/MinIO/Azure Blob/GCS bucket |
| Reverse proxy | nginx (or any L7 proxy) with health-check support |

---

## How it works

- Each node registers itself in the `cluster_nodes` table with a heartbeat every 5 seconds.
- One node at a time holds a PostgreSQL advisory lock and is designated **leader** (visible in `/health`). The leader role is informational; all nodes handle requests identically.
- If a node crashes or disconnects, PostgreSQL releases its advisory lock automatically. Another node picks up the leader role within one heartbeat interval (≤ 5 s).
- Distributed document locks (for concurrent writes) are also PostgreSQL advisory locks, keyed by document ID.

---

## Step 1 — Shared PostgreSQL database

All nodes connect to the same PostgreSQL instance. Set these on every node:

```bash
TINYDM_DB_DRIVER=postgres
TINYDM_DB_DSN="host=db.example.com port=5432 user=tinydm password=secret dbname=tinydm sslmode=require"
```

Migrations run automatically on startup. Only the first node to start after a new deployment applies pending migrations; subsequent nodes wait until the schema is ready.

---

## Step 2 — Shared object store

Pick one backend and set the same credentials on every node. Examples:

**S3 / MinIO**

```bash
TINYDM_STORAGE_BACKEND=s3
TINYDM_S3_BUCKET=tinydm-docs
TINYDM_S3_REGION=us-east-1
TINYDM_S3_ACCESS_KEY=...
TINYDM_S3_SECRET_KEY=...
# MinIO only:
TINYDM_S3_ENDPOINT=http://minio:9000
TINYDM_S3_FORCE_PATH_STYLE=true
```

**Azure Blob**

```bash
TINYDM_STORAGE_BACKEND=azure
TINYDM_AZURE_CONTAINER=tinydm-docs
TINYDM_AZURE_CONNECTION_STRING=...
```

**GCS**

```bash
TINYDM_STORAGE_BACKEND=gcs
TINYDM_GCS_BUCKET=tinydm-docs
TINYDM_GCS_CREDENTIALS_FILE=/run/secrets/gcs-creds.json
```

---

## Step 3 — Node identity and JWT secret

Set a **unique, stable** ID on each node:

```bash
TINYDM_NODE_ID=tinydm-1   # tinydm-2, tinydm-3, …
```

Defaults to the system hostname if unset. The node ID appears in `/health` responses and the `cluster_nodes` table.

The **JWT secret must be identical on every node** — tokens issued by one node must be valid on all others:

```bash
TINYDM_JWT_SECRET=<long-random-string>
```

---

## Step 4 — Load balancer (nginx)

TinyDM tokens are stateless JWTs, so no sticky sessions are needed. Use `least_conn` with health checks:

```nginx
upstream tinydm {
    least_conn;
    server tinydm-1:8080 fail_timeout=10s max_fails=3;
    server tinydm-2:8080 fail_timeout=10s max_fails=3;
    server tinydm-3:8080 fail_timeout=10s max_fails=3;
    keepalive 32;
}

server {
    listen 80;

    location / {
        proxy_pass http://tinydm;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Configure your load balancer's health check to `GET /health` — TinyDM returns HTTP 503 if the database or storage is unreachable, so degraded nodes are removed from rotation automatically.

---

## Quick start with docker-compose

The repo includes `docker-compose.cluster.yml` (3 nodes + PostgreSQL + MinIO + nginx):

```bash
# First run: bootstrap the admin account
BOOTSTRAP_PASS=yourpassword docker compose -f docker-compose.cluster.yml up --build

# Subsequent runs
docker compose -f docker-compose.cluster.yml up
```

The cluster is available at `http://localhost:80`. The MinIO console is at `http://localhost:9001`.

> **Change `TINYDM_JWT_SECRET` before deploying to production.** The compose file uses a placeholder value.

---

## Verifying the cluster

Check the `/health` endpoint on each node:

```bash
curl http://tinydm-1:8080/health
```

```json
{
  "status": "ok",
  "version": "v1.2.0",
  "node_id": "tinydm-1",
  "db": "ok",
  "storage": "ok"
}
```

Query the `cluster_nodes` table to see all registered nodes and the current leader:

```sql
SELECT node_id, last_heartbeat, is_leader FROM cluster_nodes ORDER BY node_id;
```

---

## Rolling upgrades (zero downtime)

1. Pull the new image or build the new binary on one node.
2. Remove the node from the load balancer upstream (or let `max_fails` handle it).
3. Stop the node, deploy the new version, start it. Migrations run automatically.
4. Confirm `GET /health` returns `"status": "ok"`.
5. Repeat for each remaining node.

Schema migrations are additive only, so old and new versions of TinyDM can run against the same database simultaneously during the rollout.

---

## Scaling

Add nodes by starting additional TinyDM processes with the same database and storage credentials and a unique `TINYDM_NODE_ID`. Register the new node in the nginx upstream and reload nginx. No other configuration is required.

Remove nodes by stopping the process and removing it from the upstream. PostgreSQL releases the advisory lock within seconds and the remaining nodes continue serving traffic uninterrupted.
