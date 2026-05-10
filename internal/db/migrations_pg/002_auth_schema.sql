-- +goose Up

-- ─── Users ────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS users (
    id            TEXT        NOT NULL PRIMARY KEY,
    tenant_id     TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    username      TEXT        NOT NULL,
    email         TEXT        NOT NULL DEFAULT '',
    password_hash TEXT        NOT NULL DEFAULT '',
    user_type     TEXT        NOT NULL DEFAULT 'user'
                              CHECK(user_type IN ('admin','user')),
    is_active     INTEGER     NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at    TIMESTAMPTZ,
    UNIQUE(tenant_id, username),
    UNIQUE(tenant_id, email)
);

CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);

-- ─── Groups ───────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS groups (
    id          TEXT        NOT NULL PRIMARY KEY,
    tenant_id   TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMPTZ,
    UNIQUE(tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_groups_tenant ON groups(tenant_id);

-- ─── Group membership ─────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS group_members (
    group_id   TEXT        NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    TEXT        NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id);

-- ─── API keys ─────────────────────────────────────────────────────────────────
-- The raw key is never stored; only a SHA-256 hex digest is persisted.

CREATE TABLE IF NOT EXISTS api_keys (
    id           TEXT        NOT NULL PRIMARY KEY,
    tenant_id    TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    -- Optional: key tied to a specific user; NULL = tenant-scoped admin key.
    user_id      TEXT        REFERENCES users(id) ON DELETE SET NULL,
    name         TEXT        NOT NULL,
    key_hash     TEXT        NOT NULL UNIQUE,  -- SHA-256(plaintext_key)
    key_prefix   TEXT        NOT NULL,         -- first 12 chars for display ("tdm_xxxxxxxx")
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant   ON api_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);

-- ─── RBAC rights ──────────────────────────────────────────────────────────────
-- Rights can be granted to a user or group on a specific resource, or
-- on all resources of a type (resource_id = '*').

CREATE TABLE IF NOT EXISTS rights (
    id             TEXT        NOT NULL PRIMARY KEY,
    tenant_id      TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    principal_type TEXT        NOT NULL CHECK(principal_type IN ('user','group')),
    principal_id   TEXT        NOT NULL,
    -- resource_type: 'tenant' | 'project' | 'bucket'
    resource_type  TEXT        NOT NULL CHECK(resource_type IN ('tenant','project','bucket')),
    -- resource_id: specific resource UUID, or '*' for all resources of that type
    resource_id    TEXT        NOT NULL DEFAULT '*',
    can_create     INTEGER     NOT NULL DEFAULT 0,
    can_read       INTEGER     NOT NULL DEFAULT 0,
    can_update     INTEGER     NOT NULL DEFAULT 0,
    can_delete     INTEGER     NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(principal_type, principal_id, resource_type, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_rights_principal ON rights(principal_type, principal_id);
CREATE INDEX IF NOT EXISTS idx_rights_tenant    ON rights(tenant_id);

-- +goose Down

DROP TABLE IF EXISTS rights;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS users;
