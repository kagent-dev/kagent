package checkpoint

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	clocktesting "k8s.io/utils/clock/testing"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestPublicDeleteRetainsPermanentFailure(t *testing.T) {
	store := reviewStore(t)
	source, head := recoverySource(t, store)
	tags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source"}}
	service := NewService(store, testAuthorizer{}, tags, nil)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	cp, err := createAndRecover(service, owner, source.Id, uuid.NewString(), head)
	require.NoError(t, err)
	tags.mu.Lock()
	tags.base.created.Metadata.Uid = "replacement-uid"
	tags.mu.Unlock()
	stop := startRecovery(t, service)
	// Given a replaced tag, when a separate API replica calls public Delete,
	// assert that it receives the worker's failure without waiting for timeout.
	follower := NewService(store, testAuthorizer{}, tags, nil)
	ctx, cancel := context.WithTimeout(owner, 3*time.Second)
	defer cancel()
	require.Equal(t, codes.FailedPrecondition, status.Code(follower.Delete(ctx, cp.Id)))
	stop()
	pending, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_DELETING, pending.Checkpoint.State)
	require.Equal(t, int32(codes.FailedPrecondition), pending.OperationError.GetCode())
	require.Equal(t, "tag-uid", pending.TagUID)
	tags.mu.Lock()
	require.Zero(t, tags.base.deleteCalls)
	tags.mu.Unlock()
	// A fresh service with no worker or local error cache must observe the same
	// durable result, while another owner cannot read the operation error.
	follower = NewService(store, testAuthorizer{}, tags, nil)
	require.Equal(t, codes.FailedPrecondition, status.Code(follower.Delete(ctx, cp.Id)))
	_, err = store.GetCheckpointOperation(t.Context(), cp.Id, "bob")
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestPermanentRecoveryErrorsKeepBackendConfigurationPrivate(t *testing.T) {
	const private = "private-credential-value"
	for _, scenario := range []struct {
		name    string
		failure error
		message string
	}{
		{name: "backend status", failure: status.Error(codes.FailedPrecondition, private), message: "Checkpoint recovery cannot continue safely"},
		{name: "service error with private cause", failure: serviceerrors.NewFailedPrecondition("Snapshot identity changed", errors.New(private)), message: "Snapshot identity changed"},
	} {
		t.Run("Given a "+scenario.name+", when recovery persists a permanent error, assert that callers see only the safe message", func(t *testing.T) {
			failure := permanentOperationError(scenario.failure)
			require.Equal(t, codes.FailedPrecondition, failure.Code())
			require.Equal(t, scenario.message, failure.Message())
			require.NotContains(t, failure.Message(), private)
		})
	}
}

func TestRecoveryDiscoversDuringSlowCopy(t *testing.T) {
	store := reviewStore(t)
	slow, slowHead := recoverySource(t, store)
	fast, fastHead := recoverySource(t, store)
	gate := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(gate) })
	slowTags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source"}, gate: gate, entered: make(chan struct{})}
	tags := &indexedTags{actors: map[string]*lockedTags{
		substrate.ActorName(slow.Id): slowTags,
		substrate.ActorName(fast.Id): {base: &testTags{snapshotURI: "s3://snapshots/source"}},
	}}
	service := NewService(store, testAuthorizer{}, tags, nil)
	stop := startRecovery(t, service)
	defer stop()
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	slowDone := make(chan error, 1)
	go func() { _, err := service.Create(owner, slow.Id, uuid.NewString(), slowHead); slowDone <- err }()
	select {
	case <-slowTags.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("slow copy did not start")
	}
	// Given a blocked copy, when a later request arrives, assert that a free
	// worker publishes it while the original copy is still blocked.
	ctx, cancel := context.WithTimeout(owner, 2*time.Second)
	defer cancel()
	cp, err := service.Create(ctx, fast.Id, uuid.NewString(), fastHead)
	require.NoError(t, err)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, cp.State)
	select {
	case err := <-slowDone:
		t.Fatalf("slow copy unexpectedly finished: %v", err)
	default:
	}
	release.Do(func() { close(gate) })
	require.NoError(t, <-slowDone)
}

