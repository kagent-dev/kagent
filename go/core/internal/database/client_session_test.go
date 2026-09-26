package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestMalformedDatabaseIDsReturnErrors(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	for _, test := range []struct {
		name string
		call func() error
	}{
		{"get session", func() error { _, err := client.GetSession(ctx, "invalid", "alice"); return err }},
		{"rename session", func() error { _, err := client.UpdateSessionName(ctx, "invalid", "alice", "name"); return err }},
		{"delete session", func() error { return deleteSession(ctx, client, "invalid") }},
		{"get checkpoint", func() error { _, err := client.GetSessionCheckpoint(ctx, "invalid", "alice"); return err }},
		{"reserve checkpoint", func() error {
			_, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{SessionId: "invalid", HeadTaskId: "task-1"}, "alice", "request")
			return err
		}},
		{"fork", func() error {
			_, _, err := client.ForkSession(ctx, "invalid", "alice", "request", uuid.NewString())
			return err
		}},
		{"create share", func() error {
			_, err := client.CreateSessionShare(ctx, &apiv1alpha1.SessionShare{Id: uuid.NewString(), SessionId: "invalid", Permission: apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_ONLY}, []byte("token"), "alice")
			return err
		}},
		{"create task", func() error {
			_, err := client.CreateRuntimeTask(ctx, "invalid", taskMutationHash("hash"), newSessionTask("task", "message"), "")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) { require.Error(t, test.call()) })
	}
}

