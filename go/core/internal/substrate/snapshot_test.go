package substrate

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
)

func TestActorSnapshotGenerationAndDurability(t *testing.T) {
	durable := &ateapipb.Snapshot{Generation: 1, DurableSnapshot: &ateapipb.SnapshotStorage{
		Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED,
		Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/old"},
	}}
	for _, test := range []struct {
		name    string
		latest  *ateapipb.Snapshot
		wantURI string
	}{
		{name: "node-local snapshot", latest: &ateapipb.Snapshot{Generation: 2, Locality: "node-1"}},
		{name: "unfinished local checkpoint", latest: &ateapipb.Snapshot{Generation: 2}},
		{name: "pending upload", latest: &ateapipb.Snapshot{Generation: 2, DurableSnapshot: &ateapipb.SnapshotStorage{
			Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_PENDING,
			Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/new"},
		}}},
		{name: "completed upload", wantURI: "s3://snapshots/new", latest: &ateapipb.Snapshot{Generation: 2, DurableSnapshot: &ateapipb.SnapshotStorage{
			Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED,
			Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/new"},
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actor := &ateapipb.Actor{Status: &ateapipb.ActorStatus{
				State:     ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
				Snapshots: []*ateapipb.Snapshot{test.latest, durable},
			}}
			require.Same(t, test.latest, LatestActorSnapshot(actor))
			require.Equal(t, test.wantURI, SnapshotURI(LatestActorSnapshot(actor)))
		})
	}
	require.Nil(t, LatestActorSnapshot(nil))
	require.Empty(t, SnapshotURI(nil))
}

func TestRevertSnapshotSkipsIncompleteGenerations(t *testing.T) {
	durable := &ateapipb.Snapshot{Generation: 2, DurableSnapshot: &ateapipb.SnapshotStorage{
		Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED,
		Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/durable"},
	}}
	for _, latest := range []*ateapipb.Snapshot{
		{Generation: 3, Locality: "node-1"},
		{Generation: 3, DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_PENDING}},
		{Generation: 3, DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED}},
	} {
		actor := &ateapipb.Actor{Status: &ateapipb.ActorStatus{Snapshots: []*ateapipb.Snapshot{latest, durable}}}
		require.Same(t, durable, LatestDurableActorSnapshot(actor))
		require.Same(t, latest, LatestActorSnapshot(actor))
	}
	require.Nil(t, LatestDurableActorSnapshot(nil))
	require.Nil(t, LatestDurableActorSnapshot(&ateapipb.Actor{Status: &ateapipb.ActorStatus{
		Snapshots: []*ateapipb.Snapshot{{Generation: 1, Locality: "node-1"}},
	}}))
}