func TestRecoveryScansRespectRetryDelay(t *testing.T) {
	store := reviewStore(t)
	source, head := recoverySource(t, store)
	clock := clocktesting.NewFakeClock(time.Now())
	firstFailure := make(chan struct{})
	tags := &countedFailingTags{first: firstFailure, entered: make(chan struct{}), lockedTags: &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source", createErr: status.Error(codes.PermissionDenied, "storage denied")}}}
	service := NewService(store, testAuthorizer{}, tags, nil)
	service.clock = clock
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	cp, _, err := service.beginCreate(owner, source.Id, uuid.NewString(), head)
	require.NoError(t, err)
	worker := NewRecoveryWorker(service)
	worker.interval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	waitRetry := func(attempt int64) {
		require.Eventually(t, func() bool {
			worker.mu.Lock()
			defer worker.mu.Unlock()
			retry := worker.retries[checkpointKey{id: cp.Id, owner: "alice"}]
			return tags.calls.Load() >= attempt && retry != nil && retry.readyAt.After(clock.Now())
		}, 3*time.Second, time.Millisecond)
	}
	select {
	case <-tags.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first attempt did not start")
	}
	// A discovery while the first attempt runs marks the queue key dirty. The
	// post-failure immediate requeue must still respect the new retry deadline.
	require.NoError(t, worker.sweep(t.Context()))
	close(firstFailure)
	waitRetry(1)
	// Given a persistent failure, when discovery is repeatedly woken before
	// retry eligibility, assert that it makes no extra backend call or unpins.
	for range 8 {
		clock.Step(100 * time.Millisecond)
		service.wakeRecovery()
	}
	require.Never(t, func() bool { return tags.calls.Load() > 1 }, 100*time.Millisecond, time.Millisecond)
	_, err = store.BeginSessionOperation(t.Context(), source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.ErrorIs(t, err, database.ErrConflict)
	clock.Step(300 * time.Millisecond)
	service.wakeRecovery()
	waitRetry(2)
	// The second retry has a 1.8-2 second delay, not the first one's interval.
	clock.Step(1500 * time.Millisecond)
	service.wakeRecovery()
	require.Equal(t, int64(2), tags.calls.Load())
	clock.Step(600 * time.Millisecond)
	waitRetry(3)
	// Repairing the backend eventually completes the same admitted operation.
	tags.mu.Lock()
	tags.base.createErr = nil
	tags.mu.Unlock()
	clock.Step(5 * time.Second)
	service.wakeRecovery()
	waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_READY)
}

func TestPublicCheckpointWaitBackoff(t *testing.T) {
	for _, operation := range []string{"Create", "Delete"} {
		t.Run("Given pending work, when public "+operation+" waits for a minute, assert that reads stay bounded and cancellation returns promptly", func(t *testing.T) {
			store := &countedReadStore{testStore: &testStore{
				prepared: &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: uuid.NewString(), State: apiv1.CheckpointState_CHECKPOINT_STATE_CREATING},
				snapshot: &database.SessionTaskSnapshot{},
			}}
			clock := clocktesting.NewFakeClock(time.Now())
			service := NewService(store, testAuthorizer{}, nil, nil)
			service.clock = clock
			ctx, cancel := context.WithCancel(auth.AuthSessionTo(t.Context(), testSession{userID: "alice"}))
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if operation == "Create" {
					_, err := service.Create(ctx, store.prepared.SessionId, "request", "head")
					done <- err
				} else {
					done <- service.Delete(ctx, store.prepared.Id)
				}
			}()
			for range 6000 {
				require.Eventually(t, clock.HasWaiters, time.Second, time.Millisecond)
				clock.Step(10 * time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("canceled caller remained blocked")
			}
			t.Logf("60 seconds of waiting performed %d reads", store.reads.Load())
			require.Less(t, store.reads.Load(), int64(80))
			require.Greater(t, store.reads.Load(), int64(55), "waiters must keep checking durable progress")
		})
	}
}

