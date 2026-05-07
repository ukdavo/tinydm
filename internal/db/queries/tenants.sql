-- name: CreateTenant :one
INSERT INTO tenants (id, name, description)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetTenant :one
SELECT * FROM tenants
WHERE id = ? AND deleted_at IS NULL;

-- name: GetTenantByName :one
SELECT * FROM tenants
WHERE name = ? AND deleted_at IS NULL;

-- name: ListTenants :many
SELECT * FROM tenants
WHERE deleted_at IS NULL
ORDER BY name;

-- name: UpdateTenant :one
UPDATE tenants
SET name        = ?,
    description = ?,
    updated_at  = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteTenant :exec
UPDATE tenants
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = ?;
