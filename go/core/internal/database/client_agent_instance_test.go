package database

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/gen"
	"google.golang.org/protobuf/proto"
)

func TestToAgentInstanceUsesIndexedLifecycleColumns(t *testing.T) {
	data, err := proto.Marshal(&apiv1alpha1.AgentInstance{
		Id:        "instance-1",
		State:     apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
		Operation: apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED,
	})
	if err != nil {
		t.Fatal(err)
	}

	instance, err := toAgentInstance(dbgen.AgentInstance{
		ID: "instance-1", Data: data, State: "SUSPENDED", Operation: "RESUME",
	})
	if err != nil {
		t.Fatal(err)
	}
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED ||
		instance.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME {
		t.Fatalf("lifecycle = %s/%s, want SUSPENDED/RESUME", instance.GetState(), instance.GetOperation())
	}
}

func TestAgentInstanceTasksAreDurableAndExclusive(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, namespace, user_id)
		VALUES ('instance-1', 'team-a', 'alice');

		INSERT INTO agent_instance (id, namespace, user_id, request_id, context_id, state, data)
		VALUES ('instance-1', 'team-a', 'alice', 'request-1', 'instance-1', 'READY', '\x00')
	`); err != nil {
		t.Fatal(err)
	}
	client := NewClient(db)
	now := time.Now()
	first := &a2a.Task{
		ID: "task-1", ContextID: "instance-1",
		Status:  a2a.TaskStatus{State: a2a.TaskStateSubmitted, Timestamp: &now},
		History: []*a2a.Message{{ID: "message-1", Role: a2a.MessageRoleUser}},
	}
	stored, created, err := client.CreateAgentInstanceTask(ctx, "instance-1", []byte("request-1"), first)
	if err != nil || !created || stored.ID != first.ID {
		t.Fatalf("CreateAgentInstanceTask() = %#v, created %v, error %v", stored, created, err)
	}
	replayed, created, err := client.CreateAgentInstanceTask(ctx, "instance-1", []byte("request-1"),
		&a2a.Task{ID: "ignored", ContextID: "instance-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}, History: first.History})
	if err != nil || created || replayed.ID != first.ID {
		t.Fatalf("replayed CreateAgentInstanceTask() = %#v, created %v, error %v", replayed, created, err)
	}
	if _, _, err := client.CreateAgentInstanceTask(ctx, "instance-1", []byte("different"), first); !errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		t.Fatalf("conflicting message error = %v", err)
	}
	if events := countRows(t, db, "SELECT COUNT(*) FROM agent_instance_task_event"); events != 1 {
		t.Fatalf("event count after retries = %d, want 1", events)
	}
	var eventTaskID string
	if err := db.QueryRow(ctx, "SELECT task_id FROM agent_instance_task_event").Scan(&eventTaskID); err != nil || eventTaskID != string(first.ID) {
		t.Fatalf("initial event task ID = %q, want %q: %v", eventTaskID, first.ID, err)
	}
	got, err := client.GetAgentInstanceTask(ctx, "instance-1", "task-1")
	if err != nil || got.ID != first.ID || got.Status.State != first.Status.State || len(got.History) != 1 {
		t.Fatalf("GetAgentInstanceTask() = %#v, %v", got, err)
	}
	var projectionData []byte
	if err := db.QueryRow(ctx, `SELECT data FROM agent_instance_task WHERE context_id = 'instance-1' AND id = 'task-1'`).Scan(&projectionData); err != nil {
		t.Fatal(err)
	}
	projection, err := unmarshalAgentInstanceTask(projectionData)
	if err != nil || len(projection.History) != 0 {
		t.Fatalf("stored task projection history = %#v, error %v", projection.History, err)
	}
	second := &a2a.Task{ID: "task-2", ContextID: "instance-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}
	if err := client.StoreAgentInstanceTaskEvent(ctx, "instance-1", second, second, nil); !errors.Is(err, dbpkg.ErrAgentInstanceTaskConflict) {
		t.Fatalf("second active task error = %v", err)
	}
	first.History = append(first.History, a2a.NewMessageForTask(a2a.MessageRoleAgent, first, a2a.NewTextPart("done")))
	first.Status.State = a2a.TaskStateCompleted
	snapshot := &dbpkg.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-1", UID: "snapshot-uid"}
	if err := client.StoreAgentInstanceTaskEvent(ctx, "instance-1", first, first, snapshot); err != nil {
		t.Fatal(err)
	}
	var snapshotAtespace, snapshotName, snapshotUID string
	var historySequence, latestSequence int64
	if err := db.QueryRow(ctx, `
		SELECT snapshot_atespace, snapshot_name, snapshot_uid, history_sequence
		FROM agent_instance_task WHERE context_id = 'instance-1' AND id = 'task-1'
	`).Scan(&snapshotAtespace, &snapshotName, &snapshotUID, &historySequence); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT MAX(sequence) FROM agent_instance_task_event`).Scan(&latestSequence); err != nil {
		t.Fatal(err)
	}
	if snapshotAtespace != snapshot.Atespace || snapshotName != snapshot.Name || snapshotUID != snapshot.UID || historySequence != latestSequence {
		t.Fatalf("stored boundary = %s/%s uid %s sequence %d", snapshotAtespace, snapshotName, snapshotUID, historySequence)
	}
	got, err = client.GetAgentInstanceTask(ctx, "instance-1", "task-1")
	if err != nil || len(got.History) != 2 || got.History[1].Role != a2a.MessageRoleAgent {
		t.Fatalf("reconstructed task history = %#v, error %v", got, err)
	}
	if err := client.StoreAgentInstanceTaskEvent(ctx, "instance-1", second, second, nil); err != nil {
		t.Fatal(err)
	}
	if events := countRows(t, db, "SELECT COUNT(*) FROM agent_instance_task_event"); events != 4 {
		t.Fatalf("event count = %d, want 4", events)
	}

	tasks, total, err := client.ListAgentInstanceTasks(ctx, "instance-1", "", a2a.TaskStateUnspecified, nil, 1)
	if err != nil || total != 2 || len(tasks) != 1 || tasks[0].ID != first.ID {
		t.Fatalf("first page = %#v, total %d, error %v", tasks, total, err)
	}
	tasks, total, err = client.ListAgentInstanceTasks(ctx, "instance-1", string(first.ID), a2a.TaskStateSubmitted, nil, 2)
	if err != nil || total != 1 || len(tasks) != 1 || tasks[0].ID != second.ID {
		t.Fatalf("filtered page = %#v, total %d, error %v", tasks, total, err)
	}
}

