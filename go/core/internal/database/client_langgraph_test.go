package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/stretchr/testify/require"
)

// countingTracer is a pgx.QueryTracer that counts how many executed statements
// contain a given SQL substring. It is used to guard against the N+1 query
// pattern in ListCheckpoints.
type countingTracer struct {
	substr string
	mu     sync.Mutex
	count  int
}

func (t *countingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, t.substr) {
		t.mu.Lock()
		t.count++
		t.mu.Unlock()
	}
	return ctx
}

func (t *countingTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (t *countingTracer) reset() {
	t.mu.Lock()
	t.count = 0
	t.mu.Unlock()
}

func (t *countingTracer) value() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// newCountingClient opens a single-connection pool against the shared test
// database with a query-counting tracer attached, so a test can assert how many
// times a particular query runs.
func newCountingClient(t *testing.T, tracer *countingTracer) dbpkg.Client {
	t.Helper()

	config, err := pgxpool.ParseConfig(sharedConnStr)
	require.NoError(t, err)
	config.MaxConns = 1
	config.ConnConfig.Tracer = tracer

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return NewClient(pool)
}

// TestListCheckpointsBatchesWrites verifies that reading a thread's checkpoint
// history fetches all checkpoint writes in a single query instead of one query
// per checkpoint (the N+1 pattern). It also checks the writes are grouped onto
// the correct checkpoints and keep their per-checkpoint ordering.
func TestListCheckpointsBatchesWrites(t *testing.T) {
	setupTestDB(t)

	tracer := &countingTracer{substr: "FROM lg_checkpoint_write"}
	client := newCountingClient(t, tracer)
	ctx := context.Background()

	const (
		userID       = "test-user"
		threadID     = "test-thread"
		checkpointNS = ""
		numCP        = 5
		writesPerCP  = 3
	)

	// Seed several checkpoints, each with multiple writes.
	for i := range numCP {
		cpID := fmt.Sprintf("cp-%02d", i)
		err := client.StoreCheckpoint(ctx, &dbpkg.LangGraphCheckpoint{
			UserID:       userID,
			ThreadID:     threadID,
			CheckpointNS: checkpointNS,
			CheckpointID: cpID,
			Metadata:     "{}",
			Checkpoint:   "{}",
			Version:      1,
		})
		require.NoError(t, err)

		writes := make([]*dbpkg.LangGraphCheckpointWrite, writesPerCP)
		for j := range writesPerCP {
			writes[j] = &dbpkg.LangGraphCheckpointWrite{
				UserID:       userID,
				ThreadID:     threadID,
				CheckpointNS: checkpointNS,
				CheckpointID: cpID,
				WriteIdx:     int64(j),
				Value:        fmt.Sprintf("value-%d", j),
				ValueType:    "json",
				Channel:      fmt.Sprintf("channel-%d", j),
				TaskID:       "task-0",
			}
		}
		require.NoError(t, client.StoreCheckpointWrites(ctx, writes))
	}

	// Read the whole thread history (checkpointID nil, limit 0 -> unbounded).
	tracer.reset()
	tuples, err := client.ListCheckpoints(ctx, userID, threadID, checkpointNS, nil, 0)
	require.NoError(t, err)

	// Exactly one query against lg_checkpoint_write, regardless of how many
	// checkpoints the thread has. Before batching this was one query per
	// checkpoint (numCP).
	writeQueries := tracer.value()
	require.Equal(t, 1, writeQueries,
		"expected a single checkpoint-writes query, got %d (N+1 regression)", writeQueries)

	// Every checkpoint's writes are attached, in write_idx order.
	require.Len(t, tuples, numCP)
	for _, tuple := range tuples {
		require.Len(t, tuple.Writes, writesPerCP, "checkpoint %s", tuple.Checkpoint.CheckpointID)
		for j, w := range tuple.Writes {
			require.Equal(t, int64(j), w.WriteIdx)
			require.Equal(t, tuple.Checkpoint.CheckpointID, w.CheckpointID)
		}
	}
}
