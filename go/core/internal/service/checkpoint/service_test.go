package checkpoint

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type testSession struct{ userID string }

func (s testSession) Principal() auth.Principal { return auth.Principal{ID: s.userID} }

type testAuthorizer struct{}

func (testAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return nil
}

type testStore struct {
	sourceErr       error
	checkpointOwner string
	forkSourceReads int
	reservations    int
	prepared        *apiv1.Checkpoint
	snapshot        *database.SessionTaskSnapshot
	tagUID          string
	forkSourceErr   error
	forked          *apiv1.Session
	failed          string
	deleted         bool
	finalizeErr     error
	reserveErr      error
}

func (s *testStore) GetSessionCheckpointSource(_ context.Context, id string) (*apiv1.Session, error) {
	if s.sourceErr != nil && s.prepared == nil {
		return nil, s.sourceErr
	}
	owner := s.checkpointOwner
	if owner == "" {
		owner = "alice"
	}
	return &apiv1.Session{Id: id, Creator: owner}, nil
}

func (s *testStore) GetCheckpointSourceSession(ctx context.Context, _ string) (*apiv1.Session, error) {
	if s.prepared == nil {
		return nil, database.ErrNotFound
	}
	return s.GetSessionCheckpointSource(ctx, s.prepared.SessionId)
}

func (s *testStore) ReserveSessionCheckpoint(_ context.Context, checkpoint *apiv1.Checkpoint, owner, _ string) (*apiv1.Checkpoint, *database.SessionTaskSnapshot, error) {
	s.reservations++
	if s.reserveErr != nil {
		return nil, nil, s.reserveErr
	}
	if s.checkpointOwner != "" && s.checkpointOwner != owner {
		return nil, nil, database.ErrNotFound
	}
	if s.prepared != nil {
		return s.prepared, s.snapshot, nil
	}
	if s.sourceErr != nil {
		return nil, nil, s.sourceErr
	}
	if s.checkpointOwner == "" {
		s.checkpointOwner = owner
	}
	checkpoint.HeadTaskId = "task-1"
	checkpoint.HistorySequence = 7
	s.snapshot = &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}
	checkpoint.State = apiv1.CheckpointState_CHECKPOINT_STATE_CREATING
	checkpoint.CreatedAt = timestamppb.Now()
	s.prepared = checkpoint
	return checkpoint, s.snapshot, nil
}

func (s *testStore) FinalizeSessionCheckpoint(_ context.Context, _ string, tagUID, snapshotURI, failure string) (*apiv1.Checkpoint, error) {
	if s.finalizeErr != nil {
		return nil, s.finalizeErr
	}
	if failure != "" {
		s.prepared.State, s.failed = apiv1.CheckpointState_CHECKPOINT_STATE_FAILED, failure
	} else {
		s.prepared.State, s.tagUID = apiv1.CheckpointState_CHECKPOINT_STATE_READY, tagUID
		s.snapshot.URI = snapshotURI
	}
	return s.prepared, nil
}

func (s *testStore) GetSessionCheckpoint(_ context.Context, _ string, owner string) (*apiv1.Checkpoint, error) {
	if s.prepared == nil || (s.checkpointOwner != "" && s.checkpointOwner != owner) {
		return nil, database.ErrNotFound
	}
	return s.prepared, nil
}

func (s *testStore) GetSessionCheckpointForkSource(ctx context.Context, id, owner string) (*database.SessionCheckpointForkSource, error) {
	s.forkSourceReads++
	checkpoint, err := s.GetSessionCheckpoint(ctx, id, owner)
	if err != nil {
		return nil, err
	}
	if s.forkSourceErr != nil {
		return nil, s.forkSourceErr
	}
	return &database.SessionCheckpointForkSource{
		Checkpoint: checkpoint,
		Agent:      &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"},
		Snapshot:   s.snapshot, TagUID: s.tagUID,
	}, nil
}

func (*testStore) ListSessionCheckpoints(context.Context, string, string, string, int) ([]*apiv1.Checkpoint, error) {
	return nil, nil
}

func (s *testStore) BeginDeleteSessionCheckpoint(context.Context, string, string) (*database.SessionTaskSnapshot, string, error) {
	if s.prepared == nil {
		return nil, "", database.ErrNotFound
	}
	s.prepared.State = apiv1.CheckpointState_CHECKPOINT_STATE_DELETING
	return s.snapshot, s.tagUID, nil
}

func (s *testStore) DeleteSessionCheckpoint(context.Context, string, string) error {
	s.deleted = true
	return nil
}

