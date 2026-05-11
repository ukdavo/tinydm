-- name: CreateProject :one
INSERT INTO projects (id, name, description)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects
WHERE id = ? AND deleted_at IS NULL;

-- name: GetProjectByName :one
SELECT * FROM projects
WHERE name = ? AND deleted_at IS NULL;

-- name: ListProjects :many
SELECT * FROM projects
WHERE deleted_at IS NULL
ORDER BY name;

-- name: UpdateProject :one
UPDATE projects
SET name        = ?,
    description = ?,
    updated_at  = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteProject :exec
UPDATE projects
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = ?;
