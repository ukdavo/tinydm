-- name: CreateDocument :one
INSERT INTO documents (id, bucket_id, name, content_type, size, checksum, storage_key, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetDocument :one
SELECT * FROM documents
WHERE id = ? AND deleted_at IS NULL;

-- name: GetDocumentByName :one
SELECT * FROM documents
WHERE bucket_id = ? AND name = ? AND deleted_at IS NULL;

-- name: ListDocuments :many
SELECT * FROM documents
WHERE bucket_id = ? AND deleted_at IS NULL
ORDER BY name;

-- name: UpdateDocument :one
UPDATE documents
SET name         = ?,
    content_type = ?,
    size         = ?,
    checksum     = ?,
    storage_key  = ?,
    version      = version + 1,
    updated_at   = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteDocument :exec
UPDATE documents
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SearchDocuments :many
SELECT * FROM documents
WHERE bucket_id = ?
  AND name LIKE ?
  AND deleted_at IS NULL
ORDER BY name;

-- name: CreateDocumentVersion :one
INSERT INTO document_versions (id, document_id, version, content_type, size, checksum, storage_key, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListDocumentVersions :many
SELECT * FROM document_versions
WHERE document_id = ?
ORDER BY version DESC;

-- name: GetDocumentVersion :one
SELECT * FROM document_versions
WHERE document_id = ? AND version = ?;
