package database

import (
	"crypto/sha256"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func addUnknown(message proto.Message) {
	message.ProtoReflect().SetUnknown(protowire.AppendString(protowire.AppendTag(nil, 1000, protowire.BytesType), "future field"))
}

func TestMalformedProtobufPayloads(t *testing.T) {
	for _, test := range []struct {
		name   string
		decode func([]byte) error
	}{
		{"instance", func(data []byte) error { _, err := toAgentInstance(agentInstanceRow{Data: data}); return err }},
		{"checkpoint", func(data []byte) error {
			_, err := toAgentInstanceCheckpoint(agentInstanceCheckpointRow{Data: data})
			return err
		}},
		{"share", func(data []byte) error {
			_, err := toAgentInstanceShare(agentInstanceShareRow{Data: data})
			return err
		}},
		{"card", func(data []byte) error {
			_, err := toRuntimeRevision(runtimeRevisionRow{AgentCard: data})
			return err
		}},
		{"task", func(data []byte) error { _, err := unmarshalAgentInstanceTask(data); return err }},
		{"event", func(data []byte) error { _, err := unmarshalAgentInstanceTaskEvent(data); return err }},
	} {
		t.Run(test.name, func(t *testing.T) { require.Error(t, test.decode([]byte{0xff})) })
	}
}

func TestA2AProtobufTaskEventScope(t *testing.T) {
	pool := setupTestDB(t)
	client, q, ctx := NewClient(pool), pool, t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "original"), "create")
	require.NoError(t, err)
	instanceRow, err := readAgentInstance(ctx, q, instance.Id)
	require.NoError(t, err)
	original := &a2apb.Task{Id: "task", ContextId: instance.GetContextId(), Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_WORKING},
		Artifacts: []*a2apb.Artifact{{ArtifactId: "one", Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: "first"}}, {Content: &a2apb.Part_Text{Text: "second"}}}}, {ArtifactId: "two"}},
	}
	addUnknown(original)
	addUnknown(original.Status)
	addUnknown(original.Artifacts[0])
	addUnknown(original.Artifacts[0].Parts[0])
	reordered, err := pbconv.FromProtoTask(original)
	require.NoError(t, err)
	reordered.Artifacts[0], reordered.Artifacts[1] = reordered.Artifacts[1], reordered.Artifacts[0]
	for _, test := range []struct {
		name  string
		event a2a.Event
	}{
		{"reorder artifacts", reordered},
		{"status", &a2a.TaskStatusUpdateEvent{TaskID: "task", ContextID: instance.GetContextId(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, Metadata: map[string]any{"status": "done"}}},
		{"append", &a2a.TaskArtifactUpdateEvent{TaskID: "task", ContextID: instance.GetContextId(), Append: true, Artifact: &a2a.Artifact{ID: "one", Parts: a2a.ContentParts{a2a.NewTextPart("third")}, Metadata: map[string]any{"chunk": "last"}}}},
		{"replace parts", &a2a.TaskArtifactUpdateEvent{TaskID: "task", ContextID: instance.GetContextId(), Artifact: &a2a.Artifact{ID: "one", Parts: a2a.ContentParts{a2a.NewTextPart("second"), a2a.NewTextPart("first")}}}},
		{"snapshot", &a2a.Task{ID: "task", ContextID: instance.GetContextId(), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}, Artifacts: []*a2a.Artifact{{ID: "one", Parts: a2a.ContentParts{a2a.NewTextPart("replacement")}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			task, err := pbconv.FromProtoTask(original)
			require.NoError(t, err)
			next, err := a2aevent.ApplyUpdate(task, test.event)
			require.NoError(t, err)
			data, err := proto.Marshal(original)
			require.NoError(t, err)
			_, err = saveTaskProjection(ctx, q, instanceRow.HistoryID, original.Id, string(a2a.TaskStateWorking), nil, data)
			require.NoError(t, err)
			require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, next, test.event, nil))
			row, err := readAgentInstanceTask(ctx, q, instanceRow.HistoryID, original.Id)
			require.NoError(t, err)
			require.Equal(t, string(next.Status.State), row.State)
			got := &a2apb.Task{}
			require.NoError(t, proto.Unmarshal(row.Data, got))
			require.Equal(t, original.ProtoReflect().GetUnknown(), got.ProtoReflect().GetUnknown())
			require.Equal(t, original.Status.ProtoReflect().GetUnknown(), got.Status.ProtoReflect().GetUnknown())
			switch test.name {
			case "reorder artifacts":
				require.True(t, proto.Equal(original.Artifacts[0], got.Artifacts[1]))
			case "status", "append":
				require.Equal(t, original.Artifacts[0].ProtoReflect().GetUnknown(), got.Artifacts[0].ProtoReflect().GetUnknown())
				require.True(t, proto.Equal(original.Artifacts[0].Parts[0], got.Artifacts[0].Parts[0]))
			default:
				// Replacing content must not transplant fields from the previous parts.
				require.Empty(t, got.Artifacts[0].ProtoReflect().GetUnknown())
				for _, part := range got.Artifacts[0].Parts {
					require.Empty(t, part.ProtoReflect().GetUnknown())
				}
			}
		})
	}
	data, err := proto.Marshal(original)
	require.NoError(t, err)
	_, err = saveTaskProjection(ctx, q, instanceRow.HistoryID, original.Id, string(a2a.TaskStateWorking), nil, data)
	require.NoError(t, err)
	interrupted, err := client.InterruptActiveAgentInstanceTask(ctx, instance.Id, original.Id)
	require.NoError(t, err)
	require.True(t, interrupted)
	row, err := readAgentInstanceTask(ctx, q, instanceRow.HistoryID, original.Id)
	require.NoError(t, err)
	got := &a2apb.Task{}
	require.NoError(t, proto.Unmarshal(row.Data, got))
	require.Equal(t, original.ProtoReflect().GetUnknown(), got.ProtoReflect().GetUnknown())
	require.Equal(t, original.Status.ProtoReflect().GetUnknown(), got.Status.ProtoReflect().GetUnknown())
	require.True(t, proto.Equal(original.Artifacts[0], got.Artifacts[0]))
	require.Equal(t, string(a2a.TaskStateFailed), row.State)
	require.Equal(t, a2apb.TaskState_TASK_STATE_FAILED, got.Status.State)
	require.Equal(t, row.StatusTimestamp.UnixMicro(), got.Status.Timestamp.AsTime().UnixMicro())

	task, err := pbconv.FromProtoTask(original)
	require.NoError(t, err)
	event := &a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	_, _, err = taskTransition(proto.Clone(original).(*a2apb.Task), task, event)
	// The supplied projection still says working.
	require.ErrorContains(t, err, "task status does not match event")
	_, _, err = taskTransition(proto.Clone(original).(*a2apb.Task), task, &a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Append: true, Artifact: &a2a.Artifact{ID: "one", Parts: a2a.ContentParts{a2a.NewTextPart("new")}}})
	require.ErrorContains(t, err, "projection does not match event")
}

