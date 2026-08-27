package agentinstance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/cli/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func TestReadInvokeTask(t *testing.T) {
	tests := []struct {
		name    string
		config  InvokeCfg
		stdin   string
		want    string
		wantErr string
	}{
		{name: "task flag", config: InvokeCfg{Task: "hello"}, want: "hello"},
		{name: "empty task flag", config: InvokeCfg{Task: " \n"}, wantErr: "task is empty"},
		{name: "stdin", config: InvokeCfg{File: "-"}, stdin: "hello from stdin\n", want: "hello from stdin\n"},
		{name: "neither", wantErr: "exactly one"},
		{name: "both", config: InvokeCfg{Task: "hello", File: "-"}, wantErr: "exactly one"},
		{name: "empty stdin", config: InvokeCfg{File: "-"}, stdin: " \n", wantErr: "task is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readInvokeTask(&tt.config, strings.NewReader(tt.stdin))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReadInvokeTaskFile(t *testing.T) {
	path := t.TempDir() + "/task.txt"
	require.NoError(t, os.WriteFile(path, []byte("hello from file"), 0o600))

	got, err := readInvokeTask(&InvokeCfg{File: path}, strings.NewReader(""))
	require.NoError(t, err)
	assert.Equal(t, "hello from file", got)
}

func TestNewInvokeRequestUsesAgentInstanceAsContext(t *testing.T) {
	request := newInvokeRequest("hello", "instance-id")

	require.NotNil(t, request.Message)
	assert.Equal(t, "instance-id", request.Message.ContextID)
	require.Len(t, request.Message.Parts, 1)
	assert.Equal(t, "hello", request.Message.Parts[0].Text())
}

func TestWithModelToken(t *testing.T) {
	ctx := withModelToken(context.Background(), "model-key")
	values, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	assert.Equal(t, []string{"Bearer model-key"}, values.Get("authorization"))
}

func TestWriteSendResultTable(t *testing.T) {
	tests := []struct {
		name   string
		result a2atype.SendMessageResult
		want   string
	}{
		{
			name:   "Message",
			result: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("hello")),
			want:   "hello\n",
		},
		{
			name: "completed Task with multiple artifacts",
			result: &a2atype.Task{
				ID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{
					{ID: "first", Parts: a2atype.ContentParts{a2atype.NewTextPart("hello")}},
					{ID: "second", Parts: a2atype.ContentParts{a2atype.NewTextPart("world")}},
				},
			},
			want: "hello\nworld\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			require.NoError(t, writeSendResult(&output, clioutput.FormatTable, tt.result))
			assert.Equal(t, tt.want, output.String())
		})
	}
}

func TestWriteSendResultJSONPreservesArtifactsAndNonTextParts(t *testing.T) {
	result := &a2atype.Task{
		ID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
		Artifacts: []*a2atype.Artifact{
			{
				ID: "structured", Name: "report",
				Parts: a2atype.ContentParts{
					a2atype.NewTextPart("answer"),
					a2atype.NewDataPart(map[string]any{"value": "structured"}),
				},
			},
			{ID: "binary", Parts: a2atype.ContentParts{a2atype.NewRawPart([]byte{1, 2, 3})}},
		},
	}

	var output bytes.Buffer
	require.NoError(t, writeSendResult(&output, clioutput.FormatJSON, result))
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), `"artifactId":"structured"`)
	assert.Contains(t, output.String(), `"artifactId":"binary"`)
	assert.Contains(t, output.String(), `"data"`)
	assert.Contains(t, output.String(), `"raw"`)
}

func TestWriteTableResultContinuation(t *testing.T) {
	tests := []struct {
		name  string
		state a2atype.TaskState
		want  string
	}{
		{name: "input required", state: a2atype.TaskStateInputRequired, want: "Need a value\nInput required to continue this AgentInstance.\n"},
		{name: "auth required", state: a2atype.TaskStateAuthRequired, want: "Sign in\nAuthentication required to continue this AgentInstance.\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &a2atype.Task{
				ID: "task-1", ContextID: "instance-1",
				Status: a2atype.TaskStatus{
					State: tt.state,
					Message: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(map[a2atype.TaskState]string{
						a2atype.TaskStateInputRequired: "Need a value",
						a2atype.TaskStateAuthRequired:  "Sign in",
					}[tt.state])),
				},
			}
			var output bytes.Buffer
			require.NoError(t, writeTableResult(&output, result))
			assert.Equal(t, tt.want, output.String())
			require.NoError(t, sendResultError(result))
		})
	}
}

func TestWriteJSONKeepsPausedStateExplicit(t *testing.T) {
	result := &a2atype.Task{
		ID: "task-1", ContextID: "instance-1",
		Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired},
	}
	var output bytes.Buffer

	require.NoError(t, writeSendResult(&output, clioutput.FormatJSON, result))
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), `"state":"TASK_STATE_INPUT_REQUIRED"`)
}

