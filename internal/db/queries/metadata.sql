-- Tags

-- name: AddTag :exec
INSERT OR IGNORE INTO document_tags (document_id, tag) VALUES (?, ?);

-- name: RemoveTag :exec
DELETE FROM document_tags WHERE document_id = ? AND tag = ?;

-- name: ListTags :many
SELECT tag FROM document_tags WHERE document_id = ? ORDER BY tag;

-- name: ListDocumentsByTag :many
SELECT d.* FROM documents d
JOIN document_tags t ON t.document_id = d.id
WHERE t.tag = ? AND d.deleted_at IS NULL
ORDER BY d.name;

-- Custom properties

-- name: SetProperty :exec
INSERT INTO document_properties (document_id, key, value)
VALUES (?, ?, ?)
ON CONFLICT(document_id, key) DO UPDATE SET value = excluded.value;

-- name: GetProperty :one
SELECT value FROM document_properties WHERE document_id = ? AND key = ?;

-- name: ListProperties :many
SELECT key, value FROM document_properties WHERE document_id = ? ORDER BY key;

-- name: DeleteProperty :exec
DELETE FROM document_properties WHERE document_id = ? AND key = ?;
