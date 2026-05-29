package a2amigration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kagent-dev/kagent/go/core/pkg/a2acompat/trpcv0"
)

const defaultBatchSize = 100

type Options struct {
	BatchSize int
	DryRun    bool
	Out       io.Writer
}

type Stats struct {
	TasksMigrated             int
	TasksSkipped              int
	TasksFailed               int
	PushNotificationsMigrated int
	PushNotificationsSkipped  int
	PushNotificationsFailed   int
	AlreadyV1                 int
}

func Run(ctx context.Context, db *pgxpool.Pool, opts Options) (Stats, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = defaultBatchSize
	}

	alreadyV1, err := countAlreadyV1(ctx, db)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{AlreadyV1: alreadyV1}

	if err := rejectUnknownVersions(ctx, db); err != nil {
		return stats, err
	}

	taskStats, err := migrateTable(ctx, db, tableConfig{
		name: "task",
		selectSQL: `SELECT id, data FROM task
WHERE protocol_version IS NULL AND id > $1
ORDER BY id
LIMIT $2`,
		updateSQL: `UPDATE task
SET data = $1, protocol_version = $2, updated_at = NOW()
WHERE id = $3 AND data = $4 AND protocol_version IS NULL`,
		convert: func(data string) ([]byte, error) {
			return trpcv0.TaskJSONToV1JSON([]byte(data))
		},
	}, opts)
	if err != nil {
		return stats, err
	}
	stats.TasksMigrated = taskStats.migrated
	stats.TasksSkipped = taskStats.skipped
	stats.TasksFailed = taskStats.failed

	pushStats, err := migrateTable(ctx, db, tableConfig{
		name: "push_notification",
		selectSQL: `SELECT id, data FROM push_notification
WHERE protocol_version IS NULL AND id > $1
ORDER BY id
LIMIT $2`,
		updateSQL: `UPDATE push_notification
SET data = $1, protocol_version = $2, updated_at = NOW()
WHERE id = $3 AND data = $4 AND protocol_version IS NULL`,
		convert: func(data string) ([]byte, error) {
			return trpcv0.PushNotificationJSONToV1JSON([]byte(data))
		},
	}, opts)
	if err != nil {
		return stats, err
	}
	stats.PushNotificationsMigrated = pushStats.migrated
	stats.PushNotificationsSkipped = pushStats.skipped
	stats.PushNotificationsFailed = pushStats.failed

	return stats, nil
}

type tableConfig struct {
	name      string
	selectSQL string
	updateSQL string
	convert   func(data string) ([]byte, error)
}

type tableStats struct {
	migrated int
	skipped  int
	failed   int
}

func migrateTable(ctx context.Context, db *pgxpool.Pool, cfg tableConfig, opts Options) (tableStats, error) {
	var stats tableStats
	lastID := ""

	for {
		rows, err := db.Query(ctx, cfg.selectSQL, lastID, int32(opts.BatchSize))
		if err != nil {
			return stats, fmt.Errorf("list %s rows: %w", cfg.name, err)
		}

		var batch []rowData
		for rows.Next() {
			var row rowData
			if err := rows.Scan(&row.id, &row.data); err != nil {
				rows.Close()
				return stats, fmt.Errorf("scan %s row: %w", cfg.name, err)
			}
			batch = append(batch, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return stats, fmt.Errorf("iterate %s rows: %w", cfg.name, err)
		}
		rows.Close()

		if len(batch) == 0 {
			return stats, nil
		}

		for _, row := range batch {
			lastID = row.id
			converted, err := cfg.convert(row.data)
			if err != nil {
				log.Printf("skipping %s row %s: conversion failed: %v", cfg.name, row.id, err)
				stats.failed++
				continue
			}
			if opts.DryRun {
				stats.migrated++
				continue
			}
			tag, err := db.Exec(ctx, cfg.updateSQL, string(converted), trpcv0.ProtocolVersionV1, row.id, row.data)
			if err != nil {
				return stats, fmt.Errorf("update %s row %s: %w", cfg.name, row.id, err)
			}
			if tag.RowsAffected() == 0 {
				stats.skipped++
				continue
			}
			stats.migrated++
		}
	}
}

type rowData struct {
	id   string
	data string
}

func countAlreadyV1(ctx context.Context, db *pgxpool.Pool) (int, error) {
	var count int
	err := db.QueryRow(ctx, `SELECT
	(SELECT COUNT(*) FROM task WHERE protocol_version = $1) +
	(SELECT COUNT(*) FROM push_notification WHERE protocol_version = $1)`, trpcv0.ProtocolVersionV1).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count v1 rows: %w", err)
	}
	return count, nil
}

func rejectUnknownVersions(ctx context.Context, db *pgxpool.Pool) error {
	rows, err := db.Query(ctx, `SELECT table_name, protocol_version, count FROM (
	SELECT 'task' AS table_name, protocol_version, COUNT(*) AS count
	FROM task
	WHERE protocol_version IS NOT NULL AND protocol_version <> $1
	GROUP BY protocol_version
	UNION ALL
	SELECT 'push_notification' AS table_name, protocol_version, COUNT(*) AS count
	FROM push_notification
	WHERE protocol_version IS NOT NULL AND protocol_version <> $1
	GROUP BY protocol_version
) unknown_versions
ORDER BY table_name, protocol_version`, trpcv0.ProtocolVersionV1)
	if err != nil {
		return fmt.Errorf("check protocol versions: %w", err)
	}
	defer rows.Close()

	var unknown []error
	for rows.Next() {
		var tableName, version string
		var count int
		if err := rows.Scan(&tableName, &version, &count); err != nil {
			return fmt.Errorf("scan unknown protocol version: %w", err)
		}
		unknown = append(unknown, fmt.Errorf("%s has %d row(s) with unsupported protocol_version %q", tableName, count, version))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate unknown protocol versions: %w", err)
	}
	return errors.Join(unknown...)
}