func TestCheckpointActivationPreservesAdmittedWork(t *testing.T) {
	store := reviewStore(t)
	source, head := recoverySource(t, store)
	tags := &lockedTags{base: &testTags{snapshotURI: "s3://snapshots/source"}, entered: make(chan struct{})}
	service := NewService(store, testAuthorizer{}, tags, nil)
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	request := uuid.NewString()
	cp, _, err := service.beginCreate(owner, source.Id, request, head)
	require.NoError(t, err)
	ready := make(chan struct{})
	service.GateRecovery(ready)
	stop := startRecovery(t, service)
	defer stop()
	// Given admitted legacy work, when replacements start before writers drain,
	// assert that public mutations fail promptly and no worker starts a copy.
	_, err = service.Create(owner, source.Id, request, head)
	require.Equal(t, serviceerrors.CodeUnavailable, serviceerrors.CodeOf(err))
	require.Equal(t, serviceerrors.CodeUnavailable, serviceerrors.CodeOf(service.Delete(owner, cp.Id)))
	select {
	case <-tags.entered:
		t.Fatal("recovery executed before activation")
	default:
	}
	pending, err := store.GetCheckpointOperation(t.Context(), cp.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, pending.Checkpoint.State)
	_, err = store.BeginSessionOperation(t.Context(), source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.ErrorIs(t, err, database.ErrConflict)
	close(ready)
	published, err := service.Create(owner, source.Id, request, head)
	require.NoError(t, err)
	require.Equal(t, cp.Id, published.Id)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, published.State)
}

func reviewStore(t *testing.T) *database.Client {
	t.Helper()
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	require.NoError(t, dbtest.Migrate(dsn, false))
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn), Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return database.NewClient(pool, "public")
}

type countedReadStore struct {
	*testStore
	reads atomic.Int64
}

func (s *countedReadStore) GetCheckpointOperation(_ context.Context, _ string, owner string) (*database.CheckpointOperation, error) {
	s.reads.Add(1)
	return &database.CheckpointOperation{Checkpoint: proto.CloneOf(s.prepared), Creator: owner}, nil
}

type countedFailingTags struct {
	*lockedTags
	calls   atomic.Int64
	first   <-chan struct{}
	entered chan struct{}
}

func (t *countedFailingTags) CreateTag(ctx context.Context, space, name, actor string) (*ateapipb.Tag, error) {
	call := t.calls.Add(1)
	if call == 1 && t.first != nil {
		close(t.entered)
		select {
		case <-t.first:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	t.mu.Lock()
	failure := t.base.createErr
	t.mu.Unlock()
	if failure != nil {
		return nil, failure
	}
	return t.lockedTags.CreateTag(ctx, space, name, actor)
}

type indexedTags struct {
	actors map[string]*lockedTags
	tags   sync.Map
}

func (t *indexedTags) GetActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	return t.actors[name].GetActor(ctx, space, name)
}

func (t *indexedTags) CreateTag(ctx context.Context, space, name, actor string) (*ateapipb.Tag, error) {
	t.tags.Store(name, t.actors[actor])
	return t.actors[actor].CreateTag(ctx, space, name, actor)
}

func (t *indexedTags) GetTag(ctx context.Context, space, name string) (*ateapipb.Tag, error) {
	client, ok := t.tags.Load(name)
	if !ok {
		return nil, status.Error(codes.NotFound, "tag absent")
	}
	return client.(*lockedTags).GetTag(ctx, space, name)
}

func (t *indexedTags) DeleteTag(ctx context.Context, space, name, uid string) error {
	client, ok := t.tags.Load(name)
	if !ok {
		return status.Error(codes.NotFound, "tag absent")
	}
	return client.(*lockedTags).DeleteTag(ctx, space, name, uid)
}