func TestToSessionUsesIndexedLifecycleColumns(t *testing.T) {
	data, err := proto.Marshal(&apiv1alpha1.Session{
		Id: "11111111-1111-4111-8111-111111111111", Name: "Renamed later",
		State:     apiv1alpha1.RuntimeState_RUNTIME_STATE_READY,
		Operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE,
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := toSession(sessionRow{
		runtimeInstanceRow: runtimeInstanceRow{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), State: apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED.String(), Operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME.String()}, Data: data,
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED ||
		session.GetOperation() != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME {
		t.Fatalf("lifecycle = %s/%s, want SUSPENDED/RESUME", session.GetState(), session.GetOperation())
	}
	// Display metadata stays in the protobuf.
	if session.GetName() != "Renamed later" {
		t.Fatalf("name = %q, want the protobuf value", session.GetName())
	}
}

// TestToSessionLeavesAnEmptyNameEmpty preserves unnamed sessions without
// substituting their ID or a placeholder.
func TestToSessionLeavesAnEmptyNameEmpty(t *testing.T) {
	data, err := proto.Marshal(&apiv1alpha1.Session{
		Id:    "session-1",
		State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := toSession(sessionRow{runtimeInstanceRow: runtimeInstanceRow{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY.String(), Operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE.String()}, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if session.GetName() != "" {
		t.Fatalf("name = %q, want empty", session.GetName())
	}
}

func TestSessionTasksAreDurableAndExclusive(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	insertReadySessionFixture(t, db, "11111111-1111-4111-8111-111111111111", "request-1", []byte{})
	client := NewClient(db)
	now := time.Now()
	first := &a2a.Task{
		ID: "task-1", ContextID: "11111111-1111-4111-8111-111111111111",
		Status:  a2a.TaskStatus{State: a2a.TaskStateSubmitted, Timestamp: &now},
		History: []*a2a.Message{{ID: "message-1", Role: a2a.MessageRoleUser}},
	}
	version, err := client.CreateRuntimeTask(ctx, "11111111-1111-4111-8111-111111111111", taskMutationHash("request-1"), first, "")
	require.NoError(t, err)
	require.Positive(t, version)
	if events := countRows(t, db, "SELECT COUNT(*) FROM session_task_event"); events != 2 {
		t.Fatalf("event count after retries = %d, want 2", events)
	}
	var eventTaskID string
	if err := db.QueryRow(ctx, "SELECT task_id FROM session_task_event").Scan(&eventTaskID); err != nil || eventTaskID != string(first.ID) {
		t.Fatalf("initial event task ID = %q, want %q: %v", eventTaskID, first.ID, err)
	}
	got, err := client.GetSessionTask(ctx, "11111111-1111-4111-8111-111111111111", "task-1", nil)
	if err != nil || got.ID != first.ID || got.Status.State != first.Status.State || len(got.History) != 1 {
		t.Fatalf("GetSessionTask() = %#v, %v", got, err)
	}
	var projectionData []byte
	if err := db.QueryRow(ctx, `SELECT data FROM session_task WHERE history_id = '11111111-1111-4111-8111-111111111111' AND id = 'task-1'`).Scan(&projectionData); err != nil {
		t.Fatal(err)
	}
	projection, err := unmarshalSessionTask(projectionData)
	if err != nil || len(projection.History) != 0 {
		t.Fatalf("stored task projection history = %#v, error %v", projection.History, err)
	}
	second := &a2a.Task{ID: "task-2", ContextID: "11111111-1111-4111-8111-111111111111", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}
	if err := saveRuntimeTask(t, client, "11111111-1111-4111-8111-111111111111", second, second, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("second active task error = %v", err)
	}
	first.History = append(first.History, a2a.NewMessageForTask(a2a.MessageRoleAgent, first, a2a.NewTextPart("done")))
	first.Status.State = a2a.TaskStateCompleted
	snapshot := &SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", ContentScope: "DATA"}
	if err := saveRuntimeTask(t, client, "11111111-1111-4111-8111-111111111111", first, first, snapshot); err != nil {
		t.Fatal(err)
	}
	var snapshotAtespace, snapshotURI string
	var historySequence, latestSequence int64
	if err := db.QueryRow(ctx, `
		SELECT snapshot_atespace, snapshot_uri, history_sequence
		FROM session_task WHERE history_id = '11111111-1111-4111-8111-111111111111' AND id = 'task-1'
	`).Scan(&snapshotAtespace, &snapshotURI, &historySequence); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT MAX(sequence) FROM session_task_event`).Scan(&latestSequence); err != nil {
		t.Fatal(err)
	}
	if snapshotAtespace != snapshot.Atespace || snapshotURI != snapshot.URI || historySequence != latestSequence {
		t.Fatalf("stored boundary = %s/%s sequence %d", snapshotAtespace, snapshotURI, historySequence)
	}
	got, err = client.GetSessionTask(ctx, "11111111-1111-4111-8111-111111111111", "task-1", nil)
	if err != nil || len(got.History) != 2 || got.History[1].Role != a2a.MessageRoleAgent {
		t.Fatalf("reconstructed task history = %#v, error %v", got, err)
	}
	if err := saveRuntimeTask(t, client, "11111111-1111-4111-8111-111111111111", second, second, nil); err != nil {
		t.Fatal(err)
	}
	if events := countRows(t, db, "SELECT COUNT(*) FROM session_task_event"); events != 5 {
		t.Fatalf("event count = %d, want 5", events)
	}

	tasks, total, err := client.ListSessionTasks(ctx, "11111111-1111-4111-8111-111111111111", "", a2a.TaskStateUnspecified, nil, 1, nil)
	if err != nil || total != 2 || len(tasks) != 1 || tasks[0].ID != first.ID {
		t.Fatalf("first page = %#v, total %d, error %v", tasks, total, err)
	}
	tasks, total, err = client.ListSessionTasks(ctx, "11111111-1111-4111-8111-111111111111", string(first.ID), a2a.TaskStateSubmitted, nil, 2, nil)
	if err != nil || total != 1 || len(tasks) != 1 || tasks[0].ID != second.ID {
		t.Fatalf("filtered page = %#v, total %d, error %v", tasks, total, err)
	}
}

func TestSessionReplyArchivesStatusMessageAtomically(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	session, err = markSessionReady(ctx, client, session.Id, "agent.example")
	require.NoError(t, err)
	sessionID := session.Id
	asked := &a2a.Message{ID: "message-1", Role: a2a.MessageRoleUser, TaskID: "task-1", ContextID: session.ContextId}
	question := &a2a.Message{ID: "question-1", Role: a2a.MessageRoleAgent, TaskID: "task-1", ContextID: session.ContextId}
	parked := &a2a.Task{
		ID: "task-1", ContextID: session.ContextId, History: []*a2a.Message{asked},
		Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: question},
	}
	version, err := client.CreateRuntimeTask(ctx, sessionID, taskMutationHash("request-1"), parked, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(ctx, sessionID, string(parked.ID), version))
	boundary, err := client.ClaimSessionQuiescence(ctx)
	require.NoError(t, err)
	require.NoError(t, client.FinishSessionQuiescence(ctx, boundary, nil))

	answer := &a2a.Message{ID: "answer-1", Role: a2a.MessageRoleUser, TaskID: "task-1", ContextID: session.ContextId}
	resumed := *parked
	resumed.History = []*a2a.Message{asked, question, answer}
	resumed.Status = a2a.TaskStatus{State: a2a.TaskStateSubmitted}
	if _, err := client.UpdateSessionTask(ctx, sessionID, version, taskMutationHash("reply"), &resumed, answer, ""); err != nil {
		t.Fatal(err)
	}

	got, err := client.GetSessionTask(ctx, sessionID, "task-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(got.History))
	for _, message := range got.History {
		ids = append(ids, message.ID)
	}
	if strings.Join(ids, ",") != "message-1,question-1,answer-1" {
		t.Fatalf("history = %v, want the question between the message it answers and its own answer", ids)
	}
}

func TestSessionCheckpointRetainsRecordedBoundary(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	sessionID := "11111111-1111-4111-8111-111111111111"
	session := &apiv1alpha1.Session{
		Id: sessionID, Creator: "alice",
		State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY,
	}
	sessionData, err := proto.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	insertReadySessionFixture(t, db, sessionID, "session-request", sessionData)
	client := NewClient(db)
	task := newSessionTask("task-1", "message-1")
	if _, err := client.CreateRuntimeTask(ctx, sessionID, taskMutationHash("message-request"), task, ""); err != nil {
		t.Fatal(err)
	}
	task.Status.State = a2a.TaskStateCompleted
	if err := saveRuntimeTask(t, client, sessionID, task, task,
		&SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", ContentScope: "DATA"}); err != nil {
		t.Fatal(err)
	}

	checkpoint, snapshot, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: "22222222-2222-4222-8222-222222222222", SessionId: sessionID, HeadTaskId: "task-1"}, "alice", "checkpoint-request")
	if err != nil {
		t.Fatalf("ReserveSessionCheckpoint() = %+v, error %v", checkpoint, err)
	}
	if _, err := client.GetSessionCheckpoint(ctx, checkpoint.GetId(), "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("creating checkpoint is publicly visible: %v", err)
	}
	if _, _, err := client.GetSessionCheckpointSnapshot(ctx, checkpoint.GetId(), "alice"); err != nil {
		t.Fatalf("creating checkpoint is unavailable to lifecycle work: %v", err)
	}
	if _, _, err := client.ForkSession(ctx, checkpoint.GetId(), "alice", "premature-fork", uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fork from creating checkpoint = %v, want not found", err)
	}
	if _, _, err := client.GetSessionCheckpointSnapshot(ctx, checkpoint.GetId(), "mallory"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("snapshot lookup by another user = %v, want not found", err)
	}
	require.Equal(t, "11111111-1111-4111-8111-111111111111-task-1", checkpoint.GetName())
	if _, err := client.UpdateCheckpointName(ctx, checkpoint.GetId(), "alice", "too early"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename of a creating checkpoint = %v, want not found", err)
	}
	if checkpoint.HeadTaskId != "task-1" || snapshot == nil ||
		*snapshot != (SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", ContentScope: "DATA"}) || checkpoint.HistorySequence == 0 {
		t.Fatalf("checkpoint boundary = %+v", checkpoint)
	}
	if _, err := client.CreateRuntimeTask(ctx, sessionID, taskMutationHash("blocked-request"), newSessionTask("task-2", "message-2"), ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateRuntimeTask() during checkpoint = %v, want %v", err, ErrConflict)
	} else {
		require.ErrorContains(t, err, "checkpoint being created")
	}
	_, err = client.BeginSessionOperation(ctx, sessionID, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.ErrorIs(t, err, ErrConflict)
	replayed, replayedSnapshot, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: "33333333-3333-4333-8333-333333333333", SessionId: sessionID, HeadTaskId: "task-1"}, "alice", "checkpoint-request")
	if err != nil || replayed.Id != checkpoint.Id || replayedSnapshot == nil || *replayedSnapshot != *snapshot {
		t.Fatalf("replayed checkpoint = %+v, error %v", replayed, err)
	}
	ready, err := client.FinalizeSessionCheckpoint(ctx, checkpoint.GetId(), "tag-uid", "s3://tags/checkpoint", "")
	if err != nil || ready.State != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY {
		t.Fatalf("ready checkpoint = %+v, error %v", ready, err)
	}
	if _, _, err := client.ForkSession(ctx, checkpoint.GetId(), "mallory", "unauthorized-fork", uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fork by another user = %v, want not found", err)
	}
	renamed, err := client.UpdateCheckpointName(ctx, checkpoint.GetId(), "alice", "Before the detour")
	require.NoError(t, err)
	require.Equal(t, "Before the detour", renamed.GetName())
	if _, err := client.UpdateCheckpointName(ctx, checkpoint.GetId(), "mallory", "stolen"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename by another user = %v, want not found", err)
	}
	stored, err := client.GetSessionCheckpoint(ctx, checkpoint.GetId(), "alice")
	require.NoError(t, err)
	require.Equal(t, "Before the detour", stored.GetName())
	restored, err := client.UpdateCheckpointName(ctx, checkpoint.GetId(), "alice", "")
	require.NoError(t, err)
	require.Equal(t, "11111111-1111-4111-8111-111111111111-task-1", restored.GetName())
	retained, tagUID, err := client.GetSessionCheckpointSnapshot(ctx, checkpoint.GetId(), "alice")
	if err != nil || tagUID != "tag-uid" || retained.URI != "s3://tags/checkpoint" || retained.ContentScope != snapshot.ContentScope {
		t.Fatalf("checkpoint tag = %q, error %v", tagUID, err)
	}
	if replayed, err := client.FinalizeSessionCheckpoint(ctx, checkpoint.GetId(), "tag-uid", "s3://tags/checkpoint", ""); err != nil || replayed.State != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY {
		t.Fatalf("replayed ready checkpoint = %+v, error %v", replayed, err)
	}
	failed, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: "44444444-4444-4444-8444-444444444444", SessionId: sessionID, HeadTaskId: "task-1"}, "alice", "failed-checkpoint-request")
	if err != nil {
		t.Fatal(err)
	}
	failed, err = client.FinalizeSessionCheckpoint(ctx, failed.GetId(), "", "", "tag creation failed")
	if err != nil || failed.State != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_FAILED || failed.GetFailure().GetMessage() != "tag creation failed" {
		t.Fatalf("failed checkpoint = %+v, error %v", failed, err)
	}
	if _, err := client.UpdateCheckpointName(ctx, failed.GetId(), "alice", "doomed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename of a failed checkpoint = %v, want not found", err)
	}
	if err := deleteSession(ctx, client, sessionID); err != nil {
		t.Fatal(err)
	}
	replayed, replayedSnapshot, err = client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: "55555555-5555-4555-8555-555555555555", SessionId: sessionID, HeadTaskId: "task-1"}, "alice", "checkpoint-request")
	if err != nil || replayed.Id != checkpoint.Id || replayedSnapshot == nil || *replayedSnapshot != *retained {
		t.Fatalf("checkpoint replay after source deletion = %+v, error %v", replayed, err)
	}
	listed, err := client.ListSessionCheckpoints(ctx, sessionID, "alice", "", 10)
	if err != nil || len(listed) != 1 || listed[0].Id != checkpoint.Id {
		t.Fatalf("listed checkpoints = %+v, error %v", listed, err)
	}
	if ref, tag, err := client.BeginDeleteSessionCheckpoint(ctx, checkpoint.GetId(), "mallory"); !errors.Is(err, ErrNotFound) || ref != nil || tag != "" {
		t.Fatalf("unauthorized deletion = %+v, %q, %v", ref, tag, err)
	}
	deletingSnapshot, deletingTagUID, err := client.BeginDeleteSessionCheckpoint(ctx, checkpoint.GetId(), "alice")
	if err != nil || deletingSnapshot == nil || *deletingSnapshot != *retained || deletingTagUID != "tag-uid" {
		t.Fatalf("deleting snapshot = %+v, tag = %q, error %v", deletingSnapshot, deletingTagUID, err)
	}
	if _, err := client.GetSessionCheckpoint(ctx, checkpoint.GetId(), "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting checkpoint is publicly visible: %v", err)
	}
	if _, err := client.UpdateCheckpointName(ctx, checkpoint.GetId(), "alice", "too late"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename of a deleting checkpoint = %v, want not found", err)
	}
	deletingSnapshot, deletingTagUID, err = client.BeginDeleteSessionCheckpoint(ctx, checkpoint.GetId(), "alice")
	if err != nil || deletingSnapshot == nil || *deletingSnapshot != *retained || deletingTagUID != "tag-uid" {
		t.Fatalf("deleting snapshot = %+v, tag = %q, error %v", deletingSnapshot, deletingTagUID, err)
	}
	if err := client.DeleteSessionCheckpoint(ctx, checkpoint.GetId(), "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestReserveSessionCheckpointRejectsCorruptSource(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	client := NewClient(db)
	sessionID := "99999999-9999-4999-8999-999999999999"
	insertReadySessionFixture(t, db, sessionID, "session-request", []byte{0xff})
	_, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: sessionID}, "alice", "checkpoint-request")
	require.ErrorContains(t, err, "decode Session")
}

func TestForkSessionCopiesBoundedHistory(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()
	sourceID := "66666666-6666-4666-8666-666666666666"
	forkID := "77777777-7777-4777-8777-777777777777"
	fork2ID := "88888888-8888-4888-8888-888888888888"
	revision := RuntimeRevision{
		Revision: "revision-1", Namespace: "team-a",
		AgentName: "assistant", AgentUID: "template-uid",

		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{Name: "assistant"}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision",
		ActorTemplateUID: "actor-template-uid",
	}
	pair := AgentDefinition{
		Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid",
		DesiredRevision: revision.Revision,
	}
	if err := client.UpsertAgentDefinition(ctx, pair); err != nil {
		t.Fatal(err)
	}
	if err := client.RecordRuntimeRevision(ctx, revision, true); err != nil {
		t.Fatal(err)
	}

	source, _, err := client.CreateSession(ctx, &apiv1alpha1.Session{
		Id: sourceID, Creator: "alice", Name: "Namespace explanation",

		Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}, "source-request")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markSessionReady(ctx, client, source.GetId(), "source.example"); err != nil {
		t.Fatal(err)
	}
	first := newSessionTask("task-1", "message-1")
	first.ContextID = source.GetContextId()
	first.History[0].ContextID = source.GetContextId()
	first.History[0].TaskID = first.ID
	first.History[0].ReferenceTasks = []a2a.TaskID{first.ID}
	first.Status.Message = &a2a.Message{ID: "message-1", Role: a2a.MessageRoleAgent, TaskID: first.ID, ContextID: source.GetContextId()}
	if _, err := client.CreateRuntimeTask(ctx, source.GetId(), taskMutationHash("message-request-1"), first, ""); err != nil {
		t.Fatal(err)
	}
	first.Status.State = a2a.TaskStateInputRequired
	if err := saveRuntimeTask(t, client, source.GetId(), first, first,
		&SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", ContentScope: "DATA"}); err != nil {
		t.Fatal(err)
	}
	_, _, err = client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: source.GetId(), HeadTaskId: "task-1"}, "alice", "hitl-checkpoint-request")
	require.ErrorIs(t, err, ErrFailedPrecondition)

	first.Status.State = a2a.TaskStateCompleted
	if err := saveRuntimeTask(t, client, source.GetId(), first, first,
		&SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", ContentScope: "DATA"}); err != nil {
		t.Fatal(err)
	}
	checkpoint, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: "99999999-9999-4999-8999-999999999999", SessionId: source.GetId(), HeadTaskId: "task-1"}, "alice", "checkpoint-request-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FinalizeSessionCheckpoint(ctx, checkpoint.GetId(), "tag-uid-1", "s3://tags/checkpoint", ""); err != nil {
		t.Fatal(err)
	}
	_, err = client.UpdateSessionName(ctx, source.GetId(), "alice", "Renamed after checkpoint")
	require.NoError(t, err)
	_, err = client.UpdateCheckpointName(ctx, checkpoint.GetId(), "alice", "Before the detour")
	require.NoError(t, err)

	// Completed tasks are immutable; later tasks must not change this checkpoint.
	first.Status.Message = a2a.NewMessageForTask(a2a.MessageRoleAgent, first, a2a.NewTextPart("source advanced"))
	require.ErrorIs(t, saveRuntimeTask(t, client, source.Id, first, first, nil), ErrFailedPrecondition)

	second := newSessionTask("task-2", "message-2")
	second.ContextID = source.GetContextId()
	if _, err := client.CreateRuntimeTask(ctx, source.GetId(), taskMutationHash("message-request-2"), second, ""); err != nil {
		t.Fatal(err)
	}
	second.Status.State = a2a.TaskStateCompleted
	if err := saveRuntimeTask(t, client, source.GetId(), second, second,
		&SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-2", ContentScope: "DATA"}); err != nil {
		t.Fatal(err)
	}
	if err := deleteSession(ctx, client, source.GetId()); err != nil {
		t.Fatal(err)
	}

	fork, created, err := client.ForkSession(ctx, checkpoint.GetId(), "alice", "fork-request-1", forkID)
	require.Equal(t, "Before the detour", fork.GetName())
	if err != nil || !created {
		t.Fatalf("ForkSession() = %+v, created %v, error %v", fork, created, err)
	}
	if fork.GetId() != forkID || fork.GetPreparedRevision() != revision.Revision || fork.GetA2AAuthority() != "" ||
		fork.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING ||
		fork.GetAgent().GetName() != "assistant" {
		t.Fatalf("fork = %+v", fork)
	}
	sessions, err := client.ListSessions(ctx, SessionQuery{
		UserID: "alice", Limit: 10,
	})
	if err != nil || len(sessions) != 1 || sessions[0].GetId() != fork.GetId() {
		t.Fatalf("listed forks = %+v, error %v", sessions, err)
	}
	tasks, total, err := client.ListSessionTasks(ctx, fork.GetId(), "", a2a.TaskStateUnspecified, nil, 10, nil)
	if err != nil || total != 1 || len(tasks) != 1 {
		t.Fatalf("fork tasks = %+v, total %d, error %v", tasks, total, err)
	}
	copied := tasks[0]
	require.Equal(t, a2a.TaskStateCompleted, copied.Status.State)
	if copied.ID == first.ID || copied.ContextID != fork.GetId() || len(copied.History) != 1 ||
		copied.History[0].ID != first.History[0].ID || copied.History[0].ContextID != fork.GetId() ||
		copied.History[0].TaskID != copied.ID || copied.History[0].ReferenceTasks[0] != copied.ID ||
		copied.Status.Message.ID != copied.History[0].ID || copied.Status.Message.TaskID != copied.ID {
		t.Fatalf("reidentified task = %+v", copied)
	}
	var snapshotUID string
	require.NoError(t, db.QueryRow(ctx, `
  SELECT snapshot_uri FROM session_task
  WHERE history_id = (SELECT history_id FROM session WHERE id = $1) AND id = $2
 `, fork.GetId(), copied.ID).Scan(&snapshotUID))
	require.Equal(t, "s3://tags/checkpoint", snapshotUID)
	replayed, created, err := client.ForkSession(ctx, checkpoint.GetId(), "alice", "fork-request-1", "ignored")
	if err != nil || created || replayed.GetId() != fork.GetId() {
		t.Fatalf("replayed fork = %+v, created %v, error %v", replayed, created, err)
	}
	if _, _, err := client.ForkSession(ctx, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "alice", "fork-request-1", "ignored"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting fork request error = %v", err)
	}
	if _, _, err := client.BeginDeleteSessionCheckpoint(ctx, checkpoint.GetId(), "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete referenced checkpoint error = %v", err)
	}

	if _, err := markSessionReady(ctx, client, fork.GetId(), "fork.example"); err != nil {
		t.Fatal(err)
	}
	checkpoint2, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", SessionId: fork.GetId(), HeadTaskId: string(copied.ID)}, "alice", "checkpoint-request-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FinalizeSessionCheckpoint(ctx, checkpoint2.GetId(), "tag-uid-2", "s3://tags/checkpoint", ""); err != nil {
		t.Fatal(err)
	}
	if err := deleteSession(ctx, client, fork.GetId()); err != nil {
		t.Fatal(err)
	}
	fork2, created, err := client.ForkSession(ctx, checkpoint2.GetId(), "alice", "fork-request-2", fork2ID)
	require.Equal(t, fork.GetId()+"-"+string(copied.ID), fork2.GetName())
	if err != nil || !created || fork2.GetId() != fork2ID {
		t.Fatalf("fork of fork = %+v, created %v, error %v", fork2, created, err)
	}
	// Deleting a fork releases its checkpoint pin but preserves its original
	// request identity, even after that checkpoint itself has been removed.
	_, _, err = client.BeginDeleteSessionCheckpoint(ctx, checkpoint.GetId(), "alice")
	require.NoError(t, err)
	require.NoError(t, client.DeleteSessionCheckpoint(ctx, checkpoint.GetId(), "alice"))
	_, _, err = client.ForkSession(ctx, checkpoint.GetId(), "alice", "fork-request-1", uuid.NewString())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	_, _, err = client.ForkSession(ctx, checkpoint2.GetId(), "alice", "fork-request-1", uuid.NewString())
	require.ErrorIs(t, err, ErrIdempotencyConflict)
}

func TestSessionCreateAndTransitions(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := context.Background()
	revision := RuntimeRevision{
		Revision: "revision-1", Namespace: "team-a",
		AgentName: "assistant", AgentUID: "template-uid",

		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{Name: "assistant"}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision",
		ActorTemplateUID: "actor-template-uid",
	}
	pair := AgentDefinition{
		Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid",
		DesiredRevision: revision.Revision,
	}
	if err := client.UpsertAgentDefinition(ctx, pair); err != nil {
		t.Fatal(err)
	}
	if err := client.RecordRuntimeRevision(ctx, revision, true); err != nil {
		t.Fatal(err)
	}

	storedRevision, err := client.GetRuntimeRevision(ctx, revision.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if storedRevision.AgentCard.GetName() != "assistant" {
		t.Fatalf("GetRuntimeRevision() Agent Card = %s: %v", storedRevision.AgentCard, err)
	}

	request := &apiv1alpha1.Session{
		Id: "11111111-1111-4111-8111-111111111111", Creator: "alice",

		Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}
	created, wasCreated, err := client.CreateSession(ctx, request, "request-1")
	if err != nil || !wasCreated {
		t.Fatalf("first CreateSession() = created %v, error %v", wasCreated, err)
	}
	request.Id = "22222222-2222-4222-8222-222222222222"
	replayed, wasCreated, err := client.CreateSession(ctx, request, "request-1")
	if err != nil || wasCreated {
		t.Fatalf("replayed CreateSession() = created %v, error %v", wasCreated, err)
	}
	if replayed.GetId() != created.GetId() || replayed.GetPreparedRevision() != revision.Revision {
		t.Fatalf("replayed session = %+v, want id %q revision %q", replayed, created.GetId(), revision.Revision)
	}
	sessions, err := client.ListSessions(ctx, SessionQuery{
		UserID: "alice", Limit: 10,
	})
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions() = %v, error %v", sessions, err)
	}
	ready, err := markSessionReady(ctx, client, created.GetId(), "actor.example")
	if err != nil {
		t.Fatal(err)
	}
	suspend, err := client.BeginSessionOperation(ctx, ready.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)
	_, err = client.BeginSessionOperation(ctx, ready.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
	require.ErrorIs(t, err, ErrConflict)
	executor := uuid.New()
	claimed, err := client.ClaimSessionOperation(ctx, ready.Id, suspend.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	suspended, err := client.FinishSessionOperation(ctx, ready.Id, suspend.ID, executor, "", "", "")
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED, suspended.State)

	request.Agent.Name = "different"
	if _, _, err := client.CreateSession(ctx, request, "request-1"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting request error = %v", err)
	}
}

func newSessionTask(id, messageID string) *a2a.Task {
	now := time.Now()
	return &a2a.Task{
		ID: a2a.TaskID(id), ContextID: "11111111-1111-4111-8111-111111111111",
		Status:  a2a.TaskStatus{State: a2a.TaskStateWorking, Timestamp: &now},
		History: []*a2a.Message{{ID: messageID, Role: a2a.MessageRoleUser}},
	}
}

func insertReadySessionFixture(t *testing.T, db dbExecutor, id, requestID string, data []byte) {
	t.Helper()
	_, err := db.Exec(t.Context(), `
		WITH history AS (
			INSERT INTO a2a_context (id, context_id) VALUES ($1, $1)
			RETURNING id, context_id
		), runtime AS (
			INSERT INTO runtime_instance (id, kind, user_id, request_id, state, operation)
			VALUES ($1, 'agent', 'alice', $2, 'RUNTIME_STATE_READY', 'RUNTIME_OPERATION_NONE')
			RETURNING id
		)
		INSERT INTO session (id, context_id, history_id, data)
		SELECT runtime.id, history.context_id, history.id, $3 FROM runtime CROSS JOIN history
	`, id, requestID, data)
	require.NoError(t, err)
}

// sessionFixture installs a runnable agent — a template/harness pair with a
// successful revision — so sessions can be created against it.
func sessionFixture(t *testing.T, client *Client, ctx context.Context, namespace, revisionID, template, harness string) {
	t.Helper()
	revision := RuntimeRevision{
		Revision: revisionID, Namespace: namespace,
		AgentName: template, AgentUID: template + "-uid",

		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{}, EgressDestinations: []string{},
		ActorTemplateAtespace: namespace, ActorTemplateName: revisionID + "-actor-template",
		ActorTemplateUID: revisionID + "-actor-uid",
	}
	pair := AgentDefinition{
		Namespace: namespace, AgentName: template, AgentUID: template + "-uid",
		DesiredRevision: revisionID,
	}
	if err := client.UpsertAgentDefinition(ctx, pair); err != nil {
		t.Fatal(err)
	}
	if err := client.RecordRuntimeRevision(ctx, revision, true); err != nil {
		t.Fatal(err)
	}
}

func newSessionRequest(id, template, harness, name string) *apiv1alpha1.Session {
	return &apiv1alpha1.Session{
		Id: id, Creator: "alice", Name: name,

		Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: template},
	}
}

func TestSessionsUseOwnerAndIDAcrossTargetNamespaces(t *testing.T) {
	c := NewClient(setupTestDB(t))
	ctx := t.Context()
	for _, namespace := range []string{"team-a", "team-b"} {
		sessionFixture(t, c, ctx, namespace, namespace+"-revision", "assistant", "kagent")
	}
	first := newSessionRequest(uuid.NewString(), "assistant", "kagent", "")
	created, _, err := c.CreateSession(ctx, first, "request")
	require.NoError(t, err)
	second := proto.CloneOf(first)
	second.Id = uuid.NewString()
	second.Agent.Namespace = "team-b"
	_, _, err = c.CreateSession(ctx, second, "request")
	require.ErrorIs(t, err, ErrIdempotencyConflict, "request IDs belong to the caller, across targets")
	other, _, err := c.CreateSession(ctx, second, "other-request")
	require.NoError(t, err)
	rows, err := c.ListSessions(ctx, SessionQuery{UserID: "alice", Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	filtered, err := c.ListSessions(ctx, SessionQuery{UserID: "alice", Limit: 10, Agent: second.Agent})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, other.Id, filtered[0].Id)
	loaded, err := c.GetSession(ctx, created.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, "team-a", loaded.Agent.Namespace)
	_, err = c.GetSession(ctx, created.Id, "bob")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSessionNameRoundTripsAndRenames(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := context.Background()
	sessionFixture(t, client, ctx, "team-a", "revision-1", "assistant", "kagent")
	namedID := "11111111-1111-4111-8111-111111111111"
	unnamedID := "22222222-2222-4222-8222-222222222222"

	for _, test := range []struct {
		name     string
		id       string
		given    string
		wantName string
	}{
		{name: "a name round-trips", id: namedID, given: "Debugging the ingress", wantName: "Debugging the ingress"},
		// A session created without a name must read back empty, which is how
		// every row written before the column existed reads.
		{name: "an omitted name stays empty", id: unnamedID, given: "", wantName: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			created, wasCreated, err := client.CreateSession(ctx, newSessionRequest(test.id, "assistant", "kagent", test.given), test.id)
			if err != nil || !wasCreated {
				t.Fatalf("CreateSession() = created %v, error %v", wasCreated, err)
			}
			if created.GetName() != test.wantName {
				t.Fatalf("created name = %q, want %q", created.GetName(), test.wantName)
			}
			read, err := client.GetSession(ctx, test.id, "alice")
			if err != nil || read.GetName() != test.wantName {
				t.Fatalf("re-read name = %q (%v), want %q", read.GetName(), err, test.wantName)
			}
		})
	}

	renamed, err := client.UpdateSessionName(ctx, unnamedID, "alice", "Named afterwards")
	if err != nil || renamed.GetName() != "Named afterwards" {
		t.Fatalf("UpdateSessionName() = %+v, error %v", renamed, err)
	}
	// The renamed protobuf must survive a re-read, not just be echoed back.
	read, err := client.GetSession(ctx, unnamedID, "alice")
	if err != nil || read.GetName() != "Named afterwards" {
		t.Fatalf("re-read after rename = %+v, error %v", read, err)
	}
	// Renaming back to empty must be possible, or a name can never be undone.
	cleared, err := client.UpdateSessionName(ctx, unnamedID, "alice", "")
	if err != nil || cleared.GetName() != "" {
		t.Fatalf("UpdateSessionName(\"\") = %+v, error %v", cleared, err)
	}
	// A rename is scoped to the owner, so it cannot reach another reader's row.
	if _, err := client.UpdateSessionName(ctx, namedID, "bob", "Stolen"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateSessionName() as another user error = %v, want %v", err, ErrNotFound)
	}
	if _, err := client.UpdateSessionName(ctx, "33333333-3333-4333-8333-333333333333", "alice", "Nothing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateSessionName() of a missing session error = %v, want %v", err, ErrNotFound)
	}
}

func TestListSessionsFiltersByAgent(t *testing.T) {
	c := NewClient(setupTestDB(t))
	ctx := t.Context()
	for _, name := range []string{"reviewer", "researcher"} {
		sessionFixture(t, c, ctx, "team-a", name+"-revision", name, "")
		_, _, err := c.CreateSession(ctx, newSessionRequest(uuid.NewString(), name, "", ""), name)
		require.NoError(t, err)
	}
	for _, name := range []string{"reviewer", "researcher", "missing"} {
		got, err := c.ListSessions(ctx, SessionQuery{UserID: "alice", Limit: 10, Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: name}})
		require.NoError(t, err)
		if name == "missing" {
			require.Empty(t, got)
		} else {
			require.Len(t, got, 1)
			require.Equal(t, name+"-revision", got[0].PreparedRevision)
		}
	}
}

func TestForkTaskOrderAndAuthorityIsolation(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	source, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", "Source"), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, source.GetId(), "source.example")
	require.NoError(t, err)
	require.Equal(t, source.GetId(), source.GetContextId())
	sourceRow, err := readSession(ctx, client.db, source.GetId())
	require.NoError(t, err)
	require.NotEqual(t, source.GetId(), sourceRow.HistoryID.String())
	require.NotEqual(t, source.GetContextId(), sourceRow.HistoryID.String())

	// Reverse lexical order and identical timestamps must not determine chronology.
	stamp := time.Now()
	for _, id := range []string{"z-first", "a-second"} {
		task := newSessionTask(id, "message-"+id)
		task.ContextID = source.GetContextId()
		task.Status.Timestamp = &stamp
		_, err := client.CreateRuntimeTask(ctx, source.GetId(), taskMutationHash(id), task, "")
		require.NoError(t, err)
		task.Status.State = a2a.TaskStateCompleted
		require.NoError(t, saveRuntimeTask(t, client, source.GetId(), task, task,
			&SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot-" + id, ContentScope: "DATA"}))
	}
	checkpoint, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{
		Id: uuid.NewString(), SessionId: source.GetId(), HeadTaskId: "a-second",
	}, "alice", uuid.NewString())
	require.NoError(t, err)
	waiting, err := client.GetSessionTask(ctx, source.GetId(), "z-first", nil)
	require.NoError(t, err)
	require.ErrorIs(t, saveRuntimeTask(t, client, source.GetId(), waiting, waiting, nil), ErrFailedPrecondition)
	_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.GetId(), "tag", "tag-snapshot", "")
	require.NoError(t, err)
	_, _, err = client.ForkSession(ctx, checkpoint.GetId(), "bob", uuid.NewString(), uuid.NewString())
	require.ErrorIs(t, err, ErrNotFound)
	fork, _, err := client.ForkSession(ctx, checkpoint.GetId(), "alice", uuid.NewString(), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, fork.GetId(), "fork.example")
	require.NoError(t, err)
	require.Equal(t, fork.GetId(), fork.GetContextId())
	require.NotEqual(t, source.GetContextId(), fork.GetContextId())
	forkRow, err := readSession(ctx, client.db, fork.GetId())
	require.NoError(t, err)
	require.NotEqual(t, sourceRow.HistoryID, forkRow.HistoryID)

	var forkHead string
	for _, session := range []*apiv1alpha1.Session{source, fork} {
		page, total, err := client.ListSessionTasks(ctx, session.GetId(), "", a2a.TaskStateUnspecified, nil, 1, nil)
		require.NoError(t, err)
		require.Equal(t, 2, total)
		require.Len(t, page, 1)
		require.Equal(t, "message-z-first", page[0].History[0].ID)
		page, _, err = client.ListSessionTasks(ctx, session.GetId(), string(page[0].ID), a2a.TaskStateUnspecified, nil, 1, nil)
		require.NoError(t, err)
		require.Len(t, page, 1)
		require.Equal(t, "message-a-second", page[0].History[0].ID)
		if session.Id == fork.Id {
			forkHead = string(page[0].ID)
		}
	}
	// A fork can itself be checkpointed without losing task chronology.
	nested, snapshot, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{
		Id: uuid.NewString(), SessionId: fork.GetId(), HeadTaskId: forkHead,
	}, "alice", uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, "tag-snapshot", snapshot.URI)
	_, err = client.FinalizeSessionCheckpoint(ctx, nested.GetId(), "nested-tag", "nested-snapshot", "")
	require.NoError(t, err)
	fork2, _, err := client.ForkSession(ctx, nested.GetId(), "alice", uuid.NewString(), uuid.NewString())
	require.NoError(t, err)
	tasks, _, err := client.ListSessionTasks(ctx, fork2.GetId(), "", a2a.TaskStateUnspecified, nil, 10, nil)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	require.Equal(t, "message-z-first", tasks[0].History[0].ID)
	require.Equal(t, a2a.TaskStateCompleted, tasks[0].Status.State)
	require.Equal(t, "message-a-second", tasks[1].History[0].ID)
	require.NotEqual(t, forkHead, string(tasks[1].ID))
	wrong := *waiting
	wrong.ContextID = fork.GetId()
	require.Error(t, saveRuntimeTask(t, client, fork.GetId(), &wrong, &wrong, nil))
}

// markSessionReady completes creation through the lifecycle operations.
func markSessionReady(ctx context.Context, client *Client, id, authority string) (*apiv1alpha1.Session, error) {
	return finishSessionOperation(ctx, client, id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE, authority)
}

func TestSessionShareCreationRequiresOwner(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "create")
	require.NoError(t, err)
	share := &apiv1alpha1.SessionShare{
		Id: uuid.NewString(), SessionId: session.Id,
		Permission: apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_ONLY,
	}
	_, err = client.CreateSessionShare(ctx, share, []byte("token"), "bob")
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = client.GetSessionShareByTokenHash(ctx, []byte("token"))
	require.ErrorIs(t, err, ErrNotFound)

	created, err := client.CreateSessionShare(ctx, share, []byte("token"), "alice")
	require.NoError(t, err)
	require.Equal(t, share.Id, created.Id)
	require.NotNil(t, created.CreatedAt)
	require.Nil(t, share.CreatedAt)

	require.NoError(t, deleteSession(ctx, client, session.Id))
	share.Id = uuid.NewString()
	_, err = client.CreateSessionShare(ctx, share, []byte("missing-token"), "alice")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDeletedSessionPreservesRequestIdentityAndHidesAccess(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	request := newSessionRequest(uuid.NewString(), "assistant", "kagent", "original")
	session, _, err := client.CreateSession(ctx, request, "stable-request")
	require.NoError(t, err)
	session, err = markSessionReady(ctx, client, session.Id, "runtime.example")
	require.NoError(t, err)
	task := newSessionTask("task", "message")
	task.ContextID = session.ContextId
	_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("hash"), task, "")
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateInputRequired
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))
	share := &apiv1alpha1.SessionShare{Id: uuid.NewString(), SessionId: session.Id, Permission: apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_WRITE}
	_, err = client.CreateSessionShare(ctx, share, []byte("token"), "alice")
	require.NoError(t, err)
	require.NoError(t, deleteSession(ctx, client, session.Id))
	require.NoError(t, deleteSession(ctx, client, session.Id))
	_, err = client.GetSessionByID(ctx, session.Id)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetSession(ctx, session.Id, "alice")
	require.ErrorIs(t, err, ErrNotFound)
	sessions, err := client.ListSessions(ctx, SessionQuery{AllUsers: true, Limit: 10})
	require.NoError(t, err)
	require.Empty(t, sessions)
	_, err = client.UpdateSessionName(ctx, session.Id, "alice", "resurrect")
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = client.GetSessionShareByTokenHash(ctx, []byte("token"))
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.CreateSessionShare(ctx, share, []byte("new token"), "alice")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetSessionTask(ctx, session.Id, string(task.ID), nil)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("hash"), task, "")
	require.ErrorIs(t, err, ErrNotFound, "even task retries must respect deletion")
	_, err = client.UpdateSessionTask(ctx, session.Id, 1, taskMutationHash("reply"), task, task, "")
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, saveRuntimeTask(t, client, session.Id, task, task, nil), ErrNotFound)
	request.Id = uuid.NewString()
	_, created, err := client.CreateSession(ctx, request, "stable-request")
	require.ErrorIs(t, err, ErrFailedPrecondition)
	require.False(t, created)
	request.Agent.Name = "different"
	_, _, err = client.CreateSession(ctx, request, "stable-request")
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	// Tombstones release the revision FK and no longer block runtime cleanup.
	require.NoError(t, client.RetireAgentIdentities(ctx, "team-a", "assistant", nil))
	revisions, err := client.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	_, err = client.BeginRuntimeRevisionDeletion(ctx, "revision")
	require.NoError(t, err)
	require.NoError(t, client.DeleteRuntimeRevision(ctx, "revision", revisions[0].ActorTemplateUID))
}

func TestShareCreationRacesSessionDeletion(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	for range 8 {
		session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
		require.NoError(t, err)
		token := []byte(uuid.NewString())
		share := &apiv1alpha1.SessionShare{Id: uuid.NewString(), SessionId: session.Id, Permission: apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_ONLY}
		start := make(chan struct{})
		shared, deleted := make(chan error, 1), make(chan error, 1)
		go func() {
			<-start
			_, err := client.CreateSessionShare(ctx, share, token, "alice")
			shared <- err
		}()
		go func() {
			<-start
			deleted <- deleteSession(ctx, client, session.Id)
		}()
		close(start)
		shareErr, deleteErr := <-shared, <-deleted
		require.NoError(t, deleteErr)
		if shareErr != nil {
			require.ErrorIs(t, shareErr, ErrNotFound)
		}
		_, _, err = client.GetSessionShareByTokenHash(ctx, token)
		require.ErrorIs(t, err, ErrNotFound)
		// A revoked share must actually be removed, releasing its unique token hash.
		other, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
		require.NoError(t, err)
		share.SessionId = other.Id
		_, err = client.CreateSessionShare(ctx, share, token, "alice")
		require.NoError(t, err)
	}
}
