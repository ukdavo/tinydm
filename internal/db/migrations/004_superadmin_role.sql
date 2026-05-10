-- +goose Up
-- 004_superadmin_role.sql
--
-- Extends the user_type CHECK constraint to include the new 'superadmin' role.
-- SQLite does not support ALTER TABLE ... DROP CONSTRAINT, so we recreate the
-- users table with the updated constraint using the standard SQLite pattern:
--   1. Create the new table with the updated schema.
--   2. Copy all existing rows.
--   3. Drop the old table.
--   4. Rename the new table.
--
-- All indexes, foreign keys, and defaults are preserved.

CREATE TABLE users_v2 (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    tenant_id     TEXT    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    username      TEXT    NOT NULL,
    email         TEXT    NOT NULL DEFAULT '',
    password_hash TEXT    NOT NULL,
    user_type     TEXT    NOT NULL DEFAULT 'user'
                          CHECK(user_type IN ('admin','user','superadmin')),
    is_active     INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at    TEXT,
    UNIQUE (tenant_id, username)
);

INSERT INTO users_v2 SELECT * FROM users;

DROP TABLE users;

ALTER TABLE users_v2 RENAME TO users;
