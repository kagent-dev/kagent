package database

import (
	"crypto/sha256"
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/gen"
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
		{"instance", func(data []byte) error { _, err := toAgentInstance(dbgen.AgentInstance{Data: data}); return err }},
		{"checkpoint", func(data []byte) error {
			_, err := toAgentInstanceCheckpoint(dbgen.AgentInstanceCheckpoint{Data: data})
			return err
		}},
		{"share", func(data []byte) error {
			_, err := toAgentInstanceShare(dbgen.AgentInstanceShare{Data: data})
			return err
		}},
		{"card", func(data []byte) error {
			_, err := toRuntimeRevision(dbgen.RuntimeRevision{AgentCard: data})
			return err
		}},
		{"task", func(data []byte) error { _, err := unmarshalAgentInstanceTask(data); return err }},
		{"event", func(data []byte) error { _, err := unmarshalAgentInstanceTaskEvent(data); return err }},
	} {
		t.Run(test.name, func(t *testing.T) { require.Error(t, test.decode([]byte{0xff})) })
	}
}

func TestA2AProtobufUpdatesRetainUnknownFields(t *testing.T) {
	original := &a2apb.Task{Id: "task", ContextId: "context", Status: &a2apb.TaskStatus{State: a2apb.TaskState_TASK_STATE_WORKING},
		Artifacts: []*a2apb.Artifact{{ArtifactId: "one", Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: "text"}}}}, {ArtifactId: "two"}},
	}
	addUnknown(original)
	addUnknown(original.Status)
	addUnknown(original.Artifacts[0])
	addUnknown(original.Artifacts[0].Parts[0])
	data, err := proto.Marshal(original)
	require.NoError(t, err)
	task, err := unmarshalAgentInstanceTask(data)
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	task.Artifacts[0], task.Artifacts[1] = task.Artifacts[1], task.Artifacts[0]
	updated, err := marshalAgentInstanceTask(task, data)
	require.NoError(t, err)
	got := &a2apb.Task{}
	require.NoError(t, proto.Unmarshal(updated, got))
	require.Equal(t, original.ProtoReflect().GetUnknown(), got.ProtoReflect().GetUnknown())
	require.Equal(t, original.Status.ProtoReflect().GetUnknown(), got.Status.ProtoReflect().GetUnknown())
	require.Equal(t, original.Artifacts[0].ProtoReflect().GetUnknown(), got.Artifacts[1].ProtoReflect().GetUnknown())
	require.Equal(t, original.Artifacts[0].Parts[0].ProtoReflect().GetUnknown(), got.Artifacts[1].Parts[0].ProtoReflect().GetUnknown())
	require.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, got.Status.State)
	_, err = marshalAgentInstanceTask(task, []byte{0xff})
	require.Error(t, err)

	event := &a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Task{Task: original}}
	addUnknown(event)
	eventData, err := proto.Marshal(event)
	require.NoError(t, err)
	decoded, err := unmarshalAgentInstanceTaskEvent(eventData)
	require.NoError(t, err)
	updatedEvent, err := marshalAgentInstanceTaskEvent(decoded, eventData)
	require.NoError(t, err)
	gotEvent := &a2apb.StreamResponse{}
	require.NoError(t, proto.Unmarshal(updatedEvent, gotEvent))
	require.True(t, proto.Equal(event, gotEvent))
}

