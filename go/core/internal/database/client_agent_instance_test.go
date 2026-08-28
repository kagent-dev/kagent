package database

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/gen"
	"google.golang.org/protobuf/proto"
)

func TestToAgentInstanceUsesIndexedLifecycleColumns(t *testing.T) {
	data, err := proto.Marshal(&apiv1alpha1.AgentInstance{
		Id:        "11111111-1111-4111-8111-111111111111",
		State:     apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
		Operation: apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED,
	})
	if err != nil {
		t.Fatal(err)
	}

	instance, err := toAgentInstance(dbgen.AgentInstance{
		ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Data: data, State: "SUSPENDED", Operation: "RESUME", Name: "Renamed later",
	})
	if err != nil {
		t.Fatal(err)
	}
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED ||
		instance.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME {
		t.Fatalf("lifecycle = %s/%s, want SUSPENDED/RESUME", instance.GetState(), instance.GetOperation())
	}
	// The name has to come from the column too: a rename writes only the column,
	// so reading it from the blob would serve the original name forever.
	if instance.GetName() != "Renamed later" {
		t.Fatalf("name = %q, want the column's value", instance.GetName())
	}
}

// TestToAgentInstanceLeavesAnEmptyNameEmpty pins the additive property: a row
// written before the column existed reads as unnamed, not as its id and not as
// some placeholder.
func TestToAgentInstanceLeavesAnEmptyNameEmpty(t *testing.T) {
	data, err := proto.Marshal(&apiv1alpha1.AgentInstance{
		Id:    "instance-1",
		State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
	})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := toAgentInstance(dbgen.AgentInstance{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Data: data, State: "READY", Operation: "NONE"})
	if err != nil {
		t.Fatal(err)
	}
	if instance.GetName() != "" {
		t.Fatalf("name = %q, want empty", instance.GetName())
	}
}

func TestAgentInstanceTasksAreDurableAndExclusive(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, namespace, user_id)
		VALUES ('11111111-1111-4111-8111-111111111111', 'team-a', 'alice');

		INSERT INTO agent_instance (id, namespace, user_id, request_id, context_id, state, data)
		VALUES ('11111111-1111-4111-8111-111111111111', 'team-a', 'alice', 'request-1', '11111111-1111-4111-8111-111111111111', 'READY', '\x00')
	`); err != nil {
		t.Fatal(err)
	}
	client := NewClient(db)
	now := time.Now()
	first := &a2a.Task{
		ID: "task-1", ContextID: "11111111-1111-4111-8111-111111111111",
		Status:  a2a.TaskStatus{State: a2a.TaskStateSubmitted, Timestamp: &now},
		History: []*a2a.Message{{ID: "message-1", Role: a2a.MessageRoleUser}},
	}
	stored, created, err := client.CreateAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", []byte("request-1"), first)
	if err != nil || !created || stored.ID != first.ID {
		t.Fatalf("CreateAgentInstanceTask() = %#v, created %v, error %v", stored, created, err)
	}
	replayed, created, err := client.CreateAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", []byte("request-1"),
		&a2a.Task{ID: "ignored", ContextID: "11111111-1111-4111-8111-111111111111", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}, History: first.History})
	if err != nil || created || replayed.ID != first.ID {
		t.Fatalf("replayed CreateAgentInstanceTask() = %#v, created %v, error %v", replayed, created, err)
	}
	if _, _, err := client.CreateAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", []byte("different"), first); !errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		t.Fatalf("conflicting message error = %v", err)
	}
	if events := countRows(t, db, "SELECT COUNT(*) FROM agent_instance_task_event"); events != 1 {
		t.Fatalf("event count after retries = %d, want 1", events)
	}
	var eventTaskID string
	if err := db.QueryRow(ctx, "SELECT task_id FROM agent_instance_task_event").Scan(&eventTaskID); err != nil || eventTaskID != string(first.ID) {
		t.Fatalf("initial event task ID = %q, want %q: %v", eventTaskID, first.ID, err)
	}
	got, err := client.GetAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", "task-1")
	if err != nil || got.ID != first.ID || got.Status.State != first.Status.State || len(got.History) != 1 {
		t.Fatalf("GetAgentInstanceTask() = %#v, %v", got, err)
	}
	var projectionData []byte
	if err := db.QueryRow(ctx, `SELECT data FROM agent_instance_task WHERE context_id = '11111111-1111-4111-8111-111111111111' AND id = 'task-1'`).Scan(&projectionData); err != nil {
		t.Fatal(err)
	}
	projection, err := unmarshalAgentInstanceTask(projectionData)
	if err != nil || len(projection.History) != 0 {
		t.Fatalf("stored task projection history = %#v, error %v", projection.History, err)
	}
	second := &a2a.Task{ID: "task-2", ContextID: "11111111-1111-4111-8111-111111111111", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}
	if err := client.StoreAgentInstanceTaskEvent(ctx, "11111111-1111-4111-8111-111111111111", second, second, nil); !errors.Is(err, dbpkg.ErrAgentInstanceTaskConflict) {
		t.Fatalf("second active task error = %v", err)
	}
	first.History = append(first.History, a2a.NewMessageForTask(a2a.MessageRoleAgent, first, a2a.NewTextPart("done")))
	first.Status.State = a2a.TaskStateCompleted
	snapshot := &dbpkg.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-1", UID: "snapshot-uid"}
	if err := client.StoreAgentInstanceTaskEvent(ctx, "11111111-1111-4111-8111-111111111111", first, first, snapshot); err != nil {
		t.Fatal(err)
	}
	var snapshotAtespace, snapshotName, snapshotUID string
	var historySequence, latestSequence int64
	if err := db.QueryRow(ctx, `
		SELECT snapshot_atespace, snapshot_name, snapshot_uid, history_sequence
		FROM agent_instance_task WHERE context_id = '11111111-1111-4111-8111-111111111111' AND id = 'task-1'
	`).Scan(&snapshotAtespace, &snapshotName, &snapshotUID, &historySequence); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT MAX(sequence) FROM agent_instance_task_event`).Scan(&latestSequence); err != nil {
		t.Fatal(err)
	}
	if snapshotAtespace != snapshot.Atespace || snapshotName != snapshot.Name || snapshotUID != snapshot.UID || historySequence != latestSequence {
		t.Fatalf("stored boundary = %s/%s uid %s sequence %d", snapshotAtespace, snapshotName, snapshotUID, historySequence)
	}
	got, err = client.GetAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", "task-1")
	if err != nil || len(got.History) != 2 || got.History[1].Role != a2a.MessageRoleAgent {
		t.Fatalf("reconstructed task history = %#v, error %v", got, err)
	}
	if err := client.StoreAgentInstanceTaskEvent(ctx, "11111111-1111-4111-8111-111111111111", second, second, nil); err != nil {
		t.Fatal(err)
	}
	if events := countRows(t, db, "SELECT COUNT(*) FROM agent_instance_task_event"); events != 4 {
		t.Fatalf("event count = %d, want 4", events)
	}

	tasks, total, err := client.ListAgentInstanceTasks(ctx, "11111111-1111-4111-8111-111111111111", "", a2a.TaskStateUnspecified, nil, 1)
	if err != nil || total != 2 || len(tasks) != 1 || tasks[0].ID != first.ID {
		t.Fatalf("first page = %#v, total %d, error %v", tasks, total, err)
	}
	tasks, total, err = client.ListAgentInstanceTasks(ctx, "11111111-1111-4111-8111-111111111111", string(first.ID), a2a.TaskStateSubmitted, nil, 2)
	if err != nil || total != 1 || len(tasks) != 1 || tasks[0].ID != second.ID {
		t.Fatalf("filtered page = %#v, total %d, error %v", tasks, total, err)
	}
}

