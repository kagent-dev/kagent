package tui

import (
	"context"
	"errors"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	"github.com/stretchr/testify/assert"
)

func newTestChatModel() *chatModel {
	send := func(context.Context, *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
		ch := make(chan clia2a.StreamResult)
		close(ch)
		return ch
	}
	return newChatModel(context.Background(), "reporter", "ctx-1", send, false)
}

// transcript is everything the viewport shows: committed blocks plus the one being assembled.
func transcript(m *chatModel) string {
	if m.agentText == "" {
		return m.history
	}
	return m.history + "\n\n" + m.agentText
}

func reqCtx() *a2asrv.ExecutorContext {
	return &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "ctx-1"}
}

func dataPart(kind, name string, payload map[string]any) *a2atype.Part {
	payload["name"] = name
	payload["id"] = "call-1"
	part := a2atype.NewDataPart(payload)
	part.Metadata = map[string]any{"adk_type": kind}
	return part
}

// The projection is cumulative, so a delta must not repeat and a replacement must not concatenate.
func TestChatModelStreamsAssembledText(t *testing.T) {
	tests := []struct {
		name       string
		events     func() []a2atype.Event
		want       string
		wantAbsent string
	}{
		{
			name: "a delta extends in place",
			events: func() []a2atype.Event {
				first := a2atype.NewArtifactEvent(reqCtx(), a2atype.NewTextPart("hel"))
				return []a2atype.Event{first, a2atype.NewArtifactUpdateEvent(reqCtx(), first.Artifact.ID, a2atype.NewTextPart("lo"))}
			},
			want: "hello", wantAbsent: "helhello",
		},
		{
			name: "a replacement chunk replaces",
			events: func() []a2atype.Event {
				partial := a2atype.NewArtifactEvent(reqCtx(), a2atype.NewTextPart("hel"))
				final := a2atype.NewArtifactUpdateEvent(reqCtx(), partial.Artifact.ID, a2atype.NewTextPart("hello"))
				final.Append, final.LastChunk = false, true
				return []a2atype.Event{partial, final}
			},
			want: "hello", wantAbsent: "helhello",
		},
		{
			// Only artifacts carry output; status text is control-plane content.
			name: "a task snapshot reads artifacts, not status text",
			events: func() []a2atype.Event {
				return []a2atype.Event{&a2atype.Task{
					ID: "task-1", ContextID: "ctx-1",
					Status: a2atype.TaskStatus{
						State:   a2atype.TaskStateCompleted,
						Message: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("control plane note")),
					},
					Artifacts: []*a2atype.Artifact{{
						ID: "artifact-1", Parts: a2atype.ContentParts{a2atype.NewTextPart("artifact result")},
					}},
				}}
			},
			want: "artifact result", wantAbsent: "control plane note",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newTestChatModel()
			for _, event := range tt.events() {
				model.appendEvent(event)
			}

			assert.Contains(t, transcript(model), tt.want)
			assert.NotContains(t, transcript(model), tt.wantAbsent)
		})
	}
}

// Paused, terminal-failure, and transport problems must read as three different things.
func TestChatModelRendersStateClasses(t *testing.T) {
	tests := []struct {
		name       string
		apply      func(*chatModel)
		want       string
		wantAbsent string
	}{
		{
			name: "input required is paused",
			apply: func(m *chatModel) {
				m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateInputRequired, nil))
			},
			want: "Input required", wantAbsent: "✗",
		},
		{
			name: "auth required is paused",
			apply: func(m *chatModel) {
				m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateAuthRequired, nil))
			},
			want: "Authentication required", wantAbsent: "✗",
		},
		{
			name: "failed is an error and keeps its explanation",
			apply: func(m *chatModel) {
				message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("execution failed"))
				m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateFailed, message))
			},
			want: "execution failed",
		},
		{
			name: "completed needs no banner",
			apply: func(m *chatModel) {
				m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateCompleted, nil))
			},
			wantAbsent: "✗",
		},
		{
			name: "a transport failure is not a task failure",
			apply: func(m *chatModel) {
				m.Update(clia2a.StreamResult{Err: errors.New("stream disconnected")})
			},
			want: "Connection error: stream disconnected", wantAbsent: "✗ Task",
		},
		{
			// A malformed stream is neither a task nor a transport failure, and must not be dropped.
			name: "a malformed stream is a protocol error",
			apply: func(m *chatModel) {
				m.appendEvent(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("first")))
				m.appendEvent(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("second")))
			},
			want: "Protocol error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newTestChatModel()
			tt.apply(model)

			if tt.want != "" {
				assert.Contains(t, transcript(model), tt.want)
			}
			if tt.wantAbsent != "" {
				assert.NotContains(t, transcript(model), tt.wantAbsent)
			}
			assert.False(t, model.working, "a settled task stops the working indicator")
		})
	}
}

func TestChatModelRendersToolActivityBeforeLastChunk(t *testing.T) {
	model := newTestChatModel()

	model.appendEvent(a2atype.NewArtifactEvent(reqCtx(),
		dataPart("function_call", "get_pods", map[string]any{"args": map[string]any{"namespace": "default"}})))
	model.appendEvent(a2atype.NewArtifactEvent(reqCtx(),
		dataPart("function_response", "get_pods", map[string]any{"response": map[string]any{"pods": []any{"pod-a"}}})))

	got := transcript(model)
	assert.Contains(t, got, "Tool Call: get_pods")
	assert.Contains(t, got, "Tool Result: get_pods")
	assert.Contains(t, got, "pod-a")
}

func TestChatModelAppendsHistoryTask(t *testing.T) {
	model := newTestChatModel()

	model.AppendHistoryTask(&a2atype.Task{
		ID: "task-1", ContextID: "ctx-1",
		Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
		History: []*a2atype.Message{
			a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("what is 2+2?")),
			a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("echoed agent turn")),
		},
		Artifacts: []*a2atype.Artifact{{
			ID: "artifact-1", Parts: a2atype.ContentParts{a2atype.NewTextPart("The answer is 4.")},
		}},
	})

	got := transcript(model)
	assert.Contains(t, got, "what is 2+2?")
	assert.Contains(t, got, "The answer is 4.")
	assert.NotContains(t, got, "echoed agent turn", "agent output comes from artifacts, not history messages")
}
