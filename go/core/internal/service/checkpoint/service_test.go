package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	finalizeErr error
}

func (s *testStore) ReserveAgentInstanceCheckpoint(_ context.Context, checkpoint *apiv1alpha1.Checkpoint, _, _ string) (*apiv1alpha1.Checkpoint, *database.AgentInstanceTaskSnapshot, error) {
	if s.prepared != nil {
		return s.prepared, s.snapshot, nil
	}
	checkpoint.HeadTaskId = "task-1"
	checkpoint.HistorySequence = 7
	s.snapshot = &database.AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", ContentScope: "DATA"}
	checkpoint.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_CREATING
	checkpoint.CreatedAt = timestamppb.Now()
	s.prepared = checkpoint
	return checkpoint, s.snapshot, nil
}

func (s *testStore) FinalizeAgentInstanceCheckpoint(_ context.Context, _ string, tagUID, snapshotURI, failure string) (*apiv1alpha1.Checkpoint, error) {
	if s.finalizeErr != nil {
		return nil, s.finalizeErr
	}
	if failure != "" {
		s.prepared.State, s.failed = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_FAILED, failure
	} else {
		s.prepared.State, s.tagUID = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY, tagUID
		s.snapshot.URI = snapshotURI
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

func (s *testStore) BeginDeleteAgentInstanceCheckpoint(context.Context, string, string) (*database.AgentInstanceTaskSnapshot, string, error) {
	if s.prepared == nil {
		return nil, "", database.ErrNotFound
	}
	s.prepared.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING
	return s.snapshot, s.tagUID, nil
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
	createCalls            int
	snapshotURI            string
	snapshotURIAfterCreate string
	created                *ateapipb.Tag
	deleteCalls            int
	getErr                 error
	createErr              error
	deleteErr              error
	actorErr               error
	mutateTag              func(*ateapipb.Tag)
}

func (t *testTags) GetActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if t.actorErr != nil {
		return nil, t.actorErr
	}
	uri := t.snapshotURI
	if t.created != nil && t.snapshotURIAfterCreate != "" {
		uri = t.snapshotURIAfterCreate
	}
	return &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: name, Uid: "actor-uid"},
		Status: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: uri, ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}},
	}, nil
}

func (t *testTags) GetTag(context.Context, string, string) (*ateapipb.Tag, error) {
	if t.getErr != nil {
		return nil, t.getErr
	}
	if t.created == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	return t.created, nil
}