func TestConcurrentAgentInstanceMessageReplay(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, namespace, user_id)
		VALUES ('11111111-1111-4111-8111-111111111111', 'team-a', 'alice');
		INSERT INTO agent_instance (id, namespace, user_id, request_id, context_id, state, data)
		VALUES ('11111111-1111-4111-8111-111111111111', 'team-a', 'alice', 'request-1', '11111111-1111-4111-8111-111111111111', 'READY', '\x00')
	`); err != nil {
		t.Fatal(err)
	}
	client := NewClient(db)
	start := make(chan struct{})
	type result struct {
		task    *a2a.Task
		created bool
		err     error
	}
	results := make(chan result, 2)
	for _, taskID := range []a2a.TaskID{"task-1", "task-2"} {
		go func() {
			<-start
			message := &a2a.Message{ID: "message-1", Role: a2a.MessageRoleUser, TaskID: taskID, ContextID: "11111111-1111-4111-8111-111111111111"}
			task := a2a.NewSubmittedTask(message, message)
			stored, created, err := client.CreateAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", []byte("request-1"), task)
			results <- result{stored, created, err}
		}()
	}
	close(start)

	var resultID a2a.TaskID
	createdCount := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.created {
			createdCount++
		}
		if resultID == "" {
			resultID = result.task.ID
		} else if result.task.ID != resultID {
			t.Fatalf("replayed task ID = %q, want %q", result.task.ID, resultID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want 1", createdCount)
	}
}

func TestAgentInstanceReplyArchivesStatusMessageAtomically(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	instanceID := "11111111-1111-4111-8111-111111111111"
	if _, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, namespace, user_id)
		VALUES ('11111111-1111-4111-8111-111111111111', 'team-a', 'alice');
		INSERT INTO agent_instance (id, namespace, user_id, request_id, context_id, state, data)
		VALUES ('11111111-1111-4111-8111-111111111111', 'team-a', 'alice', 'request-1', '11111111-1111-4111-8111-111111111111', 'READY', '\\x00')
	`); err != nil {
		t.Fatal(err)
	}
	client := NewClient(db)
	asked := &a2a.Message{ID: "message-1", Role: a2a.MessageRoleUser, TaskID: "task-1", ContextID: instanceID}
	question := &a2a.Message{ID: "question-1", Role: a2a.MessageRoleAgent, TaskID: "task-1", ContextID: instanceID}
	parked := &a2a.Task{
		ID: "task-1", ContextID: instanceID, History: []*a2a.Message{asked},
		Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: question},
	}
	if _, _, err := client.CreateAgentInstanceTask(ctx, instanceID, []byte("request-1"), parked); err != nil {
		t.Fatal(err)
	}

	answer := &a2a.Message{ID: "answer-1", Role: a2a.MessageRoleUser, TaskID: "task-1", ContextID: instanceID}
	resumed := *parked
	resumed.History = []*a2a.Message{asked, question, answer}
	resumed.Status = a2a.TaskStatus{State: a2a.TaskStateSubmitted}
	if err := client.StoreAgentInstanceTaskEvent(ctx, instanceID, &resumed, answer, nil); err != nil {
		t.Fatal(err)
	}

	got, err := client.GetAgentInstanceTask(ctx, instanceID, "task-1")
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

