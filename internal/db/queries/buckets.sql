-- name: CreateBucket :one
INSERT INTO buckets (id, project_id, name, description)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetBucket :one
SELECT * FROM buckets
WHERE id = ? AND deleted_at IS NULL;

-- name: GetBucketByName :one
SELECT * FROM buckets
WHERE project_id = ? AND name = ? AND deleted_at IS NULL;

-- name: ListBuckets :many
SELECT * FROM buckets
WHERE project_id = ? AND deleted_at IS NULL
ORDER BY name;

-- name: UpdateBucket :one
UPDATE buckets
SET name        = ?,
    description = ?,
    updated_at  = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteBucket :exec
UPDATE buckets
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = ?;
