package checkpoint

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type testSession struct{ userID string }

func (s testSession) Principal() auth.Principal { return auth.Principal{User: auth.User{ID: s.userID}} }

type testAuthorizer struct{}

func (testAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return nil
}

type testStore struct {
	prepared    *apiv1alpha1.Checkpoint
	snapshot    *database.AgentInstanceTaskSnapshot
	tagUID      string
	snapshotErr error
	forked      *apiv1alpha1.AgentInstance
	failed      string
	deleted     bool
}

func (s *testStore) ReserveAgentInstanceCheckpoint(_ context.Context, checkpoint *apiv1alpha1.Checkpoint, _, _ string) (*apiv1alpha1.Checkpoint, error) {
	checkpoint.HeadTaskId = "task-1"
	checkpoint.HistorySequence = 7
	s.snapshot = &database.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-1", UID: "snapshot-uid", ContentScope: "DATA"}
	checkpoint.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_CREATING
	checkpoint.CreatedAt = timestamppb.Now()
	s.prepared = checkpoint
	return checkpoint, nil
}

func (s *testStore) FinalizeAgentInstanceCheckpoint(_ context.Context, _ string, tagUID, failure string) (*apiv1alpha1.Checkpoint, error) {
	if failure != "" {
		s.prepared.State, s.failed = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_FAILED, failure
	} else {
		s.prepared.State, s.tagUID = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY, tagUID
	}
	return s.prepared, nil
}

func (s *testStore) GetAgentInstanceCheckpoint(context.Context, string, string) (*apiv1alpha1.Checkpoint, error) {
	if s.prepared == nil {
		return nil, database.ErrNotFound
	}
	return s.prepared, nil
}

func (s *testStore) GetAgentInstanceCheckpointSnapshot(context.Context, string, string) (*database.AgentInstanceTaskSnapshot, string, error) {
	if s.snapshotErr != nil {
		return nil, "", s.snapshotErr
	}
	if s.prepared == nil {
		return nil, "", database.ErrNotFound
	}
	return s.snapshot, s.tagUID, nil
}

func (*testStore) ListAgentInstanceCheckpoints(context.Context, string, string, string, int) ([]*apiv1alpha1.Checkpoint, error) {
	return nil, nil
}

func (s *testStore) BeginDeleteAgentInstanceCheckpoint(context.Context, string, string) (*apiv1alpha1.Checkpoint, error) {
	if s.prepared == nil {
		return nil, database.ErrNotFound
	}
	s.prepared.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING
	return s.prepared, nil
}

func (s *testStore) DeleteAgentInstanceCheckpoint(context.Context, string, string) error {
	s.deleted = true
	return nil
}

func (s *testStore) ForkAgentInstance(_ context.Context, _ string, userID, _ string, instanceID string) (*apiv1alpha1.AgentInstance, bool, error) {
	if s.forked == nil {
		s.forked = &apiv1alpha1.AgentInstance{
			Id: instanceID, Creator: userID,
			State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING,
		}
		return s.forked, true, nil
	}
	return s.forked, false, nil
}

type testWorkflow struct {
	snapshot *database.AgentInstanceTaskSnapshot
	tagName  string
}

func (w *testWorkflow) Fork(_ context.Context, instance *apiv1alpha1.AgentInstance, snapshot *database.AgentInstanceTaskSnapshot, tagName string) (*apiv1alpha1.AgentInstance, error) {
	w.snapshot, w.tagName = snapshot, tagName
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
	return instance, nil
}

type testTags struct {
	snapshotUID            string
	snapshotUIDAfterCreate string
	created                *ateapipb.ActorSnapshotTag
	deleteCalls            int
}

func (t *testTags) GetActorSnapshot(context.Context, string, string) (*ateapipb.ActorSnapshot, error) {
	uid := t.snapshotUID
	if t.created != nil && t.snapshotUIDAfterCreate != "" {
		uid = t.snapshotUIDAfterCreate
	}
	return &ateapipb.ActorSnapshot{
		Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "snapshot-1", Uid: uid},
		Status:   &ateapipb.ActorSnapshotStatus{ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA},
	}, nil
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

func (t *testTags) DeleteActorSnapshotTag(context.Context, string, string) error {
	t.deleteCalls++
	return nil
}

func TestCreateTagsRecordedSnapshotBoundary(t *testing.T) {
	store := &testStore{}
	tags := &testTags{snapshotUID: "snapshot-uid"}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	checkpoint, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.GetHeadTaskId() != "task-1" || checkpoint.GetHistorySequence() != 7 || checkpoint.GetState().String() != "CHECKPOINT_STATE_READY" {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
	if tags.created.GetSnapshot().GetName() != "snapshot-1" || store.tagUID != "tag-uid" {
		t.Fatalf("tag does not retain recorded snapshot: %+v", tags.created)
	}
}

func TestCreateCleansTagBeforeFailing(t *testing.T) {
	store := &testStore{}
	tags := &testTags{snapshotUID: "snapshot-uid", snapshotUIDAfterCreate: "changed-snapshot-uid"}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	if _, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1"); err == nil {
		t.Fatal("Create() succeeded after snapshot identity changed")
	}
	if tags.deleteCalls != 1 || store.failed == "" {
		t.Fatalf("cleanup calls = %d, failure = %q", tags.deleteCalls, store.failed)
	}
}

func TestDeleteHidesCheckpointBeforeDeletingTag(t *testing.T) {
	checkpoint := &apiv1alpha1.Checkpoint{Id: "018f47a2-4efb-7c21-a848-123456789abc", State: apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY}
	store := &testStore{prepared: checkpoint, snapshot: &database.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-1"}, tagUID: "tag-uid"}
	tags := &testTags{created: &ateapipb.ActorSnapshotTag{
		Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: tagName(checkpoint.GetId()), Uid: "tag-uid"},
		Snapshot: &ateapipb.ObjectRef{Atespace: "team-a", Name: "snapshot-1"},
	}}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	store.snapshotErr = fmt.Errorf("snapshot lookup unavailable")
	if err := service.Delete(ctx, checkpoint.GetId()); err == nil {
		t.Fatal("Delete() succeeded without the snapshot reference")
	}
	if checkpoint.State != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING || tags.deleteCalls != 0 || store.deleted {
		t.Fatal("snapshot lookup failure must leave deletion pending without deleting the tag")
	}
	store.snapshotErr = nil
	if err := service.Delete(ctx, checkpoint.GetId()); err != nil {
		t.Fatal(err)
	}
	if checkpoint.State != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING || tags.deleteCalls != 1 || !store.deleted {
		t.Fatalf("checkpoint state = %s, tag deletes = %d, row deleted = %v", checkpoint.State, tags.deleteCalls, store.deleted)
	}
}
func TestForkCreatesAgentInstanceFromCheckpoint(t *testing.T) {
	checkpoint := &apiv1alpha1.Checkpoint{Id: "018f47a2-4efb-7c21-a848-123456789abc", State: apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY}
	store := &testStore{prepared: checkpoint, snapshot: &database.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-1", UID: "snapshot-uid", ContentScope: "DATA"}}
	workflow := &testWorkflow{}
	service := NewService(store, testAuthorizer{}, &testTags{}, workflow)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	instance, err := service.Fork(ctx, checkpoint.GetId(), "fork-request")
	if err != nil {
		t.Fatal(err)
	}
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY ||
		workflow.snapshot != store.snapshot || workflow.tagName != tagName(checkpoint.Id) || store.forked.GetId() == "" {
		t.Fatalf("fork = %+v, checkpoint = %+v", instance, workflow.snapshot)
	}
}
