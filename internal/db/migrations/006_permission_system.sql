-- +goose Up
-- 006_permission_system.sql
--
-- 1. Adds perm_mode to tenants (explicit | open | inherit).
-- 2. Widens rights.principal_type to include 'apikey'.
-- 3. Widens rights.resource_type to include 'document'.
--
-- SQLite does not support ALTER TABLE ... DROP/MODIFY CONSTRAINT, so the
-- rights table is recreated using the standard copy-rename pattern.

-- 1. Extend tenants with perm_mode.
ALTER TABLE tenants ADD COLUMN perm_mode TEXT NOT NULL DEFAULT 'explicit'
    CHECK(perm_mode IN ('explicit','open','inherit'));

-- 2. Recreate rights with widened CHECK constraints.
CREATE TABLE rights_v2 (
    id             TEXT    NOT NULL PRIMARY KEY,
    tenant_id      TEXT    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    principal_type TEXT    NOT NULL CHECK(principal_type IN ('user','group','apikey')),
    principal_id   TEXT    NOT NULL,
    resource_type  TEXT    NOT NULL CHECK(resource_type IN ('tenant','project','bucket','document')),
    resource_id    TEXT    NOT NULL DEFAULT '*',
    can_create     INTEGER NOT NULL DEFAULT 0,
    can_read       INTEGER NOT NULL DEFAULT 0,
    can_update     INTEGER NOT NULL DEFAULT 0,
    can_delete     INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(principal_type, principal_id, resource_type, resource_id)
);

INSERT INTO rights_v2 SELECT * FROM rights;

DROP TABLE rights;
ALTER TABLE rights_v2 RENAME TO rights;

CREATE INDEX IF NOT EXISTS idx_rights_principal ON rights(principal_type, principal_id);
CREATE INDEX IF NOT EXISTS idx_rights_tenant    ON rights(tenant_id);

-- +goose Down
-- Note: perm_mode column removal and rights table narrowing are not supported
-- in SQLite without a full recreate. This down migration is intentionally omitted
-- to avoid data loss; roll forward instead.
SELECT 1;