func (t *testTags) CreateTag(_ context.Context, atespace, name, actorName string) (*ateapipb.Tag, error) {
	t.createCalls++
	if t.created != nil {
		return nil, status.Error(codes.AlreadyExists, "exists")
	}
	t.created = &ateapipb.Tag{
		Metadata:    &ateapipb.ResourceMetadata{Atespace: atespace, Name: name, Uid: "tag-uid"},
		SourceActor: &ateapipb.ObjectRef{Atespace: atespace, Name: actorName},
		Scope:       ateapipb.TagScope_TAG_SCOPE_ATESPACE,
		Status: &ateapipb.TagStatus{SourceActorUid: "actor-uid", ActorTemplateUid: "template-uid",
			Snapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://tags/checkpoint", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}},
	}
	if t.mutateTag != nil {
		t.mutateTag(t.created)
	}
	return t.created, t.createErr
}

func (t *testTags) DeleteTag(context.Context, string, string) error {
	t.deleteCalls++
	if t.deleteErr != nil {
		return t.deleteErr
	}
	t.created = nil
	return nil
}

func TestCreateTagsRecordedSnapshotBoundary(t *testing.T) {
	store := &testStore{snapshotErr: fmt.Errorf("creation must use the reserved snapshot")}
	tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1"}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	checkpoint, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.GetHeadTaskId() != "task-1" || checkpoint.GetHistorySequence() != 7 || checkpoint.GetState().String() != "CHECKPOINT_STATE_READY" {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
	if tags.created.GetSourceActor().GetName() != substrate.ActorName(checkpoint.GetAgentInstanceId()) ||
		store.snapshot.URI != "s3://tags/checkpoint" || store.tagUID != "tag-uid" {
		t.Fatalf("tag does not retain recorded snapshot: %+v", tags.created)
	}
}

func TestCreateCleansTagBeforeFailing(t *testing.T) {
	store := &testStore{}
	tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1", snapshotURIAfterCreate: "s3://snapshots/snapshot-2"}
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
	store := &testStore{prepared: checkpoint, snapshot: &database.AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1"}, tagUID: "tag-uid"}
	tags := &testTags{created: &ateapipb.Tag{
		Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: tagName(checkpoint.GetId()), Uid: "tag-uid"},
		Status:   &ateapipb.TagStatus{Snapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/snapshot-1", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}},
	}}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	store.snapshotErr = fmt.Errorf("deletion must use the snapshot returned by BeginDelete")
	tags.getErr = fmt.Errorf("tag lookup unavailable")
	if err := service.Delete(ctx, checkpoint.GetId()); err == nil {
		t.Fatal("Delete() succeeded without verifying the snapshot tag")
	}
	if checkpoint.State != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING || tags.deleteCalls != 0 || store.deleted {
		t.Fatal("tag lookup failure must leave deletion pending without deleting the tag")
	}
	tags.getErr = nil
	if err := service.Delete(ctx, checkpoint.GetId()); err != nil {
		t.Fatal(err)
	}
	if checkpoint.State != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING || tags.deleteCalls != 1 || !store.deleted {
		t.Fatalf("checkpoint state = %s, tag deletes = %d, row deleted = %v", checkpoint.State, tags.deleteCalls, store.deleted)
	}
}
func TestForkCreatesAgentInstanceFromCheckpoint(t *testing.T) {
	checkpoint := &apiv1alpha1.Checkpoint{Id: "018f47a2-4efb-7c21-a848-123456789abc", State: apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY}
	store := &testStore{prepared: checkpoint, snapshot: &database.AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", ContentScope: "DATA"}}
	workflow := &testWorkflow{}
	store.tagUID = "tag-uid"
	tags := &testTags{created: &ateapipb.Tag{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: tagName(checkpoint.Id), Uid: store.tagUID}, Status: &ateapipb.TagStatus{Snapshot: &ateapipb.ExternalSnapshot{SnapshotUri: store.snapshot.URI, ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}}}, actorErr: errors.New("source Actor was deleted")}
	service := NewService(store, testAuthorizer{}, tags, workflow)
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

func TestCreateRetriesRetainedTagAfterPublishFailure(t *testing.T) {
	store := &testStore{finalizeErr: errors.New("database unavailable")}
	tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1", createErr: status.Error(codes.DeadlineExceeded, "response lost")}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	_, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1")
	require.Error(t, err)
	require.Equal(t, apiv1alpha1.CheckpointState_CHECKPOINT_STATE_CREATING, store.prepared.State)
	require.Equal(t, 0, tags.deleteCalls)
	retained := tags.created
	store.finalizeErr = nil
	checkpoint, err := service.Create(ctx, store.prepared.AgentInstanceId, "request-1")
	require.NoError(t, err)
	require.Same(t, retained, tags.created)
	require.Equal(t, apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY, checkpoint.State)
	require.Equal(t, "s3://tags/checkpoint", store.snapshot.URI)
}

func TestCreateRejectsInvalidTagAndKeepsCleanupRetryable(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ateapipb.Tag)
	}{
		{"incomplete copy", func(tag *ateapipb.Tag) { tag.Status.Snapshot = nil }},
		{"wrong source", func(tag *ateapipb.Tag) { tag.Status.SourceActorUid = "other" }},
		{"wrong scope", func(tag *ateapipb.Tag) {
			tag.Status.Snapshot.ContentScope = ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &testStore{}
			tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1", mutateTag: tc.mutate, deleteErr: errors.New("cleanup unavailable")}
			service := NewService(store, testAuthorizer{}, tags, nil)
			ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			_, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1")
			require.Error(t, err)
			require.Equal(t, apiv1alpha1.CheckpointState_CHECKPOINT_STATE_CREATING, store.prepared.State)
			tags.deleteErr = nil
			_, err = service.Create(ctx, store.prepared.AgentInstanceId, "request-1")
			require.Error(t, err)
			require.Equal(t, apiv1alpha1.CheckpointState_CHECKPOINT_STATE_FAILED, store.prepared.State)
			require.Nil(t, tags.created)
		})
	}
}

func TestForkRejectsReplacedTag(t *testing.T) {
	checkpoint := &apiv1alpha1.Checkpoint{Id: "018f47a2-4efb-7c21-a848-123456789abc"}
	snapshot := &database.AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://tags/checkpoint", ContentScope: "DATA"}
	for _, tc := range []struct{ name, uid, uri string }{
		{"recreated tag", "other-uid", snapshot.URI},
		{"different snapshot", "tag-uid", "s3://tags/other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testStore{prepared: checkpoint, snapshot: snapshot, tagUID: "tag-uid"}
			tags := &testTags{created: &ateapipb.Tag{
				Metadata: &ateapipb.ResourceMetadata{Atespace: snapshot.Atespace, Name: tagName(checkpoint.Id), Uid: tc.uid},
				Status:   &ateapipb.TagStatus{Snapshot: &ateapipb.ExternalSnapshot{SnapshotUri: tc.uri, ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}},
			}}
			service := NewService(store, testAuthorizer{}, tags, &testWorkflow{})
			ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			_, err := service.Fork(ctx, checkpoint.Id, "fork-request")
			require.Error(t, err)
			require.Nil(t, store.forked)
		})
	}
}

func TestConcurrentCreateRetainsOneTag(t *testing.T) {
	store := &testStore{}
	tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1", mutateTag: func(*ateapipb.Tag) { runtime.Gosched() }}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	start := make(chan struct{})
	results := make(chan error, 32)
	for range cap(results) {
		go func() {
			<-start
			_, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1")
			results <- err
		}()
	}
	close(start)
	for range cap(results) {
		require.NoError(t, <-results)
	}
	require.Equal(t, 1, tags.createCalls)
	require.Equal(t, 0, tags.deleteCalls)
	require.Equal(t, "s3://tags/checkpoint", store.snapshot.URI)
}
