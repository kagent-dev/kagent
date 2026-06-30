-- name: CreateSessionShare :one
INSERT INTO session_share (token, session_id, user_id, read_only)
VALUES ($1, $2, $3, $4)
RETURNING id, token, session_id, user_id, read_only, created_at;

-- name: GetSessionShareByToken :one
SELECT id, token, session_id, user_id, read_only, created_at FROM session_share
WHERE token = $1
LIMIT 1;

-- name: ListSessionSharesBySession :many
SELECT id, token, session_id, user_id, read_only, created_at FROM session_share
WHERE session_id = $1
ORDER BY created_at DESC;

-- name: DeleteSessionShare :exec
DELETE FROM session_share
WHERE token = $1 AND session_id = $2 AND user_id = $3;

-- name: UpsertShareAccess :exec
INSERT INTO session_share_access (user_id, share_id, accessed_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id, share_id) DO UPDATE SET accessed_at = NOW();
