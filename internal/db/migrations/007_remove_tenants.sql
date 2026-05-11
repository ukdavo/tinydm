-- +goose Up
-- 007_remove_tenants.sql
--
-- Removes multi-tenancy: drops the tenants table and tenant_id columns from
-- all dependent tables. Uses the copy-rename pattern for tables whose UNIQUE
-- constraints change. Requires SQLite 3.35+ for ALTER TABLE ... DROP COLUMN.

-- 1. Recreate projects without tenant_id (UNIQUE changes from (tenant_id,name) to (name))
CREATE TABLE projects_v2 (
    id          TEXT     NOT NULL PRIMARY KEY,
    name        TEXT     NOT NULL UNIQUE,
    description TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME
);
INSERT INTO projects_v2 (id, name, description, created_at, updated_at, deleted_at)
    SELECT id, name, description, created_at, updated_at, deleted_at FROM projects;
DROP TABLE projects;
ALTER TABLE projects_v2 RENAME TO projects;

-- 2. Recreate users without tenant_id; downgrade superadmin → admin; UNIQUE on username globally
CREATE TABLE users_v2 (
    id            TEXT     NOT NULL PRIMARY KEY,
    username      TEXT     NOT NULL UNIQUE,
    email         TEXT     NOT NULL DEFAULT '',
    password_hash TEXT     NOT NULL DEFAULT '',
    user_type     TEXT     NOT NULL DEFAULT 'user'
                           CHECK(user_type IN ('admin','user')),
    is_active     INTEGER  NOT NULL DEFAULT 1,
    first_name    TEXT     NOT NULL DEFAULT '',
    last_name     TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at    DATETIME
);
INSERT INTO users_v2 (id, username, email, password_hash, user_type, is_active,
                      first_name, last_name, created_at, updated_at, deleted_at)
    SELECT id, username, email, password_hash,
           CASE user_type WHEN 'superadmin' THEN 'admin' ELSE user_type END,
           is_active,
           COALESCE(first_name, ''), COALESCE(last_name, ''),
           created_at, updated_at, deleted_at
    FROM users;
DROP TABLE users;
ALTER TABLE users_v2 RENAME TO users;

-- 3. Recreate groups without tenant_id
CREATE TABLE groups_v2 (
    id          TEXT     NOT NULL PRIMARY KEY,
    name        TEXT     NOT NULL UNIQUE,
    description TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME
);
INSERT INTO groups_v2 (id, name, description, created_at, updated_at, deleted_at)
    SELECT id, name, description, created_at, updated_at, deleted_at FROM groups;
DROP TABLE groups;
ALTER TABLE groups_v2 RENAME TO groups;

-- Recreate group_members FK (references new groups table)
CREATE TABLE group_members_v2 (
    group_id   TEXT     NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    TEXT     NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id)
);
INSERT INTO group_members_v2 SELECT * FROM group_members;
DROP TABLE group_members;
ALTER TABLE group_members_v2 RENAME TO group_members;
CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id);

-- 4. Recreate api_keys without tenant_id
CREATE TABLE api_keys_v2 (
    id           TEXT     NOT NULL PRIMARY KEY,
    user_id      TEXT     REFERENCES users(id) ON DELETE SET NULL,
    name         TEXT     NOT NULL,
    key_hash     TEXT     NOT NULL UNIQUE,
    key_prefix   TEXT     NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   DATETIME,
    last_used_at DATETIME,
    revoked_at   DATETIME
);
INSERT INTO api_keys_v2 (id, user_id, name, key_hash, key_prefix,
                         created_at, expires_at, last_used_at, revoked_at)
    SELECT id, user_id, name, key_hash, key_prefix,
           created_at, expires_at, last_used_at, revoked_at FROM api_keys;
DROP TABLE api_keys;
ALTER TABLE api_keys_v2 RENAME TO api_keys;
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);

-- 5. Recreate rights without tenant_id; remove 'tenant' from resource_type CHECK
CREATE TABLE rights_v2 (
    id             TEXT     NOT NULL PRIMARY KEY,
    principal_type TEXT     NOT NULL CHECK(principal_type IN ('user','group','apikey')),
    principal_id   TEXT     NOT NULL,
    resource_type  TEXT     NOT NULL CHECK(resource_type IN ('project','bucket','document')),
    resource_id    TEXT     NOT NULL DEFAULT '*',
    can_create     INTEGER  NOT NULL DEFAULT 0,
    can_read       INTEGER  NOT NULL DEFAULT 0,
    can_update     INTEGER  NOT NULL DEFAULT 0,
    can_delete     INTEGER  NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(principal_type, principal_id, resource_type, resource_id)
);
INSERT INTO rights_v2 (id, principal_type, principal_id, resource_type, resource_id,
                       can_create, can_read, can_update, can_delete, created_at)
    SELECT id, principal_type, principal_id, resource_type, resource_id,
           can_create, can_read, can_update, can_delete, created_at
    FROM rights WHERE resource_type != 'tenant';
DROP TABLE rights;
ALTER TABLE rights_v2 RENAME TO rights;
CREATE INDEX IF NOT EXISTS idx_rights_principal ON rights(principal_type, principal_id);

-- 6. Drop tenant_id from audit_log (SQLite 3.35+)
ALTER TABLE audit_log DROP COLUMN tenant_id;

-- 7. Drop the tenants table
DROP TABLE IF EXISTS tenants;

-- +goose Down
SELECT 1; -- irreversible; roll forward instead
