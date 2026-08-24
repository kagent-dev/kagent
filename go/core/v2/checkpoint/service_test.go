package checkpoint

import (
	"context"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type testSession struct{ userID string }

func (s testSession) Principal() auth.Principal { return auth.Principal{User: auth.User{ID: s.userID}} }

type testAuthorizer struct{}

func (testAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return nil
}

type testStore struct {
	prepared *dbpkg.AgentInstanceCheckpoint
	failed   string
}

func (s *testStore) PrepareAgentInstanceCheckpoint(_ context.Context, checkpoint dbpkg.AgentInstanceCheckpoint) (*dbpkg.AgentInstanceCheckpoint, bool, error) {
	checkpoint.HeadTaskID = "task-1"
	checkpoint.HistorySequence = 7
	checkpoint.SnapshotAtespace = "team-a"
	checkpoint.SnapshotName = "snapshot-1"
	checkpoint.SnapshotUID = "snapshot-uid"
	checkpoint.State = "CREATING"
	checkpoint.CreatedAt = time.Now()
	s.prepared = &checkpoint
	return &checkpoint, true, nil
}

func (s *testStore) MarkAgentInstanceCheckpointReady(_ context.Context, id, tagUID string) (*dbpkg.AgentInstanceCheckpoint, error) {
	s.prepared.State, s.prepared.TagUID = "READY", tagUID
	return s.prepared, nil
}

func (s *testStore) MarkAgentInstanceCheckpointFailed(_ context.Context, _ string, failure string) error {
	s.failed = failure
	return nil
}

func (*testStore) GetAgentInstanceCheckpoint(context.Context, string, string, string) (*dbpkg.AgentInstanceCheckpoint, error) {
	return nil, dbpkg.ErrNotFound
}

func (*testStore) ListAgentInstanceCheckpoints(context.Context, string, string, string, string, int) ([]dbpkg.AgentInstanceCheckpoint, error) {
	return nil, nil
}

func (*testStore) DeleteAgentInstanceCheckpoint(context.Context, string, string, string) error {
	return nil
}

type testTags struct {
	snapshotUID string
	created     *ateapipb.ActorSnapshotTag
}

func (t *testTags) GetActorSnapshot(context.Context, string, string) (*ateapipb.ActorSnapshot, error) {
	return &ateapipb.ActorSnapshot{Metadata: &ateapipb.ResourceMetadata{
		Atespace: "team-a", Name: "snapshot-1", Uid: t.snapshotUID,
	}}, nil
}

func (t *testTags) GetActorSnapshotTag(context.Context, string, string) (*ateapipb.ActorSnapshotTag, error) {
	return t.created, nil
}

func (t *testTags) CreateActorSnapshotTag(_ context.Context, atespace, name, snapshotName string) (*ateapipb.ActorSnapshotTag, error) {
	t.created = &ateapipb.ActorSnapshotTag{
		Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: name, Uid: "tag-uid"},
		Snapshot: &ateapipb.ObjectRef{Atespace: atespace, Name: snapshotName},
		Scope:    ateapipb.ActorSnapshotTagScope_ACTOR_SNAPSHOT_TAG_SCOPE_ATESPACE,
	}
	return t.created, nil
}

func (*testTags) DeleteActorSnapshotTag(context.Context, string, string) error { return nil }

func TestCreateTagsRecordedSnapshotBoundary(t *testing.T) {
	store := &testStore{}
	tags := &testTags{snapshotUID: "snapshot-uid"}
	service := NewService(store, testAuthorizer{}, tags)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	checkpoint, err := service.Create(ctx, "team-a", "018f47a2-4efb-7c21-a848-123456789abc", "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.GetHeadTaskId() != "task-1" || checkpoint.GetHistorySequence() != 7 || checkpoint.GetState().String() != "CHECKPOINT_STATE_READY" {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
	if tags.created.GetSnapshot().GetName() != "snapshot-1" || store.prepared.TagUID != "tag-uid" {
		t.Fatalf("tag does not retain recorded snapshot: %+v", tags.created)
	}
}