func (s *testStore) UpdateCheckpointName(_ context.Context, _, _, name string) (*apiv1.Checkpoint, error) {
	if s.prepared == nil {
		return nil, database.ErrNotFound
	}
	s.prepared.Name = name
	return s.prepared, nil
}

func (s *testStore) ForkSession(_ context.Context, _ string, _, userID, _ string, sessionID string) (*apiv1.Session, bool, error) {
	if s.forked == nil {
		s.forked = &apiv1.Session{
			Id: sessionID, Creator: userID,
			State: apiv1.RuntimeState_RUNTIME_STATE_CREATING,
		}
		return s.forked, true, nil
	}
	return s.forked, false, nil
}

type testWorkflow struct {
	session *apiv1.Session
}

func (w *testWorkflow) Create(_ context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	w.session = session
	session.State = apiv1.RuntimeState_RUNTIME_STATE_READY
	return session, nil
}

type testTags struct {
	getCalls               int
	createCalls            int
	snapshotURI            string
	snapshotURIAfterCreate string
	actorUIDAfterCreate    string
	created                *ateapipb.Tag
	deleteCalls            int
	getErr                 error
	createErr              error
	deleteErr              error
	actorErr               error
	mutateTag              func(*ateapipb.Tag)
	beforeDelete           func()
}

func (t *testTags) GetActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if t.actorErr != nil {
		return nil, t.actorErr
	}
	uri := t.snapshotURI
	if t.created != nil && t.snapshotURIAfterCreate != "" {
		uri = t.snapshotURIAfterCreate
	}
	actorUID := "actor-uid"
	if t.created != nil && t.actorUIDAfterCreate != "" {
		actorUID = t.actorUIDAfterCreate
	}
	return &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: name, Uid: actorUID},
		Status: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			Snapshots: []*ateapipb.Snapshot{{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: "template-uid", DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: uri}}}}},
	}, nil
}

func (t *testTags) GetTag(context.Context, string, string) (*ateapipb.Tag, error) {
	t.getCalls++
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
		Status: &ateapipb.TagStatus{ActorTemplateUid: "template-uid",
			Snapshot: &ateapipb.Snapshot{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: "template-uid", DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://tags/checkpoint"}}}},
	}
	if t.mutateTag != nil {
		t.mutateTag(t.created)
	}
	return t.created, t.createErr
}

func (t *testTags) DeleteTag(_ context.Context, _, _, uid string) error {
	if t.beforeDelete != nil {
		t.beforeDelete()
	}
	t.deleteCalls++
	if t.deleteErr != nil {
		return t.deleteErr
	}
	if uid != "" && t.created.GetMetadata().GetUid() != uid {
		return status.Error(codes.Aborted, "tag UID changed")
	}
	t.created = nil
	return nil
}

func TestCreatePreservesStoreConflictReason(t *testing.T) {
	for _, cause := range []error{database.ErrConflict, database.ErrFailedPrecondition} {
		t.Run(cause.Error(), func(t *testing.T) {
			storeErr := fmt.Errorf("Session cannot checkpoint in its current state: %w", cause)
			service := NewService(&testStore{reserveErr: storeErr}, testAuthorizer{}, nil, nil)
			ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			_, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1", "task-1")
			require.Equal(t, serviceerrors.CodeFailedPrecondition, serviceerrors.CodeOf(err))
			require.Equal(t, storeErr.Error(), serviceerrors.MessageOf(err))
			require.ErrorIs(t, err, cause)
		})
	}
}

func TestCreateDistinguishesPendingSnapshotFromAdvancedConversation(t *testing.T) {
	for _, test := range []struct {
		err    error
		reason string
	}{
		{database.ErrSnapshotPending, "KAGENT_CHECKPOINT_SNAPSHOT_PENDING"},
		{database.ErrCheckpointAdvanced, "KAGENT_CHECKPOINT_CONVERSATION_ADVANCED"},
	} {
		service := NewService(&testStore{reserveErr: test.err}, testAuthorizer{}, &testTags{}, &testWorkflow{})
		ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
		_, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request", "task-a")
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
		details := status.Convert(err).Details()
		require.Len(t, details, 1)
		info := details[0].(*errdetails.ErrorInfo)
		require.Equal(t, test.reason, info.Reason)
		require.Equal(t, "kagent.dev", info.Domain)
	}
}

func TestRenameValidatesBeforeReachingTheStore(t *testing.T) {
	store := &testStore{}
	service := NewService(store, testAuthorizer{}, nil, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	const checkpointID = "018f47a2-4efb-7c21-a848-123456789abc"

	_, err := service.Rename(ctx, "not-a-uuid", "Before the detour")
	require.Equal(t, serviceerrors.CodeInvalidArgument, serviceerrors.CodeOf(err))

	_, err = service.Rename(ctx, checkpointID, "Before the detour")
	require.Equal(t, serviceerrors.CodeNotFound, serviceerrors.CodeOf(err))

	store.prepared = &apiv1.Checkpoint{SessionId: "11111111-1111-4111-8111-111111111111", Id: checkpointID}
	renamed, err := service.Rename(ctx, checkpointID, "Before the detour")
	require.NoError(t, err)
	require.Equal(t, "Before the detour", renamed.GetName())
}

func TestCreateTagsRecordedSnapshotBoundary(t *testing.T) {
	store := &testStore{forkSourceErr: fmt.Errorf("creation must use the reserved snapshot")}
	tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1"}
	service := NewService(store, &restrictiveAuthorizer{resource: "Session", verb: auth.VerbCreate}, tags, nil)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	checkpoint, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.GetHeadTaskId() != "task-1" || checkpoint.GetHistorySequence() != 7 || checkpoint.GetState().String() != "CHECKPOINT_STATE_READY" {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
	if tags.created.GetSourceActor().GetName() != substrate.ActorName(checkpoint.GetSessionId()) ||
		store.snapshot.URI != "s3://tags/checkpoint" || store.tagUID != "tag-uid" {
		t.Fatalf("tag does not retain recorded snapshot: %+v", tags.created)
	}
}

func TestCreateCleansTagBeforeFailing(t *testing.T) {
	for _, test := range []struct {
		name                   string
		snapshotURIAfterCreate string
		actorUIDAfterCreate    string
	}{
		{name: "snapshot changed", snapshotURIAfterCreate: "s3://snapshots/snapshot-2"},
		{name: "actor replaced", actorUIDAfterCreate: "other-actor-uid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &testStore{}
			tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1", snapshotURIAfterCreate: test.snapshotURIAfterCreate, actorUIDAfterCreate: test.actorUIDAfterCreate}
			service := NewService(store, testAuthorizer{}, tags, nil)
			ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})

			_, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1", "task-1")
			require.Error(t, err)
			require.Equal(t, 1, tags.deleteCalls)
			require.NotEmpty(t, store.failed)
		})
	}
}

