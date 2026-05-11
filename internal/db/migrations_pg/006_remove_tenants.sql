-- +goose Up
-- 006_remove_tenants.sql (PostgreSQL)
--
-- Removes multi-tenancy: drops the tenants table and tenant_id columns.
-- PostgreSQL supports ALTER TABLE ... DROP COLUMN and DROP CONSTRAINT directly.

-- 1. Projects: drop tenant_id, replace unique constraint
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_tenant_id_name_key;
DROP INDEX IF EXISTS idx_projects_tenant;
ALTER TABLE projects DROP COLUMN tenant_id;
ALTER TABLE projects ADD CONSTRAINT projects_name_key UNIQUE (name);

-- 2. Users: drop tenant_id, downgrade superadmin → admin, replace unique constraints
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tenant_id_username_key;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tenant_id_email_key;
DROP INDEX IF EXISTS idx_users_tenant;
ALTER TABLE users DROP COLUMN tenant_id;
UPDATE users SET user_type = 'admin' WHERE user_type = 'superadmin';
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_user_type_check;
ALTER TABLE users ADD CONSTRAINT users_user_type_check CHECK (user_type IN ('admin','user'));
ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE (username);

-- 3. Groups: drop tenant_id, replace unique constraint
ALTER TABLE groups DROP CONSTRAINT IF EXISTS groups_tenant_id_name_key;
DROP INDEX IF EXISTS idx_groups_tenant;
ALTER TABLE groups DROP COLUMN tenant_id;
ALTER TABLE groups ADD CONSTRAINT groups_name_key UNIQUE (name);

-- 4. API keys: drop tenant_id
DROP INDEX IF EXISTS idx_api_keys_tenant;
ALTER TABLE api_keys DROP COLUMN tenant_id;

-- 5. Rights: drop tenant_id, remove 'tenant' resource_type, tighten CHECK
DROP INDEX IF EXISTS idx_rights_tenant;
ALTER TABLE rights DROP COLUMN tenant_id;
DELETE FROM rights WHERE resource_type = 'tenant';
ALTER TABLE rights DROP CONSTRAINT IF EXISTS rights_resource_type_check;
ALTER TABLE rights ADD CONSTRAINT rights_resource_type_check
    CHECK (resource_type IN ('project','bucket','document'));

-- 6. Audit log: drop tenant_id
DROP INDEX IF EXISTS idx_audit_tenant;
ALTER TABLE audit_log DROP COLUMN tenant_id;

-- 7. Drop tenants table (no FKs remain)
DROP TABLE IF EXISTS tenants;

-- +goose Down
SELECT 1; -- irreversible; roll forward instead
