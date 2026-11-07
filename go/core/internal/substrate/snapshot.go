package substrate

import ateapipb "go.ate.dev/substrate/api/ateapi/v1"

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
	if storage.GetState() != ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED {
		return ""
	}
	return storage.GetObject().GetSnapshotUri()
}

// Checkpoints require the newest generation; revert retains the newest durable copy.
func LatestDurableActorSnapshot(actor *ateapipb.Actor) *ateapipb.Snapshot {
	var latest *ateapipb.Snapshot
	for _, snapshot := range actor.GetStatus().GetSnapshots() {
		if SnapshotURI(snapshot) != "" && snapshot.GetGeneration() > latest.GetGeneration() {
			latest = snapshot
		}
	}
	return latest
}
