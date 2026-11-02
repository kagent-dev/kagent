package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// historyTask is one past turn: the user's prompt, and the reply's artifacts.
func historyTask(i int, artifacts ...*a2atype.Artifact) *a2atype.Task {
	return &a2atype.Task{
		ID: a2atype.TaskID(fmt.Sprintf("task-%d", i)), ContextID: "ctx",
		Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
		History:   []*a2atype.Message{a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(fmt.Sprintf("prompt %d", i)))},
		Artifacts: artifacts,
	}
}

func artifactOf(parts ...*a2atype.Part) *a2atype.Artifact {
	return &a2atype.Artifact{ID: a2atype.NewArtifactID(), Parts: parts}
}

// History pages through every task, asking for each one whole.
func TestAllTasksPagesThroughEverything(t *testing.T) {
	var asked []*a2atype.ListTasksRequest
	list := func(_ context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
		copied := *req
		asked = append(asked, &copied)
		if req.PageToken == "" {
			return &a2atype.ListTasksResponse{Tasks: []*a2atype.Task{historyTask(1)}, NextPageToken: "two"}, nil
		}
		return &a2atype.ListTasksResponse{Tasks: []*a2atype.Task{historyTask(2)}}, nil
	}
	tasks, err := allTasks(t.Context(), list, "ctx")
	require.NoError(t, err)
	assert.Len(t, tasks, 2)
	require.Len(t, asked, 2)
	assert.Nil(t, asked[0].HistoryLength, "no cap that drops a task's first messages")
	assert.True(t, asked[0].IncludeArtifacts)
	assert.Equal(t, 100, asked[0].PageSize)
	assert.Equal(t, "two", asked[1].PageToken)
}

// A resumed session shows every turn, tool calls, results and thoughts
// included, closes each turn as it did live, and opens at the latest.
func TestResumedHistoryShowsEverything(t *testing.T) {
	m := openWorkspace(t)

	var tasks []*a2atype.Task
	for i := 1; i <= 25; i++ {
		tasks = append(tasks, historyTask(i, artifactOf(a2atype.NewTextPart(fmt.Sprintf("reply %d", i)))))
	}
	tasks = append(tasks, historyTask(26,
		artifactOf(thoughtPart("weighing the file"), dataPart("function_call", "read_file", map[string]any{"args": map[string]any{"file_path": "notes.md"}})),
		artifactOf(dataPart("function_response", "read_file", map[string]any{"response": map[string]any{"result": "     1|ok"}})),
		artifactOf(a2atype.NewTextPart("The file says ok.")),
	))
	failed := historyTask(27)
	failed.Status = a2atype.TaskStatus{State: a2atype.TaskStateFailed, Message: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("quota exceeded"))}
	tasks = append(tasks, failed)
	m.Update(sessionHistoryLoadedMsg{sessionID: m.current.GetId(), tasks: tasks})

	got := ansi.Strip(transcript(m.chat))
	for _, want := range []string{"prompt 1", "reply 1", "prompt 25", "reply 25", "prompt 26", "✻ Thought", "● read_file  notes.md", "│ 1  ok", "The file says ok."} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, "weighing the file", "a replayed thought is collapsed")
	assert.Less(t, strings.Index(got, "✻ Thought"), strings.Index(got, "notes.md"), "parts keep their order")
	assert.Less(t, strings.Index(got, "notes.md"), strings.Index(got, "The file says ok."))
	assert.Contains(t, got, "✓ Done · 1 tool", "a replayed turn has no recorded time")
	assert.Equal(t, 26, strings.Count(got, "✓ Done"))
	assert.Contains(t, got, "✗ Failed · quota exceeded")
	assert.True(t, m.chat.vp.AtBottom(), "it opens at the latest turn")
	assert.Contains(t, ansi.Strip(m.View()), "✗ Failed · quota exceeded")
}