func TestProtobufPersistenceLifecycle(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool)
	q := dbgen.New(pool)
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
	stale.Creator, stale.PreparedRevision, stale.Labels = "mallory", "invalid", map[string]string{"invalid": "value"}
	instance, err = client.TransitionAgentInstance(ctx, stale, instance.State, instance.Operation)
	require.NoError(t, err)
	require.Equal(t, "renamed again", instance.Name)
	require.Equal(t, "alice", instance.Creator)
	require.Equal(t, "revision", instance.PreparedRevision)
	require.Empty(t, instance.Labels)
	row, err := q.GetAgentInstanceByID(ctx, uuid.MustParse(instance.Id))
	require.NoError(t, err)
	stored := &apiv1alpha1.AgentInstance{}
	require.NoError(t, proto.Unmarshal(row.Data, stored))
	require.True(t, proto.Equal(instance, stored))
	require.Equal(t, request.ProtoReflect().GetUnknown(), stored.ProtoReflect().GetUnknown())
	require.Equal(t, request.Harness.ProtoReflect().GetUnknown(), stored.Harness.ProtoReflect().GetUnknown())
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
	instance, err = client.TransitionAgentInstance(ctx, instance, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED, instance.Operation)
	require.NoError(t, err)

	task := &a2a.Task{ID: "task", ContextID: instance.Id, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
		History: []*a2a.Message{{ID: "message", Role: a2a.MessageRoleUser, Parts: a2a.ContentParts{a2a.NewTextPart("hello")}}},
	}
	_, _, err = client.CreateAgentInstanceTask(ctx, instance.Id, []byte("request hash"), task)
	require.NoError(t, err)
	// Simulate a newer writer using the same binary SQL boundary.
	taskRow, err := q.GetAgentInstanceTask(ctx, dbgen.GetAgentInstanceTaskParams{ContextID: uuid.MustParse(instance.Id), ID: string(task.ID)})
	require.NoError(t, err)
	futureTask := &a2apb.Task{}
	require.NoError(t, proto.Unmarshal(taskRow.Data, futureTask))
	addUnknown(futureTask)
	addUnknown(futureTask.Status)
	futureData, err := proto.Marshal(futureTask)
	require.NoError(t, err)
	require.NoError(t, q.UpsertAgentInstanceTask(ctx, dbgen.UpsertAgentInstanceTaskParams{ContextID: taskRow.ContextID, ID: taskRow.ID, State: taskRow.State, StatusTimestamp: taskRow.StatusTimestamp, Data: futureData}))
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, task, &dbpkg.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot", UID: "snapshot-uid", ContentScope: "DATA"}))
	checkpointRequest := &apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: instance.Id}
	addUnknown(checkpointRequest)
	checkpoint, err := client.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{Checkpoint: checkpointRequest, UserID: "alice", RequestID: "checkpoint"})
	require.NoError(t, err)
	checkpoint, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.Id, "tag-uid", "")
	require.NoError(t, err)
	checkpointRow, err := q.GetAgentInstanceCheckpoint(ctx, dbgen.GetAgentInstanceCheckpointParams{ID: uuid.MustParse(checkpoint.Id), UserID: "alice"})
	require.NoError(t, err)
	storedCheckpoint := &apiv1alpha1.Checkpoint{}
	require.NoError(t, proto.Unmarshal(checkpointRow.Data, storedCheckpoint))
	require.True(t, proto.Equal(checkpoint.Checkpoint, storedCheckpoint))
	require.Equal(t, checkpointRequest.ProtoReflect().GetUnknown(), checkpoint.ProtoReflect().GetUnknown())
	_, err = client.GetAgentInstanceCheckpoint(ctx, checkpoint.Id, "mallory")
	require.ErrorIs(t, err, dbpkg.ErrNotFound)
	fork, created, err := client.ForkAgentInstance(ctx, checkpoint.Id, "alice", "fork", uuid.NewString())
	require.NoError(t, err)
	require.True(t, created)
	tasks, total, err := client.ListAgentInstanceTasks(ctx, fork.Id, "", a2a.TaskStateUnspecified, nil, 10)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, tasks[0].History, 1)
	require.NotEqual(t, task.ID, tasks[0].ID)
	forkTaskRow, err := q.GetAgentInstanceTask(ctx, dbgen.GetAgentInstanceTaskParams{ContextID: uuid.MustParse(fork.Id), ID: string(tasks[0].ID)})
	require.NoError(t, err)
	forkTask := &a2apb.Task{}
	require.NoError(t, proto.Unmarshal(forkTaskRow.Data, forkTask))
	require.Equal(t, futureTask.ProtoReflect().GetUnknown(), forkTask.ProtoReflect().GetUnknown())
	require.Equal(t, futureTask.Status.ProtoReflect().GetUnknown(), forkTask.Status.ProtoReflect().GetUnknown())
	require.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, forkTask.Status.State)
	require.NoError(t, client.DeleteAgentInstance(ctx, fork.Id))
	deleting, err := client.BeginDeleteAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, checkpointRequest.ProtoReflect().GetUnknown(), deleting.ProtoReflect().GetUnknown())
	require.Equal(t, apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING, deleting.State)
	require.NoError(t, client.DeleteAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice"))
	_, err = client.GetAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice")
	require.ErrorIs(t, err, dbpkg.ErrNotFound)
}

