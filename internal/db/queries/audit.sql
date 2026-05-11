-- name: CreateAuditEvent :exec
INSERT INTO audit_log (id, principal, action, resource, detail)
VALUES (?, ?, ?, ?, ?);

-- name: ListAuditEvents :many
SELECT * FROM audit_log
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListAuditEventsByPrincipal :many
SELECT * FROM audit_log
WHERE principal = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListAuditEventsByAction :many
SELECT * FROM audit_log
WHERE action = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;
