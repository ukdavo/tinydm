-- name: CreateAuditEvent :exec
INSERT INTO audit_log (id, tenant_id, principal, action, resource, detail)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListAuditEvents :many
SELECT * FROM audit_log
WHERE tenant_id = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListAuditEventsByPrincipal :many
SELECT * FROM audit_log
WHERE tenant_id = ? AND principal = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListAuditEventsByAction :many
SELECT * FROM audit_log
WHERE tenant_id = ? AND action = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;
