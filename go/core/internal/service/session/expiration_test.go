package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSessionExpirationRetriesDeletion(t *testing.T) {
	for _, failure := range []string{"preparation", "lost runtime response", "persistence"} {
		t.Run(failure, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			session, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
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
			worker, err := NewExpirationWorker(store, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Second), 7*24*time.Hour, time.Minute)
			require.NoError(t, err)
			require.Error(t, worker.expire(t.Context(), session.Id, time.Now()))
			require.ErrorIs(t, store.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next turn"), database.ErrConflict)

			actors.readErr, writes.finishErr = nil, nil
			// A replacement worker discovers the durable expiration even with
			// a longer TTL, without a client retry or an in-memory work queue.
			worker, err = NewExpirationWorker(store, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Second), 30*24*time.Hour, time.Minute)
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

func (e *expirationActors) DeleteActor(ctx context.Context, space, name string, anyState bool) error {
	e.deletions++
	if err := e.retryTestActors.DeleteActor(ctx, space, name, anyState); err != nil {
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
	worker, err := NewExpirationWorker(store, NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second), time.Hour, time.Minute)
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
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
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
	session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	require.NoError(t, err)
	writes := &completionTestStore{lifecycleTestStore: store, finishErr: errors.New("database unavailable")}
	worker, err := NewExpirationWorker(store, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Second), time.Hour, time.Minute)
	require.NoError(t, err)
	require.Error(t, worker.expire(t.Context(), session.Id, time.Now()))
	writes.finishErr = nil
	require.NoError(t, worker.expire(t.Context(), session.Id, time.Now()))
	require.ErrorIs(t, worker.expire(t.Context(), session.Id, time.Time{}), database.ErrNotFound)
	require.EqualValues(t, 1, counterValue(t, reader, "kagent.session.expired"))
}

// counterValue reads one Int64Counter's single data point, or fails the test.
func counterValue(t *testing.T, reader *metric.ManualReader, name string) int64 {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))
	for _, scope := range metrics.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name == name {
				sum := measurement.Data.(metricdata.Sum[int64])
				require.Len(t, sum.DataPoints, 1)
				return sum.DataPoints[0].Value
			}
		}
	}
	t.Fatalf("missing counter %s", name)
	return 0
}

// The sweep deletes the shares expired at its clock and counts them; a share
// expiring later and one without expiry stay for the owner to see and revoke.
func TestSessionExpirationDeletesExpiredShares(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	store, session := lifecycleFixture(t)
	worker, err := NewExpirationWorker(store, NewActorWorkflow(store, &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}, make(chan struct{}, 1), time.Second), time.Hour, time.Minute)
	require.NoError(t, err)
	now := time.Now().Truncate(time.Microsecond)
	share := func(expiresAt *timestamppb.Timestamp, token string) string {
		t.Helper()
		created, err := store.CreateSessionShare(t.Context(), &apiv1alpha1.SessionShare{
			Id: uuid.NewString(), SessionId: session.Id,
			Permission: apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_ONLY,
			ExpiresAt:  expiresAt,
		}, []byte(token), session.Creator)
		require.NoError(t, err)
		return created.GetId()
	}
	listed := func() []string {
		t.Helper()
		shares, err := store.ListSessionShares(t.Context(), session.Id, session.Creator, "", 10)
		require.NoError(t, err)
		ids := make([]string, 0, len(shares))
		for _, share := range shares {
			ids = append(ids, share.GetId())
		}
		return ids
	}
	share(timestamppb.New(now.Add(-time.Second)), "expired")
	later := share(timestamppb.New(now.Add(time.Hour)), "later")
	unbounded := share(nil, "unbounded")

	worker.expireShares(t.Context(), now)
	require.ElementsMatch(t, []string{later, unbounded}, listed())
	worker.expireShares(t.Context(), now)
	require.EqualValues(t, 1, counterValue(t, reader, "kagent.session.share_expired"), "a sweep that finds nothing counts nothing")

	worker.expireShares(t.Context(), now.Add(2*time.Hour))
	require.Equal(t, []string{unbounded}, listed(), "a share without expiry stays")
	require.EqualValues(t, 2, counterValue(t, reader, "kagent.session.share_expired"))
}

func TestSessionExpirationConfiguration(t *testing.T) {
	_, err := NewExpirationWorker(nil, nil, -time.Second, time.Minute)
	require.Error(t, err)
	for _, interval := range []time.Duration{0, -time.Second} {
		_, err := NewExpirationWorker(nil, nil, time.Hour, interval)
		require.ErrorContains(t, err, "poll interval must be positive")
	}
	store := &sweepCountingStore{}
	worker, err := NewExpirationWorker(store, nil, 0, time.Millisecond)
	require.NoError(t, err)
	require.True(t, worker.NeedLeaderElection())
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- worker.Start(ctx) }()
	// A zero idle TTL turns idle deletion off, not the share sweep and not the
	// sweep of the sessions of deleted Agents.
	require.Eventually(t, func() bool { return store.shareSweeps.Load() > 1 && store.deletedAgentListings.Load() > 1 }, 5*time.Second, time.Millisecond)
	require.Zero(t, store.idleListings.Load(), "a disabled idle deletion never lists idle sessions")
	cancel()
	require.NoError(t, <-done)
}

// sweepCountingStore counts the sweeps a worker runs and holds nothing.
type sweepCountingStore struct {
	idleListings         atomic.Int64
	deletedAgentListings atomic.Int64
	shareSweeps          atomic.Int64
}

func (s *sweepCountingStore) ListIdleSessions(context.Context, time.Time, string, int) ([]string, error) {
	s.idleListings.Add(1)
	return nil, nil
}

func (*sweepCountingStore) BeginIdleSessionDeletion(context.Context, string, time.Time) (*database.IdleSessionDeletion, error) {
	return nil, database.ErrNotFound
}

func (s *sweepCountingStore) ListSessionsOfDeletedAgents(context.Context, string, int) ([]string, error) {
	s.deletedAgentListings.Add(1)
	return nil, nil
}

func (*sweepCountingStore) BeginDeletedAgentSessionDeletion(context.Context, string) (*database.SessionOperation, error) {
	return nil, database.ErrNotFound
}

func (s *sweepCountingStore) DeleteExpiredSessionShares(context.Context, time.Time) (int64, error) {
	s.shareSweeps.Add(1)
	return 0, nil
}

func TestSessionExpirationWorkerDiscoversIdleSessions(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
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
	session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	writes := &completionTestStore{lifecycleTestStore: store, afterClaim: func(ctx context.Context) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}}
	first, err := NewExpirationWorker(store, NewActorWorkflow(writes, actors, make(chan struct{}, 1), time.Second), time.Hour, time.Minute)
	require.NoError(t, err)
	second, err := NewExpirationWorker(store, NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second), time.Hour, time.Minute)
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
