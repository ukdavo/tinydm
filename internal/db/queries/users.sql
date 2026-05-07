-- name: CreateUser :one
INSERT INTO users (id, tenant_id, username, email, password_hash, user_type)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = ? AND deleted_at IS NULL;

-- name: GetUserByUsername :one
SELECT * FROM users
WHERE tenant_id = ? AND username = ? AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE tenant_id = ? AND email = ? AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE tenant_id = ? AND deleted_at IS NULL
ORDER BY username;

-- name: UpdateUser :one
UPDATE users
SET username   = ?,
    email      = ?,
    user_type  = ?,
    is_active  = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = ?,
    updated_at    = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SoftDeleteUser :exec
UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) FROM users WHERE deleted_at IS NULL;
