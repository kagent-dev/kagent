package substrate

import "github.com/agent-substrate/substrate/pkg/proto/ateapipb"

func snapshotConfig(location string, preserveMemory bool) *ateapipb.SnapshotConfig {
	scope := ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA
	if preserveMemory {
		scope = ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL
	}
	return &ateapipb.SnapshotConfig{
		StorageLocation: location,
		OnPause:         ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
		OnCommit:        scope,
	}
}
