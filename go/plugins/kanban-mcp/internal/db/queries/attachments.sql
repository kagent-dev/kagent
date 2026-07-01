-- name: AddAttachment :one
INSERT INTO kanban.attachment (task_id, type, filename, url, title, content)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM kanban.attachment
WHERE id = $1;

-- name: ListAttachments :many
SELECT * FROM kanban.attachment
WHERE task_id = $1
ORDER BY created_at;

-- name: DeleteAttachment :exec
DELETE FROM kanban.attachment
WHERE id = $1;

-- name: GetTaskAttribute :one
SELECT * FROM kanban.attachment
WHERE task_id = $1 AND type = 'attribute' AND title = $2;

-- name: SetAttachmentContent :one
UPDATE kanban.attachment
SET content = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;
