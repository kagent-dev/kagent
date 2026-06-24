package a2amigration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/pkg/a2acompat/trpcv0"
)

func TestRunMigratesA2AData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	connStr := dbtest.StartT(ctx, t)
	dbtest.MigrateT(t, connStr, false)

	db, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(db.Close)

	taskData := mustMarshalLegacyFixture(t, trpcv0.LegacyTextTaskFixture())
	pushData := mustMarshalLegacyFixture(t, trpcv0.LegacyPushConfigFixture())

	_, err = db.Exec(ctx, `INSERT INTO task (id, data, session_id) VALUES ($1, $2, $3)`, "task-1", string(taskData), "session-1")
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	_, err = db.Exec(ctx, `INSERT INTO push_notification (id, task_id, data) VALUES ($1, $2, $3)`, "push-1", "task-1", string(pushData))
	if err != nil {
		t.Fatalf("insert push notification: %v", err)
	}

	dryRunStats, err := Run(ctx, db, Options{DryRun: true, BatchSize: 1})
	if err != nil {
		t.Fatalf("dry-run migration: %v", err)
	}
	if dryRunStats.TasksMigrated != 1 || dryRunStats.PushNotificationsMigrated != 1 {
		t.Fatalf("dry-run stats = %+v", dryRunStats)
	}
	assertProtocolVersion(t, db, "task", "task-1", nil)
	assertProtocolVersion(t, db, "push_notification", "push-1", nil)

	stats, err := Run(ctx, db, Options{BatchSize: 1})
	if err != nil {
		t.Fatalf("migration: %v", err)
	}
	if stats.TasksMigrated != 1 || stats.PushNotificationsMigrated != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	wantV1 := trpcv0.ProtocolVersionV1
	assertProtocolVersion(t, db, "task", "task-1", &wantV1)
	assertProtocolVersion(t, db, "push_notification", "push-1", &wantV1)
	assertTaskLooksV1(t, db)
	assertPushNotificationLooksV1(t, db)

	secondStats, err := Run(ctx, db, Options{BatchSize: 1})
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if secondStats.TasksMigrated != 0 || secondStats.PushNotificationsMigrated != 0 || secondStats.AlreadyV1 != 2 {
		t.Fatalf("second stats = %+v", secondStats)
	}
}

func TestRunRejectsUnknownProtocolVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	connStr := dbtest.StartT(ctx, t)
	dbtest.MigrateT(t, connStr, false)

	db, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(db.Close)

	_, err = db.Exec(ctx, `INSERT INTO task (id, data, protocol_version) VALUES ($1, $2, $3)`, "task-unknown", `{}`, "2.0")
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	_, err = Run(ctx, db, Options{})
	if err == nil {
		t.Fatal("expected unknown protocol version error")
	}
}

func assertProtocolVersion(t *testing.T, db *pgxpool.Pool, table, id string, want *string) {
	t.Helper()
	var got *string
	err := db.QueryRow(context.Background(), `SELECT protocol_version FROM `+table+` WHERE id = $1`, id).Scan(&got)
	if err != nil {
		t.Fatalf("query protocol_version: %v", err)
	}
	if want == nil {
		if got != nil {
			t.Fatalf("%s protocol_version = %q, want nil", table, *got)
		}
		return
	}
	if got == nil || *got != *want {
		t.Fatalf("%s protocol_version = %v, want %q", table, got, *want)
	}
}

func assertTaskLooksV1(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	var text string
	err := db.QueryRow(context.Background(), `SELECT data::jsonb #>> '{history,0,parts,0,text}' FROM task WHERE id = 'task-1'`).Scan(&text)
	if err != nil {
		t.Fatalf("query v1 task text part: %v", err)
	}
	if text != "hi" {
		t.Fatalf("v1 task text part = %q", text)
	}
	var role string
	err = db.QueryRow(context.Background(), `SELECT data::jsonb #>> '{history,0,role}' FROM task WHERE id = 'task-1'`).Scan(&role)
	if err != nil {
		t.Fatalf("query v1 task role: %v", err)
	}
	if role != "ROLE_USER" {
		t.Fatalf("v1 task role = %q", role)
	}
}

func assertPushNotificationLooksV1(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	var scheme string
	err := db.QueryRow(context.Background(), `SELECT data::jsonb #>> '{authentication,scheme}' FROM push_notification WHERE id = 'push-1'`).Scan(&scheme)
	if err != nil {
		t.Fatalf("query v1 push notification auth scheme: %v", err)
	}
	if scheme != "Bearer" {
		t.Fatalf("v1 push notification auth scheme = %q", scheme)
	}
}

func mustMarshalLegacyFixture(t *testing.T, fixture any) []byte {
	t.Helper()
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal legacy fixture: %v", err)
	}
	return data
}
