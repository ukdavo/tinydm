-- name: GrantRights :one
INSERT INTO rights (id, principal_type, principal_id,
                    resource_type, resource_id,
                    can_create, can_read, can_update, can_delete)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(principal_type, principal_id, resource_type, resource_id)
DO UPDATE SET
    can_create = excluded.can_create,
    can_read   = excluded.can_read,
    can_update = excluded.can_update,
    can_delete = excluded.can_delete
RETURNING *;

-- name: GetRights :one
SELECT * FROM rights
WHERE principal_type = ? AND principal_id = ?
  AND resource_type  = ? AND resource_id  = ?;

-- name: ListPrincipalRights :many
SELECT * FROM rights
WHERE principal_type = ? AND principal_id = ?
ORDER BY resource_type, resource_id;

-- name: RevokeRights :exec
DELETE FROM rights
WHERE principal_type = ? AND principal_id = ?
  AND resource_type  = ? AND resource_id  = ?;

-- name: GetUserEffectiveRights :many
SELECT r.principal_type, r.principal_id, r.resource_type, r.resource_id,
       r.can_create, r.can_read, r.can_update, r.can_delete
FROM rights r
WHERE (
    (r.principal_type = 'user'  AND r.principal_id = ?)
 OR (r.principal_type = 'group' AND r.principal_id IN (
         SELECT group_id FROM group_members WHERE user_id = ?
     ))
);