func TestDeleteHidesCheckpointBeforeDeletingTag(t *testing.T) {
	checkpoint := &apiv1.Checkpoint{SessionId: "11111111-1111-4111-8111-111111111111", Id: "018f47a2-4efb-7c21-a848-123456789abc", State: apiv1.CheckpointState_CHECKPOINT_STATE_READY}
	store := &testStore{prepared: checkpoint, snapshot: &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1"}, tagUID: "tag-uid"}
	tags := &testTags{created: &ateapipb.Tag{
		Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: tagName(checkpoint.GetId()), Uid: "tag-uid"},
		Status:   &ateapipb.TagStatus{Snapshot: &ateapipb.Snapshot{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: "template-uid", DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/snapshot-1"}}}},
	}}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	store.forkSourceErr = fmt.Errorf("deletion must use the snapshot returned by BeginDelete")
	tags.getErr = fmt.Errorf("tag lookup unavailable")
	if err := service.Delete(ctx, checkpoint.GetId()); err == nil {
		t.Fatal("Delete() succeeded without verifying the snapshot tag")
	}
	if checkpoint.State != apiv1.CheckpointState_CHECKPOINT_STATE_DELETING || tags.deleteCalls != 0 || store.deleted {
		t.Fatal("tag lookup failure must leave deletion pending without deleting the tag")
	}
	tags.getErr = nil
	tags.deleteErr = status.Error(codes.Unavailable, "tag delete unavailable")
	require.Equal(t, serviceerrors.CodeUnavailable, serviceerrors.CodeOf(service.Delete(ctx, checkpoint.GetId())))
	require.False(t, store.deleted, "a failed tag delete must retain the database reference")
	require.NotNil(t, tags.created)
	tags.deleteErr = nil
	if err := service.Delete(ctx, checkpoint.GetId()); err != nil {
		t.Fatal(err)
	}
	if checkpoint.State != apiv1.CheckpointState_CHECKPOINT_STATE_DELETING || tags.deleteCalls != 2 || !store.deleted {
		t.Fatalf("checkpoint state = %s, tag deletes = %d, row deleted = %v", checkpoint.State, tags.deleteCalls, store.deleted)
	}
}

func TestDeleteFailedCheckpointWithMissingTag(t *testing.T) {
	checkpoint := &apiv1.Checkpoint{SessionId: "11111111-1111-4111-8111-111111111111", Id: "018f47a2-4efb-7c21-a848-123456789abc", State: apiv1.CheckpointState_CHECKPOINT_STATE_FAILED}
	store := &testStore{prepared: checkpoint, snapshot: &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/source"}}
	// Creation either never made a tag or already compensated it. Once absence is
	// established, deletion must not issue another unverified delete by name.
	tags := &testTags{deleteErr: errors.New("unexpected tag deletion")}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})

	tags.getErr = status.Error(codes.Unavailable, "tag lookup unavailable")
	require.Equal(t, serviceerrors.CodeUnavailable, serviceerrors.CodeOf(service.Delete(ctx, checkpoint.GetId())))
	require.False(t, store.deleted)
	require.Zero(t, tags.deleteCalls)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_DELETING, checkpoint.State)

	// Retry through a new service instance after the backend recovers.
	tags.getErr = nil
	service = NewService(store, testAuthorizer{}, tags, nil)
	require.NoError(t, service.Delete(ctx, checkpoint.GetId()))
	require.True(t, store.deleted)
	require.Zero(t, tags.deleteCalls)
}