func TestConcurrentAgentInstanceMessageReplay(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, namespace, user_id)
		VALUES ('instance-1', 'team-a', 'alice');
		INSERT INTO agent_instance (id, namespace, user_id, request_id, context_id, state, data)
		VALUES ('instance-1', 'team-a', 'alice', 'request-1', 'instance-1', 'READY', '\x00')
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
			message := &a2a.Message{ID: "message-1", Role: a2a.MessageRoleUser, TaskID: taskID, ContextID: "instance-1"}
			task := a2a.NewSubmittedTask(message, message)
			stored, created, err := client.CreateAgentInstanceTask(ctx, "instance-1", []byte("request-1"), task)
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

func TestAgentInstanceCheckpointRetainsRecordedBoundary(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	instance := &apiv1alpha1.AgentInstance{
		Id: "instance-1", Namespace: "team-a", Creator: "alice",
		State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
	}
	instanceData, err := proto.Marshal(instance)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, namespace, user_id)
		VALUES ('instance-1', 'team-a', 'alice')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO agent_instance (id, namespace, user_id, request_id, context_id, state, data)
		VALUES ('instance-1', 'team-a', 'alice', 'instance-request', 'instance-1', 'READY', $1)
	`, instanceData); err != nil {
		t.Fatal(err)
	}
	client := NewClient(db)
	task := newAgentInstanceTask("task-1", "message-1")
	if _, _, err := client.CreateAgentInstanceTask(ctx, "instance-1", []byte("message-request"), task); err != nil {
		t.Fatal(err)
	}
	task.Status.State = a2a.TaskStateCompleted
	if err := client.StoreAgentInstanceTaskEvent(ctx, "instance-1", task, task,
		&dbpkg.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-1", UID: "snapshot-uid", ContentScope: "DATA"}); err != nil {
		t.Fatal(err)
	}

	checkpoint, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: "checkpoint-1", Namespace: "team-a", SourceInstanceID: "instance-1", UserID: "alice",
		RequestID: "checkpoint-request",
	})
	if err != nil {
		t.Fatalf("ReserveAgentInstanceCheckpoint() = %+v, error %v", checkpoint, err)
	}
	if checkpoint.HeadTaskID != "task-1" || checkpoint.SnapshotUID != "snapshot-uid" ||
		checkpoint.SnapshotContentScope != "DATA" || checkpoint.HistorySequence == 0 {
		t.Fatalf("checkpoint boundary = %+v", checkpoint)
	}
	if _, _, err := client.CreateAgentInstanceTask(ctx, "instance-1", []byte("blocked-request"), newAgentInstanceTask("task-2", "message-2")); !errors.Is(err, dbpkg.ErrAgentInstanceTaskConflict) {
		t.Fatalf("CreateAgentInstanceTask() during checkpoint = %v, want %v", err, dbpkg.ErrAgentInstanceTaskConflict)
	}
	suspending := proto.Clone(instance).(*apiv1alpha1.AgentInstance)
	suspending.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND
	current, err := client.TransitionAgentInstance(ctx, suspending, instance.GetState(), instance.GetOperation())
	if !errors.Is(err, dbpkg.ErrAgentInstanceConflict) || current.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		t.Fatalf("lifecycle transition during checkpoint = %+v, error %v", current, err)
	}
	replayed, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: "ignored", Namespace: "team-a", SourceInstanceID: "instance-1", UserID: "alice",
		RequestID: "checkpoint-request",
	})
	if err != nil || replayed.ID != checkpoint.ID {
		t.Fatalf("replayed checkpoint = %+v, error %v", replayed, err)
	}
	ready, err := client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.ID, "tag-uid", "")
	if err != nil || ready.State != "READY" || ready.TagUID != "tag-uid" {
		t.Fatalf("ready checkpoint = %+v, error %v", ready, err)
	}
	if replayed, err := client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.ID, "tag-uid", ""); err != nil || replayed.State != "READY" {
		t.Fatalf("replayed ready checkpoint = %+v, error %v", replayed, err)
	}
	failed, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: "checkpoint-2", Namespace: "team-a", SourceInstanceID: "instance-1", UserID: "alice",
		RequestID: "failed-checkpoint-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	failed, err = client.FinalizeAgentInstanceCheckpoint(ctx, failed.ID, "", "tag creation failed")
	if err != nil || failed.State != "FAILED" || failed.Failure != "tag creation failed" {
		t.Fatalf("failed checkpoint = %+v, error %v", failed, err)
	}
	if err := client.DeleteAgentInstance(ctx, "instance-1"); err != nil {
		t.Fatal(err)
	}
	replayed, err = client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: "ignored-again", Namespace: "team-a", SourceInstanceID: "instance-1", UserID: "alice",
		RequestID: "checkpoint-request",
	})
	if err != nil || replayed.ID != checkpoint.ID {
		t.Fatalf("checkpoint replay after source deletion = %+v, error %v", replayed, err)
	}
	listed, err := client.ListAgentInstanceCheckpoints(ctx, "team-a", "instance-1", "alice", "", 10)
	if err != nil || len(listed) != 1 || listed[0].ID != checkpoint.ID {
		t.Fatalf("listed checkpoints = %+v, error %v", listed, err)
	}
	if _, err := client.BeginDeleteAgentInstanceCheckpoint(ctx, "team-a", checkpoint.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteAgentInstanceCheckpoint(ctx, "team-a", checkpoint.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestForkAgentInstanceCopiesBoundedHistory(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
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
		Id: "instance-1", Namespace: "team-a", Creator: "alice",
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
		ID: "checkpoint-1", Namespace: "team-a", SourceInstanceID: source.GetId(), UserID: "alice", RequestID: "checkpoint-request-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.ID, "tag-uid-1", ""); err != nil {
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

	fork, created, err := client.ForkAgentInstance(ctx, "team-a", checkpoint.ID, "alice", "fork-request-1", "fork-1")
	if err != nil || !created {
		t.Fatalf("ForkAgentInstance() = %+v, created %v, error %v", fork, created, err)
	}
	if fork.GetId() != "fork-1" || fork.GetPreparedRevision() != revision.Revision || fork.GetA2AAuthority() != "" ||
		fork.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING ||
		fork.GetHarness().GetName() != "kagent" || fork.GetAgentTemplate().GetName() != "assistant" ||
		fork.GetLabels()["app"] != "assistant" {
		t.Fatalf("fork = %+v", fork)
	}
	instances, err := client.ListAgentInstances(ctx, "team-a", "alice", false, nil, "", 10)
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
	replayed, created, err := client.ForkAgentInstance(ctx, "team-a", checkpoint.ID, "alice", "fork-request-1", "ignored")
	if err != nil || created || replayed.GetId() != fork.GetId() {
		t.Fatalf("replayed fork = %+v, created %v, error %v", replayed, created, err)
	}
	if _, _, err := client.ForkAgentInstance(ctx, "team-a", "other-checkpoint", "alice", "fork-request-1", "ignored"); !errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		t.Fatalf("conflicting fork request error = %v", err)
	}
	if _, err := client.BeginDeleteAgentInstanceCheckpoint(ctx, "team-a", checkpoint.ID, "alice"); !errors.Is(err, dbpkg.ErrNotFound) {
		t.Fatalf("delete referenced checkpoint error = %v", err)
	}

	if _, err := client.MarkAgentInstanceReady(ctx, fork.GetId(), "fork.example"); err != nil {
		t.Fatal(err)
	}
	checkpoint2, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{
		ID: "checkpoint-2", Namespace: "team-a", SourceInstanceID: fork.GetId(), UserID: "alice", RequestID: "checkpoint-request-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint2.ID, "tag-uid-2", ""); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteAgentInstance(ctx, fork.GetId()); err != nil {
		t.Fatal(err)
	}
	fork2, created, err := client.ForkAgentInstance(ctx, "team-a", checkpoint2.ID, "alice", "fork-request-2", "fork-2")
	if err != nil || !created || fork2.GetId() != "fork-2" {
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
		Id: "instance-1", Namespace: "team-a", Creator: "alice",
		Harness:       &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}
	created, wasCreated, err := client.CreateAgentInstance(ctx, request, "request-1")
	if err != nil || !wasCreated {
		t.Fatalf("first CreateAgentInstance() = created %v, error %v", wasCreated, err)
	}
	request.Id = "instance-2"
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
	instances, err := client.ListAgentInstances(ctx, "team-a", "alice", false, nil, "", 10)
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
		ID: a2a.TaskID(id), ContextID: "instance-1",
		Status:  a2a.TaskStatus{State: a2a.TaskStateWorking, Timestamp: &now},
		History: []*a2a.Message{{ID: messageID, Role: a2a.MessageRoleUser}},
	}
}

func TestInterruptActiveAgentInstanceTaskRequiresMatchingTaskAndReusesSlot(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, namespace, user_id)
		VALUES ('instance-1', 'team-a', 'alice');
		INSERT INTO agent_instance (id, namespace, user_id, request_id, context_id, state, data)
		VALUES ('instance-1', 'team-a', 'alice', 'request-1', 'instance-1', 'READY', '\x00')
	`); err != nil {
		t.Fatal(err)
	}
	client := NewClient(db)

	interrupted := newAgentInstanceTask("task-1", "message-1")
	if _, _, err := client.CreateAgentInstanceTask(ctx, "instance-1", []byte("request-1"), interrupted); err != nil {
		t.Fatal(err)
	}

	active, err := client.GetActiveAgentInstanceTask(ctx, "instance-1")
	if err != nil || active.ID != interrupted.ID {
		t.Fatalf("GetActiveAgentInstanceTask() = %#v, %v", active, err)
	}
	if interruptedTask, err := client.InterruptActiveAgentInstanceTask(ctx, "instance-1", "different-task"); err != nil || interruptedTask {
		t.Fatalf("InterruptActiveAgentInstanceTask(wrong task) = %v, %v", interruptedTask, err)
	}
	if interruptedTask, err := client.InterruptActiveAgentInstanceTask(ctx, "instance-1", "task-1"); err != nil || !interruptedTask {
		t.Fatalf("InterruptActiveAgentInstanceTask() = %v, %v", interruptedTask, err)
	}

	replacement := newAgentInstanceTask("task-2", "message-2")
	stored, created, err := client.CreateAgentInstanceTask(ctx, "instance-1", []byte("request-2"), replacement)
	if err != nil || !created || stored.ID != "task-2" {
		t.Fatalf("send after interruption = %#v, created %v, error %v", stored, created, err)
	}
	if interruptedTask, err := client.InterruptActiveAgentInstanceTask(ctx, "instance-1", "task-1"); err != nil || interruptedTask {
		t.Fatalf("InterruptActiveAgentInstanceTask(replaced task) = %v, %v", interruptedTask, err)
	}
	replacement.Status.State = a2a.TaskStateCompleted
	if err := client.StoreAgentInstanceTaskEvent(ctx, "instance-1", replacement, replacement, nil); err != nil {
		t.Fatal(err)
	}
	if interruptedTask, err := client.InterruptActiveAgentInstanceTask(ctx, "instance-1", "task-2"); err != nil || interruptedTask {
		t.Fatalf("InterruptActiveAgentInstanceTask(terminal task) = %v, %v", interruptedTask, err)
	}

	terminated, err := client.GetAgentInstanceTask(ctx, "instance-1", "task-1")
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
