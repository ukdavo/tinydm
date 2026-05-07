-- name: CreateGroup :one
INSERT INTO groups (id, tenant_id, name, description)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = ? AND deleted_at IS NULL;

-- name: GetGroupByName :one
SELECT * FROM groups
WHERE tenant_id = ? AND name = ? AND deleted_at IS NULL;

-- name: ListGroups :many
SELECT * FROM groups
WHERE tenant_id = ? AND deleted_at IS NULL
ORDER BY name;

-- name: UpdateGroup :one
UPDATE groups
SET name        = ?,
    description = ?,
    updated_at  = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteGroup :exec
UPDATE groups SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: AddGroupMember :exec
INSERT OR IGNORE INTO group_members (group_id, user_id) VALUES (?, ?);

-- name: RemoveGroupMember :exec
DELETE FROM group_members WHERE group_id = ? AND user_id = ?;

-- name: ListGroupMembers :many
SELECT u.* FROM users u
JOIN group_members m ON m.user_id = u.id
WHERE m.group_id = ? AND u.deleted_at IS NULL
ORDER BY u.username;

-- name: ListUserGroups :many
SELECT g.* FROM groups g
JOIN group_members m ON m.group_id = g.id
WHERE m.user_id = ? AND g.deleted_at IS NULL
ORDER BY g.name;
