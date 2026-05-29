-- name: CountAlreadyV1Rows :one
SELECT
    (SELECT COUNT(*) FROM task WHERE task.protocol_version = $1) +
    (SELECT COUNT(*) FROM push_notification WHERE push_notification.protocol_version = $1)
AS count;

-- name: ListUnknownProtocolVersions :many
SELECT table_name, protocol_version, row_count FROM (
    SELECT 'task' AS table_name, task.protocol_version, COUNT(*) AS row_count
    FROM task
    WHERE task.protocol_version IS NOT NULL AND task.protocol_version <> $1
    GROUP BY task.protocol_version
    UNION ALL
    SELECT 'push_notification' AS table_name, push_notification.protocol_version, COUNT(*) AS row_count
    FROM push_notification
    WHERE push_notification.protocol_version IS NOT NULL AND push_notification.protocol_version <> $1
    GROUP BY push_notification.protocol_version
) unknown_versions
ORDER BY table_name, protocol_version;