func TestAgentInstanceCheckpointRetainsRecordedBoundary(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	instanceID := "11111111-1111-4111-8111-111111111111"
	instance := &apiv1alpha1.AgentInstance{
		Id: instanceID, Namespace: "team-a", Creator: "alice",
		State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
	}
	instanceData, err := proto.Marshal(instance)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, namespace, user_id)
		VALUES ($1, 'team-a', 'alice')
	`, instanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO agent_instance (id, namespace, user_id, request_id, context_id, state, data)
		VALUES ($1, 'team-a', 'alice', 'instance-request', $1, 'READY', $2)
	`, instanceID, instanceData); err != nil {
		t.Fatal(err)
	}
	client := NewClient(db)
	task := newAgentInstanceTask("task-1", "message-1")
	if _, _, err := client.CreateAgentInstanceTask(ctx, instanceID, []byte("message-request"), task); err != nil {
		t.Fatal(err)
	}
	task.Status.State = a2a.TaskStateCompleted
	if err := client.StoreAgentInstanceTaskEvent(ctx, instanceID, task, task,
		&dbpkg.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-1", UID: "snapshot-uid", ContentScope: "DATA"}); err != nil {
		t.Fatal(err)
	}

	checkpoint, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), Namespace: "team-a", SourceInstanceID: uuid.MustParse(instanceID), UserID: "alice",
		RequestID: "checkpoint-request",
	})
	if err != nil {
		t.Fatalf("ReserveAgentInstanceCheckpoint() = %+v, error %v", checkpoint, err)
	}
	if checkpoint.HeadTaskID != "task-1" || checkpoint.SnapshotUID != "snapshot-uid" ||
		checkpoint.SnapshotContentScope != "DATA" || checkpoint.HistorySequence == 0 {
		t.Fatalf("checkpoint boundary = %+v", checkpoint)
	}
	if _, _, err := client.CreateAgentInstanceTask(ctx, instanceID, []byte("blocked-request"), newAgentInstanceTask("task-2", "message-2")); !errors.Is(err, dbpkg.ErrAgentInstanceTaskConflict) {
		t.Fatalf("CreateAgentInstanceTask() during checkpoint = %v, want %v", err, dbpkg.ErrAgentInstanceTaskConflict)
	}
	suspending := proto.Clone(instance).(*apiv1alpha1.AgentInstance)
	suspending.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND
	current, err := client.TransitionAgentInstance(ctx, suspending, instance.GetState(), instance.GetOperation())
	if !errors.Is(err, dbpkg.ErrAgentInstanceConflict) || current.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		t.Fatalf("lifecycle transition during checkpoint = %+v, error %v", current, err)
	}
	replayed, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: uuid.MustParse("33333333-3333-4333-8333-333333333333"), Namespace: "team-a", SourceInstanceID: uuid.MustParse(instanceID), UserID: "alice",
		RequestID: "checkpoint-request",
	})
	if err != nil || replayed.ID != checkpoint.ID {
		t.Fatalf("replayed checkpoint = %+v, error %v", replayed, err)
	}
	ready, err := client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.ID.String(), "tag-uid", "")
	if err != nil || ready.State != "READY" || ready.TagUID != "tag-uid" {
		t.Fatalf("ready checkpoint = %+v, error %v", ready, err)
	}
	if replayed, err := client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.ID.String(), "tag-uid", ""); err != nil || replayed.State != "READY" {
		t.Fatalf("replayed ready checkpoint = %+v, error %v", replayed, err)
	}
	failed, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: uuid.MustParse("44444444-4444-4444-8444-444444444444"), Namespace: "team-a", SourceInstanceID: uuid.MustParse(instanceID), UserID: "alice",
		RequestID: "failed-checkpoint-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	failed, err = client.FinalizeAgentInstanceCheckpoint(ctx, failed.ID.String(), "", "tag creation failed")
	if err != nil || failed.State != "FAILED" || failed.Failure != "tag creation failed" {
		t.Fatalf("failed checkpoint = %+v, error %v", failed, err)
	}
	if err := client.DeleteAgentInstance(ctx, instanceID); err != nil {
		t.Fatal(err)
	}
	replayed, err = client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: uuid.MustParse("55555555-5555-4555-8555-555555555555"), Namespace: "team-a", SourceInstanceID: uuid.MustParse(instanceID), UserID: "alice",
		RequestID: "checkpoint-request",
	})
	if err != nil || replayed.ID != checkpoint.ID {
		t.Fatalf("checkpoint replay after source deletion = %+v, error %v", replayed, err)
	}
	listed, err := client.ListAgentInstanceCheckpoints(ctx, "team-a", instanceID, "alice", "", 10)
	if err != nil || len(listed) != 1 || listed[0].ID != checkpoint.ID {
		t.Fatalf("listed checkpoints = %+v, error %v", listed, err)
	}
	if _, err := client.BeginDeleteAgentInstanceCheckpoint(ctx, "team-a", checkpoint.ID.String(), "alice"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteAgentInstanceCheckpoint(ctx, "team-a", checkpoint.ID.String(), "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestForkAgentInstanceCopiesBoundedHistory(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()
	sourceID := "66666666-6666-4666-8666-666666666666"
	forkID := "77777777-7777-4777-8777-777777777777"
	fork2ID := "88888888-8888-4888-8888-888888888888"
	revision := dbpkg.RuntimeRevision{
		Revision: "revision-1", Namespace: "team-a",
		AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "kagent", HarnessUID: "harness-uid",
		SourceSnapshot: []byte("{}"), AgentCard: []byte(`{"name":"assistant"}`), EgressDestinations: []string{},
		ActorTemplateNamespace: "team-a", ActorTemplateName: "assistant-kagent-revision",
		ActorTemplateUID: "actor-template-uid", Phase: "Ready",
	}
	if err := client.UpsertRuntimeRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	pair := dbpkg.AgentTemplateHarnessPair{
		Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "kagent", HarnessUID: "harness-uid", DesiredRevision: revision.Revision,
		AgentTemplateLabels: map[string]string{"app": "assistant"},
	}
	if err := client.UpsertAgentTemplateHarnessPair(ctx, pair); err != nil {
		t.Fatal(err)
	}
	if err := client.MarkRuntimeRevisionSuccessful(ctx, pair); err != nil {
		t.Fatal(err)
	}

	source, _, err := client.CreateAgentInstance(ctx, &apiv1alpha1.AgentInstance{
		Id: sourceID, Namespace: "team-a", Creator: "alice",
		Harness:       &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}, "source-request")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.MarkAgentInstanceReady(ctx, source.GetId(), "source.example"); err != nil {
		t.Fatal(err)
	}
	first := newAgentInstanceTask("task-1", "message-1")
	first.History[0].ContextID = source.GetId()
	first.History[0].TaskID = first.ID
	first.History[0].ReferenceTasks = []a2a.TaskID{first.ID}
	first.Status.Message = &a2a.Message{ID: "message-1", Role: a2a.MessageRoleAgent}
	if _, _, err := client.CreateAgentInstanceTask(ctx, source.GetId(), []byte("message-request-1"), first); err != nil {
		t.Fatal(err)
	}
	first.Status.State = a2a.TaskStateCompleted
	if err := client.StoreAgentInstanceTaskEvent(ctx, source.GetId(), first, first,
		&dbpkg.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-1", UID: "snapshot-uid-1", ContentScope: "DATA"}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: uuid.MustParse("99999999-9999-4999-8999-999999999999"), Namespace: "team-a", SourceInstanceID: uuid.MustParse(source.GetId()), UserID: "alice", RequestID: "checkpoint-request-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.ID.String(), "tag-uid-1", ""); err != nil {
		t.Fatal(err)
	}

	second := newAgentInstanceTask("task-2", "message-2")
	if _, _, err := client.CreateAgentInstanceTask(ctx, source.GetId(), []byte("message-request-2"), second); err != nil {
		t.Fatal(err)
	}
	second.Status.State = a2a.TaskStateCompleted
	if err := client.StoreAgentInstanceTaskEvent(ctx, source.GetId(), second, second,
		&dbpkg.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-2", UID: "snapshot-uid-2", ContentScope: "DATA"}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteAgentInstance(ctx, source.GetId()); err != nil {
		t.Fatal(err)
	}

	fork, created, err := client.ForkAgentInstance(ctx, "team-a", checkpoint.ID.String(), "alice", "fork-request-1", forkID)
	if err != nil || !created {
		t.Fatalf("ForkAgentInstance() = %+v, created %v, error %v", fork, created, err)
	}
	if fork.GetId() != forkID || fork.GetPreparedRevision() != revision.Revision || fork.GetA2AAuthority() != "" ||
		fork.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING ||
		fork.GetHarness().GetName() != "kagent" || fork.GetAgentTemplate().GetName() != "assistant" ||
		fork.GetLabels()["app"] != "assistant" {
		t.Fatalf("fork = %+v", fork)
	}
	instances, err := client.ListAgentInstances(ctx, dbpkg.AgentInstanceQuery{
		Namespace: "team-a", UserID: "alice", Limit: 10,
	})
	if err != nil || len(instances) != 1 || instances[0].GetId() != fork.GetId() {
		t.Fatalf("listed forks = %+v, error %v", instances, err)
	}
	tasks, total, err := client.ListAgentInstanceTasks(ctx, fork.GetId(), "", a2a.TaskStateUnspecified, nil, 10)
	if err != nil || total != 1 || len(tasks) != 1 {
		t.Fatalf("fork tasks = %+v, total %d, error %v", tasks, total, err)
	}
	copied := tasks[0]
	if copied.ID == first.ID || copied.ContextID != fork.GetId() || len(copied.History) != 1 ||
		copied.History[0].ID == first.History[0].ID || copied.History[0].ContextID != fork.GetId() ||
		copied.History[0].TaskID != copied.ID || copied.History[0].ReferenceTasks[0] != copied.ID ||
		copied.Status.Message.ID != copied.History[0].ID || copied.Status.Message.TaskID != copied.ID {
		t.Fatalf("reidentified task = %+v", copied)
	}
	var initialMessageID *string
	var requestHash []byte
	var snapshotUID string
	if err := db.QueryRow(ctx, `
		SELECT initial_message_id, request_hash, snapshot_uid
		FROM agent_instance_task WHERE context_id = $1 AND id = $2
	`, fork.GetId(), copied.ID).Scan(&initialMessageID, &requestHash, &snapshotUID); err != nil {
		t.Fatal(err)
	}
	if initialMessageID != nil || requestHash != nil || snapshotUID != "snapshot-uid-1" {
		t.Fatalf("copied persistence metadata = message %v hash %v snapshot %q", initialMessageID, requestHash, snapshotUID)
	}
	replayed, created, err := client.ForkAgentInstance(ctx, "team-a", checkpoint.ID.String(), "alice", "fork-request-1", "ignored")
	if err != nil || created || replayed.GetId() != fork.GetId() {
		t.Fatalf("replayed fork = %+v, created %v, error %v", replayed, created, err)
	}
	if _, _, err := client.ForkAgentInstance(ctx, "team-a", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "alice", "fork-request-1", "ignored"); !errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		t.Fatalf("conflicting fork request error = %v", err)
	}
	if _, err := client.BeginDeleteAgentInstanceCheckpoint(ctx, "team-a", checkpoint.ID.String(), "alice"); !errors.Is(err, dbpkg.ErrNotFound) {
		t.Fatalf("delete referenced checkpoint error = %v", err)
	}

	if _, err := client.MarkAgentInstanceReady(ctx, fork.GetId(), "fork.example"); err != nil {
		t.Fatal(err)
	}
	checkpoint2, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"), Namespace: "team-a", SourceInstanceID: uuid.MustParse(fork.GetId()), UserID: "alice", RequestID: "checkpoint-request-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint2.ID.String(), "tag-uid-2", ""); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteAgentInstance(ctx, fork.GetId()); err != nil {
		t.Fatal(err)
	}
	fork2, created, err := client.ForkAgentInstance(ctx, "team-a", checkpoint2.ID.String(), "alice", "fork-request-2", fork2ID)
	if err != nil || !created || fork2.GetId() != fork2ID {
		t.Fatalf("fork of fork = %+v, created %v, error %v", fork2, created, err)
	}
}