func TestDeleteRejectsUnverifiedTag(t *testing.T) {
	for _, tc := range []struct {
		name, retainedUID, actualUID string
	}{
		{name: "failed checkpoint with unexpected tag", actualUID: "unexpected-uid"},
		{name: "failed checkpoint with missing tag identity"},
		{name: "ready checkpoint with replacement tag", retainedUID: "tag-uid", actualUID: "replacement-uid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := apiv1.CheckpointState_CHECKPOINT_STATE_FAILED
			if tc.retainedUID != "" {
				state = apiv1.CheckpointState_CHECKPOINT_STATE_READY
			}
			checkpoint := &apiv1.Checkpoint{SessionId: "11111111-1111-4111-8111-111111111111", Id: "018f47a2-4efb-7c21-a848-123456789abc", State: state}
			snapshot := &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/source"}
			store := &testStore{prepared: checkpoint, snapshot: snapshot, tagUID: tc.retainedUID}
			tags := &testTags{created: &ateapipb.Tag{
				Metadata: &ateapipb.ResourceMetadata{Atespace: snapshot.Atespace, Name: tagName(checkpoint.GetId()), Uid: tc.actualUID},
				Status:   &ateapipb.TagStatus{Snapshot: &ateapipb.Snapshot{DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: snapshot.URI}}}},
			}}
			service := NewService(store, testAuthorizer{}, tags, nil)
			ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			require.Equal(t, serviceerrors.CodeFailedPrecondition, serviceerrors.CodeOf(service.Delete(ctx, checkpoint.GetId())))
			require.False(t, store.deleted)
			require.Zero(t, tags.deleteCalls)
		})
	}
}

func TestForkCreatesSessionFromRetainedCheckpointAfterSourceDeletion(t *testing.T) {
	checkpoint := &apiv1.Checkpoint{SessionId: "11111111-1111-4111-8111-111111111111", Id: "018f47a2-4efb-7c21-a848-123456789abc", State: apiv1.CheckpointState_CHECKPOINT_STATE_READY}
	store := &testStore{sourceErr: database.ErrNotFound, checkpointOwner: "alice", prepared: checkpoint, snapshot: &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}}
	workflow := &testWorkflow{}
	store.tagUID = "tag-uid"
	tags := &testTags{created: &ateapipb.Tag{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: tagName(checkpoint.Id), Uid: store.tagUID}, Status: &ateapipb.TagStatus{Snapshot: &ateapipb.Snapshot{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: "template-uid", DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: store.snapshot.URI}}}}}, actorErr: errors.New("source Actor was deleted")}
	service := NewService(store, &restrictiveAuthorizer{resource: "Checkpoint", verb: auth.VerbCreate}, tags, workflow)
	ctx := auth.AuthSessionTo(context.Background(), testSession{userID: "alice"})

	session, err := service.Fork(ctx, checkpoint.GetId(), "fork-request")
	if err != nil {
		t.Fatal(err)
	}
	if session.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY ||
		workflow.session != store.forked || store.forked.GetId() == "" {
		t.Fatalf("fork = %+v, checkpoint = %+v", session, checkpoint)
	}
	retried, err := service.Fork(ctx, checkpoint.GetId(), "fork-request")
	require.NoError(t, err)
	require.Equal(t, session.GetId(), retried.GetId())
}

