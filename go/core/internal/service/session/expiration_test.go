package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSessionExpirationRetriesDeletion(t *testing.T) {
	for _, failure := range []string{"preparation", "lost runtime response", "persistence"} {
		t.Run(failure, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			session, err := NewActorWorkflow(store, base).Create(t.Context(), session)
			require.NoError(t, err)
			actors := &expirationActors{retryTestActors: &retryTestActors{lifecycleTestActors: base}}
			writes := &completionTestStore{lifecycleTestStore: store}
			switch failure {
			case "preparation":
				actors.readErr = status.Error(codes.Unavailable, "read unavailable")
			case "lost runtime response":
				actors.loseDeleteResponse = true
			case "persistence":
				writes.finishErr = errors.New("database unavailable")
			}
			worker, err := NewExpirationWorker(store, NewActorWorkflow(writes, actors), 7*24*time.Hour, time.Minute)
			require.NoError(t, err)
			require.Error(t, worker.expire(t.Context(), session.Id, time.Now()))
			require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next turn"), database.ErrConflict)

			actors.readErr, writes.finishErr = nil, nil
			// A replacement worker discovers the durable expiration even with
			// a longer TTL, without a client retry or an in-memory work queue.
			worker, err = NewExpirationWorker(store, NewActorWorkflow(writes, actors), 30*24*time.Hour, time.Minute)
			require.NoError(t, err)
			ids, err := store.ListIdleSessions(t.Context(), time.Time{}, "", 100)
			require.NoError(t, err)
			require.Equal(t, []string{session.Id}, ids)
			require.NoError(t, worker.expire(t.Context(), session.Id, time.Time{}))
			_, err = store.GetSessionByID(t.Context(), session.Id)
			require.ErrorIs(t, err, database.ErrNotFound)
			require.Empty(t, base.actors)
			require.Equal(t, 1, actors.deletions, "retries must not allocate or delete another Actor")
		})
	}
}

type expirationActors struct {
	*retryTestActors
	loseDeleteResponse bool
	deletions          int
}

func (e *expirationActors) DeleteActor(ctx context.Context, space, name string) error {
	e.deletions++
	if err := e.retryTestActors.DeleteActor(ctx, space, name); err != nil {
		return err
	}
	if e.loseDeleteResponse {
		e.loseDeleteResponse = false
		return status.Error(codes.Unavailable, "lost delete response")
	}
	return nil
}

func TestSessionExpirationRetainsCheckpointHistory(t *testing.T) {
	store, source := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	_, checkpointID := lifecycleForkFixture(t, store, actors, source)
	worker, err := NewExpirationWorker(store, NewActorWorkflow(store, actors), time.Hour, time.Minute)
	require.NoError(t, err)
	require.NoError(t, worker.expire(t.Context(), source.Id, time.Now()))
	// A new fork after expiration reconstructs the retained conversation.
	fork, created, err := store.ForkSession(t.Context(), checkpointID, source.Creator, uuid.NewString(), uuid.NewString())
	require.NoError(t, err)
	require.True(t, created)
	tasks, count, err := store.ListSessionTasks(t.Context(), fork.Id, "", "", nil, 100, nil)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, a2a.NewTextPart("hello"), tasks[0].History[0].Parts[0])
}

func TestSessionDeleteCompletesIdleDeletion(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	_, err = store.BeginIdleSessionDeletion(t.Context(), session.Id, time.Now())
	require.NoError(t, err)

	// An explicit Delete joins the idle deletion and removes its receipt in
	// the ordinary completion transaction; no separate expiration finish runs.
	_, err = workflow.Delete(t.Context(), session)
	require.NoError(t, err)
	_, err = store.GetSessionByID(t.Context(), session.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	worker, err := NewExpirationWorker(store, workflow, time.Hour, time.Minute)
	require.NoError(t, err)
	require.ErrorIs(t, worker.expire(t.Context(), session.Id, time.Time{}), database.ErrNotFound)
}

func TestSessionExpirationCountsCompletedSweepDeletionOnce(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	session, err := NewActorWorkflow(store, actors).Create(t.Context(), session)
	require.NoError(t, err)
	writes := &completionTestStore{lifecycleTestStore: store, finishErr: errors.New("database unavailable")}
	worker, err := NewExpirationWorker(store, NewActorWorkflow(writes, actors), time.Hour, time.Minute)
	require.NoError(t, err)
	require.Error(t, worker.expire(t.Context(), session.Id, time.Now()))
	writes.finishErr = nil
	require.NoError(t, worker.expire(t.Context(), session.Id, time.Now()))
	require.ErrorIs(t, worker.expire(t.Context(), session.Id, time.Time{}), database.ErrNotFound)
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))
	for _, scope := range metrics.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name == "kagent.session.expired" {
				sum := measurement.Data.(metricdata.Sum[int64])
				require.Len(t, sum.DataPoints, 1)
				require.EqualValues(t, 1, sum.DataPoints[0].Value)
				return
			}
		}
	}
	t.Fatal("missing session expiration counter")
}

func TestSessionExpirationConfiguration(t *testing.T) {
	_, err := NewExpirationWorker(nil, nil, -time.Second, time.Minute)
	require.Error(t, err)
	for _, interval := range []time.Duration{0, -time.Second} {
		_, err := NewExpirationWorker(nil, nil, time.Hour, interval)
		require.ErrorContains(t, err, "poll interval must be positive")
	}
	worker, err := NewExpirationWorker(nil, nil, 0, time.Minute)
	require.NoError(t, err)
	require.True(t, worker.NeedLeaderElection())
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- worker.Start(ctx) }()
	select {
	case <-done:
		t.Fatal("disabled worker should wait for shutdown without accessing the store")
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	require.NoError(t, <-done)
}

func TestSessionExpirationWorkerDiscoversIdleSessions(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	worker, err := NewExpirationWorker(store, workflow, time.Nanosecond, time.Minute)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	require.Eventually(t, func() bool {
		_, err := store.GetSessionByID(t.Context(), session.Id)
		return errors.Is(err, database.ErrNotFound)
	}, 5*time.Second, 10*time.Millisecond)
}

func TestSessionExpirationSerializesRuntimeAttempts(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	session, err := NewActorWorkflow(store, actors).Create(t.Context(), session)
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	writes := &completionTestStore{lifecycleTestStore: store, afterClaim: func(ctx context.Context) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}}
	first, err := NewExpirationWorker(store, NewActorWorkflow(writes, actors), time.Hour, time.Minute)
	require.NoError(t, err)
	second, err := NewExpirationWorker(store, NewActorWorkflow(store, actors), time.Hour, time.Minute)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- first.expire(ctx, session.Id, time.Now()) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("expiration did not claim runtime execution")
	}
	require.ErrorIs(t, second.expire(t.Context(), session.Id, time.Now()), database.ErrConflict)
	require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next"), database.ErrConflict)
	close(release)
	// Cleanup waits for the attempt; check through the public store boundary.
	require.Eventually(t, func() bool {
		_, err := store.GetSessionByID(t.Context(), session.Id)
		return errors.Is(err, database.ErrNotFound)
	}, 5*time.Second, 10*time.Millisecond)
}
