-- +goose Up

-- ─── Tenants ──────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS tenants (
    id          TEXT    NOT NULL PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    description TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME
);

-- ─── Projects ─────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT    NOT NULL PRIMARY KEY,
    tenant_id   TEXT    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME,
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_projects_tenant ON projects(tenant_id);

-- ─── Buckets ──────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS buckets (
    id          TEXT    NOT NULL PRIMARY KEY,
    project_id  TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME,
    UNIQUE (project_id, name)
);

CREATE INDEX IF NOT EXISTS idx_buckets_project ON buckets(project_id);

-- ─── Documents ────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS documents (
    id           TEXT    NOT NULL PRIMARY KEY,
    bucket_id    TEXT    NOT NULL REFERENCES buckets(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    content_type TEXT    NOT NULL DEFAULT 'application/octet-stream',
    size         INTEGER NOT NULL DEFAULT 0,
    -- SHA-256 hex digest of the file content
    checksum     TEXT    NOT NULL DEFAULT '',
    -- Content-addressed key within the storage backend (e.g. "ab/cdef1234...")
    storage_key  TEXT    NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1,
    created_by   TEXT    NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at   DATETIME,
    UNIQUE (bucket_id, name)
);

CREATE INDEX IF NOT EXISTS idx_documents_bucket   ON documents(bucket_id);
CREATE INDEX IF NOT EXISTS idx_documents_checksum ON documents(checksum);

-- ─── Document versions ────────────────────────────────────────────────────────
-- Snapshot written on every update before the document row is modified.

CREATE TABLE IF NOT EXISTS document_versions (
    id           TEXT    NOT NULL PRIMARY KEY,
    document_id  TEXT    NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    version      INTEGER NOT NULL,
    content_type TEXT    NOT NULL DEFAULT 'application/octet-stream',
    size         INTEGER NOT NULL DEFAULT 0,
    checksum     TEXT    NOT NULL DEFAULT '',
    storage_key  TEXT    NOT NULL,
    created_by   TEXT    NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_doc_versions_document ON document_versions(document_id);

-- ─── Document metadata ────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS document_tags (
    document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    tag         TEXT NOT NULL,
    PRIMARY KEY (document_id, tag)
);

CREATE TABLE IF NOT EXISTS document_properties (
    document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (document_id, key)
);

-- ─── Audit log ────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS audit_log (
    id          TEXT     NOT NULL PRIMARY KEY,
    tenant_id   TEXT     NOT NULL,
    principal   TEXT     NOT NULL DEFAULT '',   -- user / api-key identifier
    action      TEXT     NOT NULL,              -- e.g. "document.create"
    resource    TEXT     NOT NULL DEFAULT '',   -- ID of affected resource
    detail      TEXT     NOT NULL DEFAULT '',   -- JSON payload (optional)
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_tenant    ON audit_log(tenant_id);
CREATE INDEX IF NOT EXISTS idx_audit_principal ON audit_log(principal);
CREATE INDEX IF NOT EXISTS idx_audit_created   ON audit_log(created_at);

-- +goose Down

DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS document_properties;
DROP TABLE IF EXISTS document_tags;
DROP TABLE IF EXISTS document_versions;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS buckets;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS tenants;