func TestCreateRetriesRetainedTagAfterPublishFailure(t *testing.T) {
	store := &testStore{finalizeErr: errors.New("database unavailable")}
	tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1", createErr: status.Error(codes.DeadlineExceeded, "response lost")}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	_, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1", "task-1")
	require.Error(t, err)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, store.prepared.State)
	require.Equal(t, 0, tags.deleteCalls)
	retained := tags.created
	store.finalizeErr = nil
	checkpoint, err := service.Create(ctx, store.prepared.SessionId, "request-1", "task-1")
	require.NoError(t, err)
	require.Same(t, retained, tags.created)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, checkpoint.State)
	require.Equal(t, "s3://tags/checkpoint", store.snapshot.URI)
}

func TestCreateRejectsInvalidTagAndKeepsCleanupRetryable(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ateapipb.Tag)
	}{
		{"incomplete copy", func(tag *ateapipb.Tag) { tag.Status.Snapshot = nil }},
		{"upload pending", func(tag *ateapipb.Tag) {
			tag.Status.Snapshot.DurableSnapshot.Status = ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_PENDING
		}},
		{"wrong source", func(tag *ateapipb.Tag) { tag.SourceActor.Name = "other" }},
		{"wrong template", func(tag *ateapipb.Tag) { tag.Status.ActorTemplateUid = "other" }},
		{"missing template", func(tag *ateapipb.Tag) { tag.Status.ActorTemplateUid = "" }},
		{"wrong scope", func(tag *ateapipb.Tag) {
			tag.Status.Snapshot.Fidelity = ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_MEMORY
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &testStore{}
			tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1", mutateTag: tc.mutate, deleteErr: errors.New("cleanup unavailable")}
			service := NewService(store, testAuthorizer{}, tags, nil)
			ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			_, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1", "task-1")
			require.Error(t, err)
			require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, store.prepared.State)
			tags.deleteErr = nil
			_, err = service.Create(ctx, store.prepared.SessionId, "request-1", "task-1")
			require.Error(t, err)
			require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED, store.prepared.State)
			require.Nil(t, tags.created)
		})
	}
}

func TestForkRejectsReplacedTag(t *testing.T) {
	checkpoint := &apiv1.Checkpoint{SessionId: "11111111-1111-4111-8111-111111111111", Id: "018f47a2-4efb-7c21-a848-123456789abc"}
	snapshot := &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://tags/checkpoint", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}
	for _, tc := range []struct{ name, uid, uri string }{
		{"recreated tag", "other-uid", snapshot.URI},
		{"different snapshot", "tag-uid", "s3://tags/other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testStore{prepared: checkpoint, snapshot: snapshot, tagUID: "tag-uid"}
			tags := &testTags{created: &ateapipb.Tag{
				Metadata: &ateapipb.ResourceMetadata{Atespace: snapshot.Atespace, Name: tagName(checkpoint.Id), Uid: tc.uid},
				Status:   &ateapipb.TagStatus{Snapshot: &ateapipb.Snapshot{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: "template-uid", DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: tc.uri}}}},
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
	store := &testStore{checkpointOwner: "alice"}
	tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1", mutateTag: func(*ateapipb.Tag) { runtime.Gosched() }}
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	start := make(chan struct{})
	results := make(chan error, 32)
	for range cap(results) {
		go func() {
			<-start
			_, err := service.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1", "task-1")
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

func TestDeletePreservesTagReplacedAfterLookup(t *testing.T) {
	checkpoint := &apiv1.Checkpoint{SessionId: "11111111-1111-4111-8111-111111111111", Id: "018f47a2-4efb-7c21-a848-123456789abc", State: apiv1.CheckpointState_CHECKPOINT_STATE_READY}
	store := &testStore{prepared: checkpoint, snapshot: &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1"}, tagUID: "tag-uid"}
	tags := &testTags{created: &ateapipb.Tag{
		Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: tagName(checkpoint.GetId()), Uid: "tag-uid"},
		Status:   &ateapipb.TagStatus{Snapshot: &ateapipb.Snapshot{DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: store.snapshot.URI}}}},
	}}
	tags.beforeDelete = func() { tags.created.Metadata.Uid = "replacement-uid" }
	service := NewService(store, testAuthorizer{}, tags, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})

	require.Error(t, service.Delete(ctx, checkpoint.GetId()))
	require.Equal(t, 1, tags.deleteCalls)
	require.Equal(t, "replacement-uid", tags.created.GetMetadata().GetUid(), "replacement snapshot must survive")
	require.False(t, store.deleted, "retain the checkpoint reference after a rejected deletion")
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_DELETING, checkpoint.State)

	tags.beforeDelete = nil
	require.Equal(t, serviceerrors.CodeFailedPrecondition, serviceerrors.CodeOf(service.Delete(ctx, checkpoint.GetId())))
	require.Equal(t, 1, tags.deleteCalls, "retry must reject the replacement at lookup")
	tags.created = nil
	require.NoError(t, service.Delete(ctx, checkpoint.GetId()))
	require.True(t, store.deleted, "cleanup can finish once the tag is absent")
}

type checkpointAuthorization struct {
	verb     auth.Verb
	resource auth.Resource
}

type restrictiveAuthorizer struct {
	resource string
	verb     auth.Verb
	calls    []checkpointAuthorization
}

func (a *restrictiveAuthorizer) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	a.calls = append(a.calls, checkpointAuthorization{verb: verb, resource: resource})
	if resource.Type == a.resource && verb == a.verb {
		return errors.New("denied")
	}
	return nil
}

