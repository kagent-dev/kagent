-- name: GetPushNotification :one
SELECT * FROM push_notification
WHERE task_id = $1 AND id = $2 AND deleted_at IS NULL
LIMIT 1;

-- name: ListPushNotifications :many
SELECT * FROM push_notification
WHERE task_id = $1 AND deleted_at IS NULL
ORDER BY created_at ASC;

-- name: UpsertPushNotification :exec
INSERT INTO push_notification (id, task_id, data, protocol_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    data             = EXCLUDED.data,
    protocol_version = EXCLUDED.protocol_version,
    updated_at       = NOW();

-- name: SoftDeletePushNotification :exec
UPDATE push_notification SET deleted_at = NOW()
WHERE task_id = $1 AND deleted_at IS NULL;

-- name: ListLegacyPushNotifications :many
SELECT id, data FROM push_notification
WHERE protocol_version IS NULL AND id > $1
ORDER BY id
LIMIT $2;

-- name: MigratePushNotification :execresult
UPDATE push_notification
SET data = $1, protocol_version = $2, updated_at = NOW()
WHERE id = $3 AND data = $4 AND protocol_version IS NULL;
