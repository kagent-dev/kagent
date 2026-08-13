package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	"github.com/stretchr/testify/require"
)

func TestChatModelDisplaysStreamError(t *testing.T) {
	send := func(context.Context, *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
		ch := make(chan clia2a.StreamResult)
		close(ch)
		return ch
	}
	model := newChatModel("default/agent", "session-1", send, false)
	model.streaming = true
	model.working = true

	updated, cmd := model.Update(clia2a.StreamResult{Err: errors.New("stream disconnected")})
	got := updated.(*chatModel)

	require.Nil(t, cmd)
	require.False(t, got.streaming)
	require.False(t, got.working)
	require.True(t, strings.Contains(got.history, "Error: stream disconnected"))
}

func TestChatModelBuffersArtifactDeltasUntilTerminalStatus(t *testing.T) {
	model := newTestChatModel()
	reqCtx := &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "ctx-1"}
	first := a2atype.NewArtifactEvent(reqCtx, a2atype.NewTextPart("hel"))
	second := a2atype.NewArtifactUpdateEvent(reqCtx, first.Artifact.ID, a2atype.NewTextPart("lo"))

	model.appendEvent(first)
	model.appendEvent(second)
	require.NotContains(t, model.history, "hello", "open artifacts should not be displayed before completion")

	model.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil))
	require.Contains(t, model.history, "hello")
	require.Empty(t, model.artifacts)
	require.Empty(t, model.artifactOrder)
}

func TestChatModelArtifactReplacementAndContentBearingLastChunk(t *testing.T) {
	model := newTestChatModel()
	reqCtx := &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "ctx-1"}
	partial := a2atype.NewArtifactEvent(reqCtx, a2atype.NewTextPart("hel"))
	final := a2atype.NewArtifactUpdateEvent(reqCtx, partial.Artifact.ID, a2atype.NewTextPart("hello"))
	final.Append = false
	final.LastChunk = true

	model.appendEvent(partial)
	model.appendEvent(final)

	require.Contains(t, model.history, "hello")
	require.NotContains(t, model.history, "helhello", "append=false must replace buffered partial text")
	require.Empty(t, model.artifacts)

	// A later terminal status must not display an already-closed artifact again.
	before := model.history
	model.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil))
	require.Equal(t, before, model.history)
}

func TestChatModelProcessesToolPartsBeforeLastChunk(t *testing.T) {
	model := newTestChatModel()
	reqCtx := &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "ctx-1"}
	callPart := a2atype.NewDataPart(map[string]any{
		"name": "get_pods",
		"id":   "call-1",
		"args": map[string]any{"namespace": "default"},
	})
	callPart.Metadata = map[string]any{"adk_type": "function_call"}
	callUpdate := a2atype.NewArtifactEvent(reqCtx, callPart)
	resultPart := a2atype.NewDataPart(map[string]any{
		"name":     "get_pods",
		"id":       "call-1",
		"response": map[string]any{"pods": []any{"pod-a"}},
	})
	resultPart.Metadata = map[string]any{"adk_type": "function_response"}
	resultUpdate := a2atype.NewArtifactEvent(reqCtx, resultPart)

	model.appendEvent(callUpdate)
	model.appendEvent(resultUpdate)

	require.Contains(t, model.history, "Tool Call: get_pods")
	require.Contains(t, model.history, "Tool Result: get_pods")
	require.Contains(t, model.history, "call-1")
	require.Contains(t, model.history, "pod-a")
}

func TestChatModelPreservesFailedStatusExplanation(t *testing.T) {
	model := newTestChatModel()
	reqCtx := &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "ctx-1"}
	message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("execution failed"))

	model.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateFailed, message))

	require.Contains(t, model.history, "execution failed")
}

func TestChatModelReadsTaskSnapshotOutputFromArtifactsOnly(t *testing.T) {
	model := newTestChatModel()
	artifact := &a2atype.Artifact{
		ID:    "artifact-1",
		Parts: a2atype.ContentParts{a2atype.NewTextPart("artifact result")},
	}
	task := &a2atype.Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
		Artifacts: []*a2atype.Artifact{artifact},
	}

	model.appendEvent(task)

	require.Contains(t, model.history, "artifact result")
}

func newTestChatModel() *chatModel {
	send := func(context.Context, *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
		ch := make(chan clia2a.StreamResult)
		close(ch)
		return ch
	}
	return newChatModel("default/agent", "session-1", send, false)
}