func TestCheckpointComposesPermissionsBeforeEffects(t *testing.T) {
	for _, test := range []struct {
		name, operation, resource string
		verb                      auth.Verb
		ownerDenied               bool
	}{
		{name: "create category", operation: "create", resource: "Checkpoint", verb: auth.VerbCreate},
		{name: "create source read", operation: "create", resource: "Session", verb: auth.VerbGet},
		{name: "create source owner", operation: "create", ownerDenied: true},
		{name: "fork checkpoint read", operation: "fork", resource: "Checkpoint", verb: auth.VerbGet},
		{name: "fork source read", operation: "fork", resource: "Session", verb: auth.VerbGet},
		{name: "fork destination create", operation: "fork", resource: "Session", verb: auth.VerbCreate},
		{name: "fork target Agent", operation: "fork", resource: "Agent", verb: auth.VerbGet},
		{name: "fork source owner", operation: "fork", ownerDenied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sourceID := "11111111-1111-4111-8111-111111111111"
			checkpointID := "22222222-2222-4222-8222-222222222222"
			store := &testStore{prepared: &apiv1.Checkpoint{Id: checkpointID, SessionId: sourceID}}
			if test.ownerDenied {
				if test.operation == "create" {
					store.sourceErr = database.ErrNotFound
					store.prepared = nil
				} else {
					store.checkpointOwner = "bob"
				}
			}
			policy := &restrictiveAuthorizer{resource: test.resource, verb: test.verb}
			service := NewService(store, policy, nil, nil)
			ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			var err error
			if test.operation == "create" {
				_, err = service.Create(ctx, sourceID, "request", "head")
			} else {
				_, err = service.Fork(ctx, checkpointID, "request")
			}
			code := serviceerrors.CodePermissionDenied
			if test.ownerDenied {
				code = serviceerrors.CodeNotFound
			}
			require.True(t, serviceerrors.IsCode(err, code), "%v", err)
			expected := []checkpointAuthorization{{verb: auth.VerbCreate, resource: auth.Resource{Type: "Checkpoint"}}, {verb: auth.VerbGet, resource: auth.Resource{Type: "Session", Name: sourceID}}}
			if test.operation == "fork" {
				expected = []checkpointAuthorization{
					{verb: auth.VerbGet, resource: auth.Resource{Type: "Checkpoint", Name: checkpointID}},
					{verb: auth.VerbGet, resource: auth.Resource{Type: "Session", Name: sourceID}},
					{verb: auth.VerbCreate, resource: auth.Resource{Type: "Session", Namespace: "team-a"}},
					{verb: auth.VerbGet, resource: auth.Resource{Type: "Agent", Namespace: "team-a", Name: "assistant"}},
				}
			}
			if test.ownerDenied && test.operation == "fork" {
				expected = expected[:2]
			} else if !test.ownerDenied {
				for index, check := range expected {
					if check.resource.Type == test.resource && check.verb == test.verb {
						expected = expected[:index+1]
						break
					}
				}
			}
			require.Equal(t, expected, policy.calls, "every authorization must use the exact trusted identity")

			require.Zero(t, store.reservations)
			if test.operation == "fork" && !test.ownerDenied && (test.resource == "Agent" || test.resource == "Session" && test.verb == auth.VerbCreate) {
				require.Equal(t, 1, store.forkSourceReads)
			} else {
				require.Zero(t, store.forkSourceReads)
			}
			require.Nil(t, store.forked)
		})
	}
}

