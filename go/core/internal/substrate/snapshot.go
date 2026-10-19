package substrate

import "go.ate.dev/substrate/pkg/proto/ateapipb"

// LatestActorSnapshot returns the newest generation, including an incomplete
// one. Falling back to an older durable snapshot would checkpoint stale state.
func LatestActorSnapshot(actor *ateapipb.Actor) *ateapipb.Snapshot {
	var latest *ateapipb.Snapshot
	for _, snapshot := range actor.GetStatus().GetSnapshots() {
		if snapshot.GetGeneration() > latest.GetGeneration() {
			latest = snapshot
		}
	}
	return latest
}

// SnapshotURI returns an address only after the durable copy is complete.
func SnapshotURI(snapshot *ateapipb.Snapshot) string {
	storage := snapshot.GetDurableSnapshot()
	if storage.GetStatus() != ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED {
		return ""
	}
	return storage.GetObject().GetSnapshotUri()
}
