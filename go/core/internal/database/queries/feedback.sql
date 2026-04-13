-- name: InsertFeedback :exec
INSERT INTO feedback (user_id, message_id, is_positive, feedback_text, issue_type, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW());

-- name: ListFeedback :many
SELECT * FROM feedback
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at ASC;