func TestProtobufPersistenceLifecycle(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool)
	q := pool
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	request := newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "original")
	addUnknown(request)
	addUnknown(request.Harness)
	instance, _, err := client.CreateAgentInstance(ctx, request, "create")
	require.NoError(t, err)
	_, err = client.UpdateAgentInstanceName(ctx, instance.Id, "alice", "renamed while creating")
	require.NoError(t, err)
	instance, err = client.MarkAgentInstanceReady(ctx, instance.Id, "runtime:80")
	require.NoError(t, err)
	require.Equal(t, "renamed while creating", instance.Name)
	stale := proto.Clone(instance).(*apiv1alpha1.AgentInstance)
	_, err = client.UpdateAgentInstanceName(ctx, instance.Id, "alice", "renamed again")
	require.NoError(t, err)
	stale.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED
	stale.Creator, stale.PreparedRevision = "mallory", "invalid"
	instance, err = client.TransitionAgentInstance(ctx, stale, instance.State, instance.Operation)
	require.NoError(t, err)
	require.Equal(t, "renamed again", instance.Name)
	require.Equal(t, "alice", instance.Creator)
	require.Equal(t, "revision", instance.PreparedRevision)
	row, err := readAgentInstance(ctx, q, instance.Id)
	require.NoError(t, err)
	stored := &apiv1alpha1.AgentInstance{}
	require.NoError(t, proto.Unmarshal(row.Data, stored))
	require.True(t, proto.Equal(instance, stored))
	require.Equal(t, request.ProtoReflect().GetUnknown(), stored.ProtoReflect().GetUnknown())
	require.Equal(t, request.Harness.ProtoReflect().GetUnknown(), stored.Harness.ProtoReflect().GetUnknown())
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
	instance, err = client.TransitionAgentInstance(ctx, instance, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED, instance.Operation)
	require.NoError(t, err)

	task := &a2a.Task{ID: "task", ContextID: instance.GetContextId(), Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
		History: []*a2a.Message{{ID: "message", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("hello")}}},
	}
	_, _, err = client.CreateAgentInstanceTask(ctx, instance.Id, []byte("request hash"), task)
	require.NoError(t, err)
	// Simulate a newer writer using the same binary SQL boundary.
	taskRow, err := readAgentInstanceTask(ctx, q, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	futureTask := &a2apb.Task{}
	require.NoError(t, proto.Unmarshal(taskRow.Data, futureTask))
	addUnknown(futureTask)
	addUnknown(futureTask.Status)
	futureTask.Status.Message = &a2apb.Message{MessageId: "question", Role: a2apb.Role_ROLE_AGENT, TaskId: string(task.ID), ContextId: instance.GetContextId(),
		Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: "question"}}}}
	addUnknown(futureTask.Status.Message)
	addUnknown(futureTask.Status.Message.Parts[0])
	futureData, err := proto.Marshal(futureTask)
	require.NoError(t, err)
	_, err = saveTaskProjection(ctx, q, taskRow.HistoryID, taskRow.ID, taskRow.State, taskRow.StatusTimestamp, futureData)
	require.NoError(t, err)
	futureEvent, err := proto.Marshal(&a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Task{Task: futureTask}})
	require.NoError(t, err)
	_, err = insertTaskEvent(ctx, q, taskEventWrite{
		HistoryID: taskRow.HistoryID,
		TaskID:    &taskRow.ID,
		Data:      futureEvent,
	})
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, &a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: task.Status}, &AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot", ContentScope: "DATA"}))
	checkpointRequest := &apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: instance.Id}
	addUnknown(checkpointRequest)
	checkpoint, _, err := client.ReserveAgentInstanceCheckpoint(ctx, checkpointRequest, "alice", "checkpoint")
	require.NoError(t, err)
	checkpoint, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.Id, "tag-uid", "s3://tags/checkpoint", "")
	require.NoError(t, err)
	checkpointRow, err := readCheckpoint(ctx, q, checkpoint.Id, "alice", new("READY"))
	require.NoError(t, err)
	storedCheckpoint := &apiv1alpha1.Checkpoint{}
	require.NoError(t, proto.Unmarshal(checkpointRow.Data, storedCheckpoint))
	require.True(t, proto.Equal(checkpoint, storedCheckpoint))
	require.Equal(t, checkpointRequest.ProtoReflect().GetUnknown(), checkpoint.ProtoReflect().GetUnknown())
	_, err = client.GetAgentInstanceCheckpoint(ctx, checkpoint.Id, "mallory")
	require.ErrorIs(t, err, ErrNotFound)
	fork, created, err := client.ForkAgentInstance(ctx, checkpoint.Id, "alice", "fork", uuid.NewString())
	require.NoError(t, err)
	require.True(t, created)
	tasks, total, err := client.ListAgentInstanceTasks(ctx, fork.Id, "", a2a.TaskStateUnspecified, nil, 10)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, tasks[0].History, 2)
	forkRow, err := readAgentInstance(ctx, q, fork.Id)
	require.NoError(t, err)
	forkHistory, err := readTaskMessages(ctx, q, forkRow.HistoryID, []string{string(tasks[0].ID)})
	require.NoError(t, err)
	question := &a2apb.StreamResponse{}
	require.NoError(t, proto.Unmarshal(forkHistory[1].Data, question))
	require.Equal(t, futureTask.Status.Message.ProtoReflect().GetUnknown(), question.GetMessage().ProtoReflect().GetUnknown())
	require.Equal(t, futureTask.Status.Message.Parts[0].ProtoReflect().GetUnknown(), question.GetMessage().Parts[0].ProtoReflect().GetUnknown())
	require.Equal(t, task.ID, tasks[0].ID)
	forkTaskRow, err := readAgentInstanceTask(ctx, q, forkRow.HistoryID, string(tasks[0].ID))
	require.NoError(t, err)
	forkTask := &a2apb.Task{}
	require.NoError(t, proto.Unmarshal(forkTaskRow.Data, forkTask))
	require.Equal(t, futureTask.ProtoReflect().GetUnknown(), forkTask.ProtoReflect().GetUnknown())
	require.Equal(t, futureTask.Status.ProtoReflect().GetUnknown(), forkTask.Status.ProtoReflect().GetUnknown())
	require.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, forkTask.Status.State)
	require.NoError(t, client.DeleteAgentInstance(ctx, fork.Id))
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice")
	require.NoError(t, err)
	checkpointRow, err = readCheckpoint(ctx, q, checkpoint.Id, "alice", nil)
	require.NoError(t, err)
	deleting := &apiv1alpha1.Checkpoint{}
	require.NoError(t, proto.Unmarshal(checkpointRow.Data, deleting))
	require.Equal(t, checkpointRequest.ProtoReflect().GetUnknown(), deleting.ProtoReflect().GetUnknown())
	require.Equal(t, apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING, deleting.State)
	require.NoError(t, client.DeleteAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice"))
	_, err = client.GetAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestShareAndAgentCardProtobufPersistence(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool)
	ctx := t.Context()
	card, err := pbconv.ToProtoAgentCard(&a2a.AgentCard{Name: "assistant", Description: "assistant", Version: "v1", SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface("http://runtime", a2a.TransportProtocolGRPC)}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"}, Skills: []a2a.AgentSkill{{ID: "skill", Name: "skill", Description: "skill", Tags: []string{"tag"}}}, Capabilities: a2a.AgentCapabilities{Streaming: true}})
	require.NoError(t, err)
	addUnknown(card)
	revision := RuntimeRevision{Revision: "revision", Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "template", HarnessName: "kagent", HarnessUID: "harness", SourceSnapshot: []byte(`{}`), AgentCard: card, EgressDestinations: []string{}, ActorTemplateAtespace: "team-a", ActorTemplateName: "template"}
	require.NoError(t, client.UpsertRuntimeRevision(ctx, revision))
	// Reconciliation can update runtime identity, but the pinned card is immutable.
	revision.AgentCard = &a2apb.AgentCard{Name: "replacement"}
	revision.ActorTemplateUID = "new-uid"
	require.NoError(t, client.UpsertRuntimeRevision(ctx, revision))
	stored, err := client.GetRuntimeRevision(ctx, "revision")
	require.NoError(t, err)
	require.True(t, proto.Equal(card, stored.AgentCard))
	require.Equal(t, "new-uid", stored.ActorTemplateUID)
	harnesses, err := client.ListActorTemplateHarnesses(ctx)
	require.NoError(t, err)
	require.Equal(t, []ActorTemplateHarness{{Atespace: "team-a", Name: "template", UID: "new-uid", HarnessName: "kagent"}}, harnesses)
	cards, err := client.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.True(t, proto.Equal(card, cards[0].AgentCard))
	rendered, err := pbconv.FromProtoAgentCard(stored.AgentCard)
	require.NoError(t, err)
	require.Equal(t, "assistant", rendered.Name)
	require.True(t, rendered.Capabilities.Streaming)
	agentInstanceFixture(t, client, ctx, "team-a", "instance-revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), "create")
	require.NoError(t, err)
	for _, permission := range []apiv1alpha1.AgentInstanceSharePermission{apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_ONLY, apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_WRITE} {
		digest := sha256.Sum256([]byte(permission.String()))
		value := &apiv1alpha1.AgentInstanceShare{Id: uuid.NewString(), AgentInstanceId: instance.Id, Permission: permission}
		addUnknown(value)
		share, err := client.CreateAgentInstanceShare(ctx, value, digest[:])
		require.NoError(t, err)
		require.Equal(t, value.ProtoReflect().GetUnknown(), share.ProtoReflect().GetUnknown())
		resolved, ownerUserID, err := client.GetAgentInstanceShareByTokenHash(ctx, digest[:])
		require.NoError(t, err)
		require.True(t, proto.Equal(share, resolved))
		require.Equal(t, "alice", ownerUserID)
		listed, err := client.ListAgentInstanceShares(ctx, instance.Id, "alice", "", 10)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		require.True(t, proto.Equal(share, listed[0]))
		listed, err = client.ListAgentInstanceShares(ctx, instance.Id, "mallory", "", 10)
		require.NoError(t, err)
		require.Empty(t, listed)
		require.ErrorIs(t, client.DeleteAgentInstanceShare(ctx, share.Id, "mallory"), ErrNotFound)
		require.NoError(t, client.DeleteAgentInstanceShare(ctx, share.Id, "alice"))
		_, _, err = client.GetAgentInstanceShareByTokenHash(ctx, digest[:])
		require.ErrorIs(t, err, ErrNotFound)
	}
}

func TestProtobufRowsRejectInconsistentIndexes(t *testing.T) {
	id := uuid.New()
	checkpoint := &apiv1alpha1.Checkpoint{Id: id.String(), AgentInstanceId: id.String(), State: apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY}
	data, err := proto.Marshal(checkpoint)
	require.NoError(t, err)
	_, err = toAgentInstanceCheckpoint(agentInstanceCheckpointRow{ID: id, SourceInstanceID: id, State: "CREATING", Data: data})
	require.ErrorContains(t, err, "disagrees with indexed columns")
	share := &apiv1alpha1.AgentInstanceShare{Id: id.String(), AgentInstanceId: id.String(), Permission: apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_WRITE}
	data, err = proto.Marshal(share)
	require.NoError(t, err)
	_, err = toAgentInstanceShare(agentInstanceShareRow{ID: id, InstanceID: id, Permission: "READ_ONLY", Data: data})
	require.ErrorContains(t, err, "disagrees with indexed columns")
}
