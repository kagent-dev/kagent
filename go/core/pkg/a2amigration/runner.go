package a2amigration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/gen"
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

	q := dbgen.New(db)
	v1 := trpcv0.ProtocolVersionV1
	limit := int32(opts.BatchSize)

	alreadyV1, err := q.CountAlreadyV1Rows(ctx, &v1)
	if err != nil {
		return Stats{}, fmt.Errorf("count v1 rows: %w", err)
	}
	stats := Stats{AlreadyV1: int(alreadyV1)}

	if err := rejectUnknownVersions(ctx, q, &v1); err != nil {
		return stats, err
	}

	taskStats, err := migrateTable(ctx, tableConfig{
		name: "task",
		list: func(ctx context.Context, lastID string) ([]rowData, error) {
			rows, err := q.ListLegacyTasks(ctx, dbgen.ListLegacyTasksParams{ID: lastID, Limit: limit})
			if err != nil {
				return nil, err
			}
			result := make([]rowData, len(rows))
			for i, r := range rows {
				result[i] = rowData{id: r.ID, data: r.Data}
			}
			return result, nil
		},
		update: func(ctx context.Context, newData, oldData, id string) (pgconn.CommandTag, error) {
			return q.MigrateTask(ctx, dbgen.MigrateTaskParams{
				Data:            newData,
				ProtocolVersion: &v1,
				ID:              id,
				Data_2:          oldData,
			})
		},
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

	pushStats, err := migrateTable(ctx, tableConfig{
		name: "push_notification",
		list: func(ctx context.Context, lastID string) ([]rowData, error) {
			rows, err := q.ListLegacyPushNotifications(ctx, dbgen.ListLegacyPushNotificationsParams{ID: lastID, Limit: limit})
			if err != nil {
				return nil, err
			}
			result := make([]rowData, len(rows))
			for i, r := range rows {
				result[i] = rowData{id: r.ID, data: r.Data}
			}
			return result, nil
		},
		update: func(ctx context.Context, newData, oldData, id string) (pgconn.CommandTag, error) {
			return q.MigratePushNotification(ctx, dbgen.MigratePushNotificationParams{
				Data:            newData,
				ProtocolVersion: &v1,
				ID:              id,
				Data_2:          oldData,
			})
		},
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
	name    string
	list    func(ctx context.Context, lastID string) ([]rowData, error)
	update  func(ctx context.Context, newData, oldData, id string) (pgconn.CommandTag, error)
	convert func(data string) ([]byte, error)
}

type tableStats struct {
	migrated int
	skipped  int
	failed   int
}

func migrateTable(ctx context.Context, cfg tableConfig, opts Options) (tableStats, error) {
	var stats tableStats
	lastID := ""

	for {
		batch, err := cfg.list(ctx, lastID)
		if err != nil {
			return stats, fmt.Errorf("list %s rows: %w", cfg.name, err)
		}
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
			tag, err := cfg.update(ctx, string(converted), row.data, row.id)
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

func rejectUnknownVersions(ctx context.Context, q *dbgen.Queries, v1version *string) error {
	rows, err := q.ListUnknownProtocolVersions(ctx, v1version)
	if err != nil {
		return fmt.Errorf("check protocol versions: %w", err)
	}

	var unknown []error
	for _, row := range rows {
		version := ""
		if row.ProtocolVersion != nil {
			version = *row.ProtocolVersion
		}
		unknown = append(unknown, fmt.Errorf("%s has %d row(s) with unsupported protocol_version %q", row.TableName, row.RowCount, version))
	}
	return errors.Join(unknown...)
}