func TestAgentInstanceCreateAndTransitions(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := context.Background()
	revision := dbpkg.RuntimeRevision{
		Revision: "revision-1", Namespace: "team-a",
		AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "kagent", HarnessUID: "harness-uid",
		SourceSnapshot: []byte("{}"), AgentCard: []byte(`{"name":"assistant"}`), EgressDestinations: []string{},
		ActorTemplateNamespace: "team-a", ActorTemplateName: "assistant-kagent-revision",
		ActorTemplateUID: "actor-template-uid", Phase: "Ready",
	}
	if err := client.UpsertRuntimeRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	storedRevision, err := client.GetRuntimeRevision(ctx, revision.Revision)
	if err != nil {
		t.Fatal(err)
	}
	var storedCard map[string]string
	if err := json.Unmarshal(storedRevision.AgentCard, &storedCard); err != nil || storedCard["name"] != "assistant" {
		t.Fatalf("GetRuntimeRevision() Agent Card = %s: %v", storedRevision.AgentCard, err)
	}
	pair := dbpkg.AgentTemplateHarnessPair{
		Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "kagent", HarnessUID: "harness-uid", DesiredRevision: revision.Revision,
	}
	if err := client.UpsertAgentTemplateHarnessPair(ctx, pair); err != nil {
		t.Fatal(err)
	}
	if err := client.MarkRuntimeRevisionSuccessful(ctx, pair); err != nil {
		t.Fatal(err)
	}

	request := &apiv1alpha1.AgentInstance{
		Id: "11111111-1111-4111-8111-111111111111", Namespace: "team-a", Creator: "alice",
		Harness:       &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}
	created, wasCreated, err := client.CreateAgentInstance(ctx, request, "request-1")
	if err != nil || !wasCreated {
		t.Fatalf("first CreateAgentInstance() = created %v, error %v", wasCreated, err)
	}
	request.Id = "22222222-2222-4222-8222-222222222222"
	replayed, wasCreated, err := client.CreateAgentInstance(ctx, request, "request-1")
	if err != nil || wasCreated {
		t.Fatalf("replayed CreateAgentInstance() = created %v, error %v", wasCreated, err)
	}
	if replayed.GetId() != created.GetId() || replayed.GetPreparedRevision() != revision.Revision {
		t.Fatalf("replayed instance = %+v, want id %q revision %q", replayed, created.GetId(), revision.Revision)
	}
	if len(replayed.GetLabels()) != 0 {
		t.Fatalf("labels = %v", replayed.GetLabels())
	}
	instances, err := client.ListAgentInstances(ctx, dbpkg.AgentInstanceQuery{
		Namespace: "team-a", UserID: "alice", Limit: 10,
	})
	if err != nil || len(instances) != 1 {
		t.Fatalf("ListAgentInstances() = %v, error %v", instances, err)
	}
	ready, err := client.MarkAgentInstanceReady(ctx, created.GetId(), "actor.example")
	if err != nil {
		t.Fatal(err)
	}
	suspending := proto.Clone(ready).(*apiv1alpha1.AgentInstance)
	suspending.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND
	if _, err := client.TransitionAgentInstance(ctx, suspending, ready.GetState(), ready.GetOperation()); err != nil {
		t.Fatal(err)
	}
	resuming := proto.Clone(ready).(*apiv1alpha1.AgentInstance)
	resuming.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME
	current, err := client.TransitionAgentInstance(ctx, resuming, ready.GetState(), ready.GetOperation())
	if !errors.Is(err, dbpkg.ErrAgentInstanceConflict) || current.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND {
		t.Fatalf("conflicting transition = instance %v, error %v", current, err)
	}
	suspended := proto.Clone(suspending).(*apiv1alpha1.AgentInstance)
	suspended.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED
	suspended.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
	if _, err := client.TransitionAgentInstance(ctx, suspended, ready.GetState(), suspending.GetOperation()); err != nil {
		t.Fatal(err)
	}

	request.AgentTemplate.Name = "different"
	if _, _, err := client.CreateAgentInstance(ctx, request, "request-1"); !errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		t.Fatalf("conflicting request error = %v", err)
	}
}

