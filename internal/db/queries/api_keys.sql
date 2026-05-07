-- name: CreateAPIKey :one
INSERT INTO api_keys (id, tenant_id, user_id, name, key_hash, key_prefix, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAPIKey :one
SELECT * FROM api_keys WHERE id = ?;

-- name: GetAPIKeyByHash :one
SELECT * FROM api_keys WHERE key_hash = ?;

-- name: ListAPIKeys :many
SELECT * FROM api_keys
WHERE tenant_id = ? AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: RevokeAPIKey :exec
UPDATE api_keys SET revoked_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?;
