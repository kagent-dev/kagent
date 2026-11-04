package a2a

import (
	"encoding/json"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

func TestTaskReply(t *testing.T) {
	agentMessage := func(text string) *a2atype.Message {
		return a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(text))
	}
	artifact := func(parts ...*a2atype.Part) *a2atype.Artifact {
		return &a2atype.Artifact{ID: a2atype.NewArtifactID(), Parts: parts}
	}
	for _, test := range []struct {
		name string
		task *a2atype.Task
		want string
	}{
		{
			name: "status message",
			task: &a2atype.Task{Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Message: agentMessage("The answer is 4.")}},
			want: "The answer is 4.",
		},
		{
			name: "artifacts joined in order",
			task: &a2atype.Task{
				Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{artifact(a2atype.NewTextPart("Let me check.")), artifact(a2atype.NewTextPart("The answer is 4."))},
			},
			want: "Let me check.\nThe answer is 4.",
		},
		{
			name: "streamed text parts concatenated within an artifact",
			task: &a2atype.Task{
				Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{
					artifact(a2atype.NewTextPart("hel"), a2atype.NewTextPart("lo")),
					artifact(a2atype.NewTextPart("wor"), a2atype.NewTextPart("ld")),
				},
			},
			want: "hello\nworld",
		},
		{
			name: "structured result apart from the text around it",
			task: &a2atype.Task{
				Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{artifact(a2atype.NewTextPart("Here:"), apia2a.NewStructuredOutputPart(map[string]any{"answer": 4}, "digest"))},
			},
			want: "Here:\n{\"answer\":4}",
		},
		{
			name: "failed task keeps its error and partial output",
			task: &a2atype.Task{
				Status:    a2atype.TaskStatus{State: a2atype.TaskStateFailed, Message: agentMessage("model unavailable")},
				Artifacts: []*a2atype.Artifact{artifact(a2atype.NewTextPart("Let me check."))},
			},
			want: "model unavailable\nLet me check.",
		},
		{
			name: "tool activity is not the reply",
			task: &a2atype.Task{
				Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{
					artifact(apia2a.NewToolCallPart("call-1", "read_file", map[string]any{"path": "/etc/hosts"})),
					artifact(apia2a.NewToolResultPart("call-1", "read_file", "two lines")),
					artifact(a2atype.NewTextPart("The file has two lines.")),
				},
			},
			want: "The file has two lines.",
		},
		{
			name: "structured result as JSON",
			task: &a2atype.Task{
				Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{artifact(apia2a.NewStructuredOutputPart(map[string]any{"answer": 4}, "digest"))},
			},
			want: `{"answer":4}`,
		},
		{
			name: "latest agent message of the history",
			task: &a2atype.Task{
				Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				History: []*a2atype.Message{
					a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 2+2?")),
					agentMessage("Let me think."),
					agentMessage("The answer is 4."),
				},
			},
			want: "The answer is 4.",
		},
		{
			name: "no reply yet",
			task: &a2atype.Task{Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}},
			want: "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, TaskReply(test.task))
			if test.want != "" {
				require.Equal(t, test.want, TaskText(test.task), "invoke_agent answers the same reply")
			}
		})
	}
}

func TestTaskTextAnswersATaskWithoutReplyAsJSON(t *testing.T) {
	task := &a2atype.Task{ID: "task-0", ContextID: "context-0", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	var decoded a2atype.Task
	require.NoError(t, json.Unmarshal([]byte(TaskText(task)), &decoded))
	require.Equal(t, task.ID, decoded.ID)
	require.Equal(t, a2atype.TaskStateWorking, decoded.Status.State)
}