func TestCheckpointSharesAndCategoryDenialsProtectEveryOperation(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, shared := range []bool{false, true} {
		for _, operation := range []struct {
			name string
			verb auth.Verb
			call func(context.Context, *Service) error
		}{
			{"get", auth.VerbGet, func(ctx context.Context, s *Service) error { _, err := s.Get(ctx, id); return err }},
			{"list", auth.VerbList, func(ctx context.Context, s *Service) error {
				_, err := s.List(ctx, ListRequest{SessionID: id})
				return err
			}},
			{"rename", auth.VerbUpdate, func(ctx context.Context, s *Service) error { _, err := s.Rename(ctx, id, "name"); return err }},
			{"delete", auth.VerbDelete, func(ctx context.Context, s *Service) error { return s.Delete(ctx, id) }},
			{"create", auth.VerbCreate, func(ctx context.Context, s *Service) error {
				_, err := s.Create(ctx, id, "request", "head")
				return err
			}},
			{"fork", auth.VerbGet, func(ctx context.Context, s *Service) error { _, err := s.Fork(ctx, id, "request"); return err }},
		} {
			t.Run(fmt.Sprintf("%s/share=%v", operation.name, shared), func(t *testing.T) {
				ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
				policy := &restrictiveAuthorizer{resource: "Checkpoint", verb: operation.verb}
				if shared {
					ctx = auth.ShareContextTo(ctx, &auth.ShareContext{SessionID: id, UserID: "alice"})
					policy.resource = ""
				}
				require.True(t, serviceerrors.IsCode(operation.call(ctx, NewService(nil, policy, nil, nil)), serviceerrors.CodePermissionDenied))
			})
		}
	}
}

func TestCreateRetriesReadyCheckpointAfterSourceDeletion(t *testing.T) {
	store := &testStore{}
	tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1"}
	policy := &restrictiveAuthorizer{}
	svc := NewService(store, policy, tags, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	first, err := svc.Create(ctx, "018f47a2-4efb-7c21-a848-123456789abc", "request-1", "task-1")
	require.NoError(t, err)
	store.sourceErr = database.ErrNotFound
	retried, err := svc.Create(ctx, first.SessionId, "request-1", "task-1")
	require.NoError(t, err)
	require.Equal(t, first.Id, retried.Id)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, retried.State)
	require.Equal(t, 1, tags.createCalls)
	before := store.reservations
	policy.resource, policy.verb = "Session", auth.VerbGet
	_, err = svc.Create(ctx, first.SessionId, "request-1", "task-1")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
	require.Equal(t, before, store.reservations)
	policy.resource = ""
	_, err = svc.Create(auth.AuthSessionTo(t.Context(), testSession{userID: "bob"}), first.SessionId, "request-1", "task-1")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
}

type forkNamespacePolicy struct{ namespace string }

func (f forkNamespacePolicy) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if resource.Type == "Session" && verb == auth.VerbCreate && (resource.Namespace != f.namespace || resource.Name != "") {
		return errors.New("Session creation denied in target namespace")
	}
	return nil
}

func TestForkAuthorizesResolvedTargetNamespaceBeforeTagRead(t *testing.T) {
	for _, namespace := range []string{"team-a", "team-b"} {
		t.Run(namespace, func(t *testing.T) {
			checkpoint := &apiv1.Checkpoint{Id: "018f47a2-4efb-7c21-a848-123456789abc", SessionId: "11111111-1111-4111-8111-111111111111"}
			tagErr := errors.New("tag boundary reached")
			store := &testStore{prepared: checkpoint, checkpointOwner: "alice", sourceErr: database.ErrNotFound, snapshot: &database.SessionTaskSnapshot{Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}}
			tags := &testTags{getErr: tagErr}
			svc := NewService(store, forkNamespacePolicy{namespace: namespace}, tags, nil)
			_, err := svc.Fork(auth.AuthSessionTo(t.Context(), testSession{userID: "alice"}), checkpoint.Id, "request")
			if namespace == "team-a" {
				require.ErrorIs(t, err, tagErr, "resolved namespace should pass authorization")
				require.Equal(t, 1, tags.getCalls)
			} else {
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
				require.Zero(t, tags.getCalls)
			}
			require.Equal(t, 1, store.forkSourceReads)
			require.Nil(t, store.forked)
		})
	}
}

type exactIdentityPolicy struct {
	denied auth.Resource
	calls  []auth.Resource
}

func (e *exactIdentityPolicy) Check(_ context.Context, _ auth.Principal, _ auth.Verb, resource auth.Resource) error {
	e.calls = append(e.calls, resource)
	if resource == e.denied {
		return errors.New("identity denied")
	}
	return nil
}