func TestSendResultError(t *testing.T) {
	tests := []struct {
		state   a2atype.TaskState
		wantErr bool
	}{
		{state: a2atype.TaskStateCompleted},
		{state: a2atype.TaskStateInputRequired},
		{state: a2atype.TaskStateAuthRequired},
		{state: a2atype.TaskStateFailed, wantErr: true},
		{state: a2atype.TaskStateRejected, wantErr: true},
		{state: a2atype.TaskStateCanceled, wantErr: true},
		{state: a2atype.TaskStateWorking, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			err := sendResultError(&a2atype.Task{
				ID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: tt.state},
			})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestConsumeA2AStreamReturnsPartialResultOnTruncation(t *testing.T) {
	result, err := consumeA2AStream(eventStream(
		streamItem{event: &a2atype.Task{
			ID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking},
		}},
		streamItem{event: &a2atype.TaskArtifactUpdateEvent{
			TaskID: "task-1", ContextID: "instance-1",
			Artifact: &a2atype.Artifact{ID: "answer", Parts: a2atype.ContentParts{a2atype.NewTextPart("partial")}},
		}},
	), func(a2atype.Event, a2atype.SendMessageResult) error { return nil })

	require.ErrorIs(t, err, errTruncatedA2AStream)
	assert.Equal(t, "partial", sendResultText(result))
}

func TestConsumeA2AStreamWritesJSONL(t *testing.T) {
	events := []streamItem{
		{event: &a2atype.Task{
			ID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking},
		}},
		{event: &a2atype.TaskArtifactUpdateEvent{
			TaskID: "task-1", ContextID: "instance-1",
			Artifact: &a2atype.Artifact{ID: "answer", Parts: a2atype.ContentParts{a2atype.NewTextPart("done")}},
		}},
		{event: &a2atype.TaskStatusUpdateEvent{
			TaskID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
		}},
	}
	var output bytes.Buffer

	result, err := consumeA2AStream(eventStream(events...), func(event a2atype.Event, _ a2atype.SendMessageResult) error {
		return writeStreamEvent(&output, event)
	})
	require.NoError(t, err)
	assert.Equal(t, "done", sendResultText(result))
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, len(events))
	for _, line := range lines {
		assert.True(t, json.Valid([]byte(line)), "invalid JSONL line: %s", line)
	}
}

func TestConsumeA2AStreamWritesTableBeforeCompletion(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stream := func(yield func(a2atype.Event, error) bool) {
		if !yield(&a2atype.Task{
			ID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking},
		}, nil) {
			return
		}
		if !yield(&a2atype.TaskArtifactUpdateEvent{
			TaskID: "task-1", ContextID: "instance-1",
			Artifact: &a2atype.Artifact{ID: "answer", Parts: a2atype.ContentParts{a2atype.NewTextPart("Hel")}},
		}, nil) {
			return
		}
		<-release
		if !yield(&a2atype.TaskArtifactUpdateEvent{
			TaskID: "task-1", ContextID: "instance-1", LastChunk: true,
			Artifact: &a2atype.Artifact{ID: "answer", Parts: a2atype.ContentParts{a2atype.NewTextPart("Hello")}},
		}, nil) {
			return
		}
		yield(&a2atype.TaskStatusUpdateEvent{
			TaskID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
		}, nil)
	}
	out := &signalWriter{wrote: make(chan struct{}, 1)}
	done := make(chan error, 1)

	go func() {
		writer := tableStreamWriter{w: out}
		result, err := consumeA2AStream(stream, writer.Write)
		if finishErr := writer.Finish(result); err == nil {
			err = finishErr
		}
		done <- err
	}()

	select {
	case <-out.wrote:
		assert.Equal(t, "Hel", out.String())
	case err := <-done:
		t.Fatalf("table stream returned before completion: %v", err)
	case <-time.After(time.Second):
		t.Fatalf("table stream did not write the partial response before completion: %q", out.String())
	}
	release <- struct{}{}
	require.NoError(t, <-done)
	assert.Equal(t, "Hello\n", out.String())
}

func TestConsumeA2AStreamPreservesTerminalError(t *testing.T) {
	wantErr := errors.New("stream disconnected")
	result, err := consumeA2AStream(eventStream(
		streamItem{event: &a2atype.Task{
			ID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking},
		}},
		streamItem{err: wantErr},
	), func(a2atype.Event, a2atype.SendMessageResult) error { return nil })

	require.ErrorIs(t, err, wantErr)
	require.NotNil(t, result)
}

func TestFinishInvokeStreamPreservesStreamAndWriteErrors(t *testing.T) {
	streamErr := errors.New("stream disconnected")
	writeErr := errors.New("broken pipe")
	result := &a2atype.Task{
		ID: "task-1", ContextID: "instance-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking},
	}

	err := finishInvokeStream(&tableStreamWriter{w: failingWriter{err: writeErr}, text: "partial"}, result, streamErr)

	require.ErrorIs(t, err, streamErr)
	require.ErrorIs(t, err, writeErr)
}

type streamItem struct {
	event a2atype.Event
	err   error
}

func eventStream(items ...streamItem) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		for _, item := range items {
			if !yield(item.event, item.err) {
				return
			}
		}
	}
}

type signalWriter struct {
	bytes.Buffer
	wrote chan struct{}
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w *signalWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	w.signal()
	return n, err
}

func (w *signalWriter) WriteString(s string) (int, error) {
	n, err := w.Buffer.WriteString(s)
	w.signal()
	return n, err
}

func (w *signalWriter) signal() {
	select {
	case w.wrote <- struct{}{}:
	default:
	}
}
