package checkpoint

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
)

func TestIdlePurgeRetriesLostTagDeleteResponse(t *testing.T) {
	ctx := t.Context()
	checkpointID := uuid.NewString()
	tags := &testTags{deleteErr: status.Error(codes.Unavailable, "transport closed")}
	tag, err := tags.CreateTag(ctx, "team-a", tagName(checkpointID), "source")
	require.NoError(t, err)
	store := &testStore{}
	queue := &purgeTestStore{checkpoint: database.IdlePurgeCheckpoint{
		ID: checkpointID, UserID: "user", TagUID: tag.GetMetadata().GetUid(),
		Snapshot: database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://tags/checkpoint"},
	}}
	historyID := uuid.New()
	queue.checkpoints = store
	worker, err := NewPurgeWorker(queue, NewService(store, testAuthorizer{}, tags, nil), time.Second, noop.NewMeterProvider())
	require.NoError(t, err)
	tags.beforeDelete = func() { tags.created = nil }
	require.Error(t, worker.purge(ctx, historyID))
	require.False(t, store.deleted, "the row retains the Tag identity until deletion settles")

	// A replacement controller observes the settled Tag deletion and finishes.
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	worker, err = NewPurgeWorker(queue, NewService(store, testAuthorizer{}, tags, nil), time.Second, meterProvider)
	require.NoError(t, err)
	require.NoError(t, worker.purge(ctx, historyID))
	require.True(t, store.deleted)
	require.True(t, queue.historyDeleted)
	require.Equal(t, 1, tags.deleteCalls)
	requirePurgeCounter(t, reader, conv.KagentSessionPurgeCompleted, conv.KagentSessionPurgeCompletedUnit)
}

func TestIdlePurgeRecordsBlockedAttempt(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	worker, err := NewPurgeWorker(blockedPurgeStore{}, nil, time.Second, meterProvider)
	require.NoError(t, err)
	require.NoError(t, worker.purge(t.Context(), uuid.New()))
	requirePurgeCounter(t, reader, conv.KagentSessionPurgeBlocked, conv.KagentSessionPurgeBlockedUnit)
}

func requirePurgeCounter(t *testing.T, reader *sdkmetric.ManualReader, name, unit string) {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))
	require.Len(t, metrics.ScopeMetrics, 1)
	measurements := metrics.ScopeMetrics[0].Metrics
	require.Len(t, measurements, 1)
	require.Equal(t, name, measurements[0].Name)
	require.Equal(t, unit, measurements[0].Unit)
	sum, ok := measurements[0].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.True(t, sum.IsMonotonic)
	require.Len(t, sum.DataPoints, 1)
	require.EqualValues(t, 1, sum.DataPoints[0].Value)
	require.Zero(t, sum.DataPoints[0].Attributes.Len())
}

type blockedPurgeStore struct{ purgeStore }

func (blockedPurgeStore) BeginIdlePurgeCheckpointDeletion(context.Context, uuid.UUID) (*database.IdlePurgeCheckpoint, bool, error) {
	return nil, false, nil
}

func (blockedPurgeStore) DeleteIdlePurgeHistory(context.Context, uuid.UUID) (bool, error) {
	return false, database.ErrConflict
}

func (blockedPurgeStore) ListIdlePurgeForks(context.Context, uuid.UUID, string, int) ([]database.IdlePurgeFork, error) {
	return nil, nil
}

func (blockedPurgeStore) IdlePurgeLastEventAt(context.Context, uuid.UUID) (time.Time, error) {
	return time.Now(), nil
}

func TestIdlePurgeOnlyReleasesForkPinAfterOwnedSnapshot(t *testing.T) {
	queue := &purgeTestStore{}
	tags := &purgeForkTags{owner: ateapipb.SnapshotOwner_SNAPSHOT_OWNER_TAG}
	worker, err := NewPurgeWorker(queue, NewService(&testStore{}, testAuthorizer{}, tags, nil), time.Second, noop.NewMeterProvider())
	require.NoError(t, err)
	fork := database.IdlePurgeFork{ID: uuid.NewString(), ActorUID: "actor-uid", Atespace: "team-a"}
	released, err := worker.releaseForkPin(t.Context(), fork)
	require.NoError(t, err)
	require.False(t, released)
	require.Empty(t, queue.snapshot.URI)

	tags.owner = ateapipb.SnapshotOwner_SNAPSHOT_OWNER_ACTOR
	released, err = worker.releaseForkPin(t.Context(), fork)
	require.NoError(t, err)
	require.True(t, released)
	require.Equal(t, "s3://fork-snapshot", queue.snapshot.URI)
}

func TestIdlePurgeContinuesAfterForkLookupFailure(t *testing.T) {
	first := database.IdlePurgeFork{ID: uuid.NewString(), ActorUID: "actor-uid", Atespace: "unavailable"}
	second := database.IdlePurgeFork{ID: uuid.NewString(), ActorUID: "actor-uid", Atespace: "team-a"}
	store := &purgeTestStore{forks: []database.IdlePurgeFork{first, second}}
	tags := &purgeForkTags{owner: ateapipb.SnapshotOwner_SNAPSHOT_OWNER_ACTOR}
	worker, err := NewPurgeWorker(store, NewService(&testStore{}, testAuthorizer{}, tags, nil), time.Second, noop.NewMeterProvider())
	require.NoError(t, err)
	released, err := worker.releaseForkPins(t.Context(), uuid.New())
	require.NoError(t, err)
	require.True(t, released)
	require.Equal(t, second.ID, store.releasedFork)
}

type purgeTestStore struct {
	forks        []database.IdlePurgeFork
	releasedFork string
	purgeStore
	checkpoint     database.IdlePurgeCheckpoint
	checkpoints    *testStore
	historyDeleted bool
	snapshot       database.SessionTaskSnapshot
}

func (s *purgeTestStore) ListIdlePurgeForks(context.Context, uuid.UUID, string, int) ([]database.IdlePurgeFork, error) {
	return s.forks, nil
}

func (s *purgeTestStore) ReleaseIdlePurgeForkPin(_ context.Context, fork database.IdlePurgeFork, snapshot database.SessionTaskSnapshot) (bool, error) {
	s.releasedFork = fork.ID
	s.snapshot = snapshot
	return true, nil
}

type purgeForkTags struct {
	testTags
	owner ateapipb.SnapshotOwner
}

func (t *purgeForkTags) GetActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if atespace == "unavailable" {
		return nil, status.Error(codes.Unavailable, "lookup unavailable")
	}
	t.snapshotURI = "s3://fork-snapshot"
	actor, err := t.testTags.GetActor(ctx, atespace, name)
	actor.GetStatus().GetSnapshots()[0].Owner = t.owner
	return actor, err
}

func (s *purgeTestStore) BeginIdlePurgeCheckpointDeletion(context.Context, uuid.UUID) (*database.IdlePurgeCheckpoint, bool, error) {
	if s.checkpoints.deleted {
		return nil, false, nil
	}
	return &s.checkpoint, false, nil
}

func (s *purgeTestStore) DeleteIdlePurgeHistory(context.Context, uuid.UUID) (bool, error) {
	s.historyDeleted = true
	return true, nil
}