func TestShareAndAgentCardProtobufPersistence(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool)
	ctx := t.Context()
	card, err := pbconv.ToProtoAgentCard(&a2a.AgentCard{Name: "assistant", Description: "assistant", Version: "v1", SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface("http://runtime", a2a.TransportProtocolGRPC)}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"}, Skills: []a2a.AgentSkill{{ID: "skill", Name: "skill", Description: "skill", Tags: []string{"tag"}}}, Capabilities: a2a.AgentCapabilities{Streaming: true}})
	require.NoError(t, err)
	addUnknown(card)
	revision := dbpkg.RuntimeRevision{Revision: "revision", Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "template", HarnessName: "kagent", HarnessUID: "harness", SourceSnapshot: []byte(`{}`), AgentCard: card, EgressDestinations: []string{}, ActorTemplateAtespace: "team-a", ActorTemplateName: "template"}
	require.NoError(t, client.UpsertRuntimeRevision(ctx, revision))
	// Reconciliation can update runtime identity, but the pinned card is immutable.
	revision.AgentCard = &a2apb.AgentCard{Name: "replacement"}
	revision.ActorTemplateUID = "new-uid"
	require.NoError(t, client.UpsertRuntimeRevision(ctx, revision))
	stored, err := client.GetRuntimeRevision(ctx, "revision")
	require.NoError(t, err)
	require.True(t, proto.Equal(card, stored.AgentCard))
	require.Equal(t, "new-uid", stored.ActorTemplateUID)
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
		share, err := client.CreateAgentInstanceShare(ctx, dbpkg.AgentInstanceShare{AgentInstanceShare: value, TokenHash: digest[:]})
		require.NoError(t, err)
		require.Equal(t, value.ProtoReflect().GetUnknown(), share.ProtoReflect().GetUnknown())
		resolved, err := client.GetAgentInstanceShareByTokenHash(ctx, digest[:])
		require.NoError(t, err)
		require.True(t, proto.Equal(share.AgentInstanceShare, resolved.AgentInstanceShare))
		require.Equal(t, "alice", resolved.OwnerUserID)
		listed, err := client.ListAgentInstanceShares(ctx, instance.Id, "alice", "", 10)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		require.True(t, proto.Equal(share.AgentInstanceShare, listed[0].AgentInstanceShare))
		listed, err = client.ListAgentInstanceShares(ctx, instance.Id, "mallory", "", 10)
		require.NoError(t, err)
		require.Empty(t, listed)
		require.ErrorIs(t, client.DeleteAgentInstanceShare(ctx, share.Id, "mallory"), dbpkg.ErrNotFound)
		require.NoError(t, client.DeleteAgentInstanceShare(ctx, share.Id, "alice"))
		_, err = client.GetAgentInstanceShareByTokenHash(ctx, digest[:])
		require.ErrorIs(t, err, dbpkg.ErrNotFound)
	}
}

func TestProtobufRowsRejectInconsistentIndexes(t *testing.T) {
	id := uuid.New()
	checkpoint := &apiv1alpha1.Checkpoint{Id: id.String(), AgentInstanceId: id.String(), State: apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY}
	data, err := proto.Marshal(checkpoint)
	require.NoError(t, err)
	_, err = toAgentInstanceCheckpoint(dbgen.AgentInstanceCheckpoint{ID: id, SourceInstanceID: id, State: "CREATING", Data: data})
	require.ErrorContains(t, err, "disagrees with indexed columns")
	share := &apiv1alpha1.AgentInstanceShare{Id: id.String(), AgentInstanceId: id.String(), Permission: apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_WRITE}
	data, err = proto.Marshal(share)
	require.NoError(t, err)
	_, err = toAgentInstanceShare(dbgen.AgentInstanceShare{ID: id, InstanceID: id, Permission: "READ_ONLY", Data: data})
	require.ErrorContains(t, err, "disagrees with indexed columns")
}