func newAgentInstanceTask(id, messageID string) *a2a.Task {
	now := time.Now()
	return &a2a.Task{
		ID: a2a.TaskID(id), ContextID: "11111111-1111-4111-8111-111111111111",
		Status:  a2a.TaskStatus{State: a2a.TaskStateWorking, Timestamp: &now},
		History: []*a2a.Message{{ID: messageID, Role: a2a.MessageRoleUser}},
	}
}

func TestInterruptActiveAgentInstanceTaskRequiresMatchingTaskAndReusesSlot(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, namespace, user_id)
		VALUES ('11111111-1111-4111-8111-111111111111', 'team-a', 'alice');
		INSERT INTO agent_instance (id, namespace, user_id, request_id, context_id, state, data)
		VALUES ('11111111-1111-4111-8111-111111111111', 'team-a', 'alice', 'request-1', '11111111-1111-4111-8111-111111111111', 'READY', '\x00')
	`); err != nil {
		t.Fatal(err)
	}
	client := NewClient(db)

	interrupted := newAgentInstanceTask("task-1", "message-1")
	if _, _, err := client.CreateAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", []byte("request-1"), interrupted); err != nil {
		t.Fatal(err)
	}

	active, err := client.GetActiveAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111")
	if err != nil || active.ID != interrupted.ID {
		t.Fatalf("GetActiveAgentInstanceTask() = %#v, %v", active, err)
	}
	if interruptedTask, err := client.InterruptActiveAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", "different-task"); err != nil || interruptedTask {
		t.Fatalf("InterruptActiveAgentInstanceTask(wrong task) = %v, %v", interruptedTask, err)
	}
	if interruptedTask, err := client.InterruptActiveAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", "task-1"); err != nil || !interruptedTask {
		t.Fatalf("InterruptActiveAgentInstanceTask() = %v, %v", interruptedTask, err)
	}

	replacement := newAgentInstanceTask("task-2", "message-2")
	stored, created, err := client.CreateAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", []byte("request-2"), replacement)
	if err != nil || !created || stored.ID != "task-2" {
		t.Fatalf("send after interruption = %#v, created %v, error %v", stored, created, err)
	}
	if interruptedTask, err := client.InterruptActiveAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", "task-1"); err != nil || interruptedTask {
		t.Fatalf("InterruptActiveAgentInstanceTask(replaced task) = %v, %v", interruptedTask, err)
	}
	replacement.Status.State = a2a.TaskStateCompleted
	if err := client.StoreAgentInstanceTaskEvent(ctx, "11111111-1111-4111-8111-111111111111", replacement, replacement, nil); err != nil {
		t.Fatal(err)
	}
	if interruptedTask, err := client.InterruptActiveAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", "task-2"); err != nil || interruptedTask {
		t.Fatalf("InterruptActiveAgentInstanceTask(terminal task) = %v, %v", interruptedTask, err)
	}

	terminated, err := client.GetAgentInstanceTask(ctx, "11111111-1111-4111-8111-111111111111", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if terminated.Status.State != a2a.TaskStateFailed {
		t.Fatalf("interrupted task state = %s, want %s", terminated.Status.State, a2a.TaskStateFailed)
	}
	if len(terminated.History) != 2 {
		t.Fatalf("interrupted task history = %d messages, want the interruption appended", len(terminated.History))
	}
	last := terminated.History[len(terminated.History)-1]
	if last.Role != a2a.MessageRoleAgent || last.TaskID != terminated.ID {
		t.Fatalf("interruption message = %#v, want an agent message on the task", last)
	}
	if terminated.Status.Message == nil || terminated.Status.Message.ID != last.ID {
		t.Fatalf("interrupted task status message = %#v, want the appended message", terminated.Status.Message)
	}
	if events := countRows(t, db,
		"SELECT COUNT(*) FROM agent_instance_task_event WHERE task_id = $1", "task-1"); events != 2 {
		t.Fatalf("events recorded for the interrupted task = %d, want the send and the interruption", events)
	}
}

// agentInstanceFixture installs a runnable agent — a template/harness pair with a
// successful revision — so instances can be created against it.
func agentInstanceFixture(t *testing.T, client dbpkg.Client, ctx context.Context, revisionID, template, harness string) {
	t.Helper()
	revision := dbpkg.RuntimeRevision{
		Revision: revisionID, Namespace: "team-a",
		AgentTemplateName: template, AgentTemplateUID: template + "-uid",
		HarnessName: harness, HarnessUID: harness + "-uid",
		SourceSnapshot: []byte("{}"), AgentCard: []byte("{}"), EgressDestinations: []string{},
		ActorTemplateNamespace: "team-a", ActorTemplateName: revisionID + "-actor-template",
		ActorTemplateUID: revisionID + "-actor-uid", Phase: "Ready",
	}
	if err := client.UpsertRuntimeRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	pair := dbpkg.AgentTemplateHarnessPair{
		Namespace: "team-a", AgentTemplateName: template, AgentTemplateUID: template + "-uid",
		HarnessName: harness, HarnessUID: harness + "-uid", DesiredRevision: revisionID,
	}
	if err := client.UpsertAgentTemplateHarnessPair(ctx, pair); err != nil {
		t.Fatal(err)
	}
	if err := client.MarkRuntimeRevisionSuccessful(ctx, pair); err != nil {
		t.Fatal(err)
	}
}

func newAgentInstanceRequest(id, template, harness, name string) *apiv1alpha1.AgentInstance {
	return &apiv1alpha1.AgentInstance{
		Id: id, Namespace: "team-a", Creator: "alice", Name: name,
		Harness:       &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: harness},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: template},
	}
}

func TestAgentInstanceNameRoundTripsAndRenames(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := context.Background()
	agentInstanceFixture(t, client, ctx, "revision-1", "assistant", "kagent")
	namedID := "11111111-1111-4111-8111-111111111111"
	unnamedID := "22222222-2222-4222-8222-222222222222"

	for _, test := range []struct {
		name     string
		id       string
		given    string
		wantName string
	}{
		{name: "a name round-trips", id: namedID, given: "Debugging the ingress", wantName: "Debugging the ingress"},
		// An instance created without a name must read back empty, which is how
		// every row written before the column existed reads.
		{name: "an omitted name stays empty", id: unnamedID, given: "", wantName: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			created, wasCreated, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(test.id, "assistant", "kagent", test.given), test.id)
			if err != nil || !wasCreated {
				t.Fatalf("CreateAgentInstance() = created %v, error %v", wasCreated, err)
			}
			if created.GetName() != test.wantName {
				t.Fatalf("created name = %q, want %q", created.GetName(), test.wantName)
			}
			read, err := client.GetAgentInstance(ctx, "team-a", test.id, "alice")
			if err != nil || read.GetName() != test.wantName {
				t.Fatalf("re-read name = %q (%v), want %q", read.GetName(), err, test.wantName)
			}
		})
	}

	renamed, err := client.UpdateAgentInstanceName(ctx, "team-a", unnamedID, "alice", "Named afterwards")
	if err != nil || renamed.GetName() != "Named afterwards" {
		t.Fatalf("UpdateAgentInstanceName() = %+v, error %v", renamed, err)
	}
	// The rename has to survive a re-read, not just be echoed back: the name lives
	// in a column while the rest of the message lives in a blob the rename does not
	// rewrite, so an echoed value proves nothing about what was stored.
	read, err := client.GetAgentInstance(ctx, "team-a", unnamedID, "alice")
	if err != nil || read.GetName() != "Named afterwards" {
		t.Fatalf("re-read after rename = %+v, error %v", read, err)
	}
	// Renaming back to empty must be possible, or a name can never be undone.
	cleared, err := client.UpdateAgentInstanceName(ctx, "team-a", unnamedID, "alice", "")
	if err != nil || cleared.GetName() != "" {
		t.Fatalf("UpdateAgentInstanceName(\"\") = %+v, error %v", cleared, err)
	}
	// A rename is scoped to the owner, so it cannot reach another reader's row.
	if _, err := client.UpdateAgentInstanceName(ctx, "team-a", namedID, "bob", "Stolen"); !errors.Is(err, dbpkg.ErrNotFound) {
		t.Fatalf("UpdateAgentInstanceName() as another user error = %v, want %v", err, dbpkg.ErrNotFound)
	}
	if _, err := client.UpdateAgentInstanceName(ctx, "team-a", "33333333-3333-4333-8333-333333333333", "alice", "Nothing"); !errors.Is(err, dbpkg.ErrNotFound) {
		t.Fatalf("UpdateAgentInstanceName() of a missing instance error = %v, want %v", err, dbpkg.ErrNotFound)
	}
}

// TestListAgentInstancesFiltersByAgentPair covers the server-side filter behind
// "this agent's conversations". The pair is resolved through the instance's
// prepared revision rather than its labels, because the labels an instance
// carries are the *template's* own Kubernetes labels and are identical for two
// harnesses admitting one template.
func TestListAgentInstancesFiltersByAgentPair(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := context.Background()
	agentInstanceFixture(t, client, ctx, "revision-1", "assistant", "kagent")
	agentInstanceFixture(t, client, ctx, "revision-2", "assistant", "claude")
	agentInstanceFixture(t, client, ctx, "revision-3", "researcher", "kagent")

	for id, pair := range map[string][2]string{
		"11111111-1111-4111-8111-111111111111": {"assistant", "kagent"},
		"22222222-2222-4222-8222-222222222222": {"assistant", "claude"},
		"33333333-3333-4333-8333-333333333333": {"researcher", "kagent"},
	} {
		if _, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(id, pair[0], pair[1], ""), id); err != nil {
			t.Fatalf("CreateAgentInstance(%s) error %v", id, err)
		}
	}

	for _, test := range []struct {
		name  string
		query dbpkg.AgentInstanceQuery
		want  []string
	}{
		{
			name:  "no filter lists every conversation",
			query: dbpkg.AgentInstanceQuery{},
			want:  []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"},
		},
		{
			name:  "one agent, which is one pair",
			query: dbpkg.AgentInstanceQuery{AgentTemplate: "assistant", Harness: "kagent"},
			want:  []string{"11111111-1111-4111-8111-111111111111"},
		},
		{
			// The case labels could never serve: one template, two harnesses, two
			// agents, and identical labels on both instances.
			name:  "the same template on a different harness is a different agent",
			query: dbpkg.AgentInstanceQuery{AgentTemplate: "assistant", Harness: "claude"},
			want:  []string{"22222222-2222-4222-8222-222222222222"},
		},
		{
			name:  "template alone spans its harnesses",
			query: dbpkg.AgentInstanceQuery{AgentTemplate: "assistant"},
			want:  []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"},
		},
		{
			name:  "harness alone spans its templates",
			query: dbpkg.AgentInstanceQuery{Harness: "kagent"},
			want:  []string{"11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333"},
		},
		{
			name:  "an unknown agent matches nothing rather than everything",
			query: dbpkg.AgentInstanceQuery{AgentTemplate: "absent", Harness: "kagent"},
			want:  []string{},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			query.Namespace, query.UserID, query.Limit = "team-a", "alice", 10
			instances, err := client.ListAgentInstances(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(instances))
			for _, instance := range instances {
				got = append(got, instance.GetId())
			}
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Fatalf("ListAgentInstances() = %v, want %v", got, test.want)
			}
		})
	}
}
