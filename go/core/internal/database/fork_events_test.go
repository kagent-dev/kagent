package database

import (
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestForkEventsRewritePublicReferencesOnly(t *testing.T) {
	metadata, err := structpb.NewStruct(map[string]any{"remote_task_id": "source-task"})
	require.NoError(t, err)
	message := &a2apb.Message{MessageId: "message", TaskId: "source-task", ContextId: "source-context",
		ReferenceTaskIds: []string{"source-task", "remote-task"}, Metadata: metadata}
	source := &a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Task{Task: &a2apb.Task{
		Id: "source-task", ContextId: "source-context", History: []*a2apb.Message{message},
		Status:    &a2apb.TaskStatus{Message: proto.CloneOf(message)},
		Artifacts: []*a2apb.Artifact{{ArtifactId: "artifact"}},
	}}}
	addUnknown(source)
	data, err := proto.Marshal(source)
	require.NoError(t, err)
	contextID := uuid.NewString()
	rows, err := forkTaskEvents([]agentInstanceTaskEventRow{{TaskID: "source-task", Data: data}}, contextID)
	require.NoError(t, err)
	rewritten := &a2apb.StreamResponse{}
	require.NoError(t, proto.Unmarshal(rows[0].Data, rewritten))
	task := rewritten.GetTask()
	require.NotEqual(t, "source-task", task.Id)
	require.Equal(t, contextID, task.ContextId)
	require.Equal(t, rows[0].TaskID, task.Id)
	for _, copied := range []*a2apb.Message{task.History[0], task.Status.Message} {
		require.Equal(t, task.Id, copied.TaskId)
		require.Equal(t, contextID, copied.ContextId)
		require.Equal(t, []string{task.Id, "remote-task"}, copied.ReferenceTaskIds)
		require.Equal(t, "message", copied.MessageId)
		require.Equal(t, "source-task", copied.Metadata.Fields["remote_task_id"].GetStringValue())
	}
	require.Equal(t, "artifact", task.Artifacts[0].ArtifactId)
	require.Equal(t, source.ProtoReflect().GetUnknown(), rewritten.ProtoReflect().GetUnknown())
	require.Equal(t, "source-task", source.GetTask().Id)
	require.Equal(t, "source-context", source.GetTask().History[0].ContextId)
}