func TestCheckpointCanonicalIdentityDenialsCannotBeBypassed(t *testing.T) {
	const canonical = "018f47a2-4efb-7c21-a848-123456789abc"
	for _, encoded := range []string{canonical, strings.ToUpper(canonical), strings.ReplaceAll(canonical, "-", ""), "urn:uuid:" + canonical, "{" + canonical + "}"} {
		for _, op := range []string{"create", "get", "rename", "delete", "fork"} {
			t.Run(op+"/"+encoded, func(t *testing.T) {
				resource := auth.Resource{Type: "Checkpoint", Name: canonical}
				if op == "create" {
					resource.Type = "Session"
				}
				policy := &exactIdentityPolicy{denied: resource}
				svc := NewService(nil, policy, nil, nil)
				ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
				var err error
				switch op {
				case "create":
					_, err = svc.Create(ctx, encoded, "request", "head")
				case "get":
					_, err = svc.Get(ctx, encoded)
				case "rename":
					_, err = svc.Rename(ctx, encoded, "name")
				case "delete":
					err = svc.Delete(ctx, encoded)
				case "fork":
					_, err = svc.Fork(ctx, encoded, "request")
				}
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), "%v", err)
				require.Equal(t, resource, policy.calls[len(policy.calls)-1])
			})
		}
	}
}

type canonicalCheckpointStore struct {
	*testStore
	sessionID    string
	checkpointID string
	afterID      string
}

func (c *canonicalCheckpointStore) ReserveSessionCheckpoint(ctx context.Context, value *apiv1.Checkpoint, owner, requestID string) (*apiv1.Checkpoint, *database.SessionTaskSnapshot, error) {
	c.sessionID = value.GetSessionId()
	return c.testStore.ReserveSessionCheckpoint(ctx, value, owner, requestID)
}

func (c *canonicalCheckpointStore) GetSessionCheckpoint(ctx context.Context, id, owner string) (*apiv1.Checkpoint, error) {
	c.checkpointID = id
	return c.testStore.GetSessionCheckpoint(ctx, id, owner)
}

func (c *canonicalCheckpointStore) ListSessionCheckpoints(_ context.Context, sourceID, _, afterID string, _ int) ([]*apiv1.Checkpoint, error) {
	c.sessionID, c.afterID = sourceID, afterID
	return nil, nil
}

func TestCheckpointCanonicalIdentityReachesStorageAndRetries(t *testing.T) {
	const canonical = "018f47a2-4efb-7c21-a848-123456789abc"
	const checkpointID = "018f47a2-4efb-7c21-a848-abcdefabcdef"
	store := &canonicalCheckpointStore{testStore: &testStore{prepared: &apiv1.Checkpoint{Id: checkpointID, SessionId: canonical, State: apiv1.CheckpointState_CHECKPOINT_STATE_READY}}}
	svc := NewService(store, testAuthorizer{}, nil, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	for _, id := range []string{canonical, strings.ToUpper(canonical)} {
		got, err := svc.Create(ctx, id, "same-request", "head")
		require.NoError(t, err)
		require.Equal(t, checkpointID, got.GetId())
		require.Equal(t, canonical, store.sessionID)
	}
	_, err := svc.Get(ctx, strings.ToUpper(checkpointID))
	require.NoError(t, err)
	require.Equal(t, checkpointID, store.checkpointID)
	_, err = svc.List(ctx, ListRequest{SessionID: strings.ToUpper(canonical), PageToken: base64.RawURLEncoding.EncodeToString([]byte(strings.ToUpper(checkpointID)))})
	require.NoError(t, err)
	require.Equal(t, canonical, store.sessionID)
	require.Equal(t, checkpointID, store.afterID)
}

func TestForkCanonicalizesSavedSourceIdentity(t *testing.T) {
	const source = "018f47a2-4efb-7c21-a848-123456789abc"
	store := &testStore{prepared: &apiv1.Checkpoint{Id: "22222222-2222-4222-8222-222222222222", SessionId: strings.ToUpper(source)}}
	policy := &exactIdentityPolicy{denied: auth.Resource{Type: "Session", Name: source}}
	svc := NewService(store, policy, nil, nil)
	_, err := svc.Fork(auth.AuthSessionTo(t.Context(), testSession{userID: "alice"}), store.prepared.Id, "request")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
	require.Equal(t, policy.denied, policy.calls[len(policy.calls)-1])
	require.Zero(t, store.forkSourceReads)
	require.Nil(t, store.forked)
}
