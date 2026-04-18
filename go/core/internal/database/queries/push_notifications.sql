-- name: GetPushNotification :one
SELECT * FROM push_notification
WHERE task_id = $1 AND id = $2 AND deleted_at IS NULL
LIMIT 1;

-- name: ListPushNotifications :many
SELECT * FROM push_notification
WHERE task_id = $1 AND deleted_at IS NULL
ORDER BY created_at ASC;

-- name: UpsertPushNotification :exec
INSERT INTO push_notification (id, task_id, data, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    data       = EXCLUDED.data,
    updated_at = NOW();

-- name: SoftDeletePushNotification :exec
UPDATE push_notification SET deleted_at = NOW()
WHERE task_id = $1 AND deleted_at IS NULL;
