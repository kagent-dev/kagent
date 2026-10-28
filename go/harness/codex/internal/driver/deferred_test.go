package driver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type fakeToolCaller struct {
	call func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error)
}

func (f fakeToolCaller) Call(ctx context.Context, server, tool string, args map[string]any) (*mcp.CallToolResult, error) {
	return f.call(ctx, server, tool, args)
}

func waitingRecord() pendingRecord {
	return pendingRecord{Version: 1, TaskID: "task-1", ContextID: "context-1", ThreadID: "thread-1", Phase: phaseWaiting,
		Server: "protected", Tool: "write", Approval: &runtime.ApprovalRequest{ID: "approval-1", CallID: "call-1", Name: "protected.write", Args: map[string]any{"value": float64(7)}}}
}

func TestPendingTraceOriginSurvivesRestoreAndRepark(t *testing.T) {
	r := waitingRecord()
	r.Origin = runtime.NewTraceOrigin(trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled}))
	d := NewProcessDriver(ProcessConfig{PendingDir: t.TempDir(), MaxFrameBytes: 4096})
	d.config.Tools = fakeToolCaller{call: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
		return nil, &toolNotExecutedError{err: errors.New("connection unavailable")}
	}}
	require.NoError(t, d.savePending(&r))
	_, _, pending, err := d.RestorePending()
	require.NoError(t, err)
	_, err = pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: true}, &recordingSink{})
	require.NoError(t, err)
	restored := NewProcessDriver(ProcessConfig{PendingDir: d.config.PendingDir, MaxFrameBytes: 4096})
	_, _, pending, err = restored.RestorePending()
	require.NoError(t, err)
	got := pending.(*deferredTurn).TraceOrigin().SpanContext()
	require.Equal(t, r.Origin.SpanContext().TraceID(), got.TraceID())
	require.Equal(t, r.Origin.SpanContext().SpanID(), got.SpanID())
	require.Equal(t, r.Origin.SpanContext().TraceFlags(), got.TraceFlags())
}

func TestRestorePendingValidatesDurableData(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
	}{
		{"invalid json", "{"}, {"unsupported version", `{"version":99}`},
		{"unknown field", `{"unexpected":true}`}, {"missing identity", `{"version":1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "codex-pending.json"), []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			driver := NewProcessDriver(ProcessConfig{PendingDir: directory, MaxFrameBytes: 4096})
			if _, _, _, err := driver.RestorePending(); err == nil {
				t.Fatal("corrupt durable state accepted")
			}
		})
	}
}

func TestDeferredTurnRejectsUncertainAndChangedResponses(t *testing.T) {
	for _, phase := range []pendingPhase{phaseWaiting, phaseExecuting, phaseResult, phaseContinuing, phaseComplete, phaseCanceled, phaseAbandoned} {
		t.Run(string(phase), func(t *testing.T) {
			r := waitingRecord()
			r.Phase = phase
			decision := &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: true}
			if phase != phaseWaiting && phase != phaseCanceled {
				r.Decision = decision
			}
			if phase == phaseResult || phase == phaseContinuing || phase == phaseComplete {
				r.Result = &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "already executed"}}}
			}
			calls := 0
			driver := NewProcessDriver(ProcessConfig{PendingDir: t.TempDir(), MaxFrameBytes: 4096, Tools: fakeToolCaller{call: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
				calls++
				return nil, errors.New("unexpected replay")
			}}})
			if err := driver.savePending(&r); err != nil {
				t.Fatal(err)
			}
			if phase == phaseExecuting || phase == phaseContinuing || phase == phaseAbandoned {
				taskID, contextID, restored, err := driver.RestorePending()
				require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
				require.Empty(t, taskID)
				require.Empty(t, contextID)
				require.Nil(t, restored)
			}
			pending := &deferredTurn{driver: driver, record: r}
			response := decision
			if phase == phaseWaiting {
				response = &runtime.ApprovalDecision{ID: "wrong", Approved: true}
			}
			if phase == phaseResult {
				response = &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: false}
			}
			if _, err := pending.Resume(t.Context(), response, &recordingSink{}); err == nil || calls != 0 {
				t.Fatalf("unsafe replay: calls=%d err=%v", calls, err)
			}
			_, _, restored, err := driver.RestorePending()
			if phase == phaseComplete || phase == phaseCanceled {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			}
			require.Nil(t, restored, "terminal failure must never restore a pending task")
		})
	}
}

func TestDeferredTurnPersistsExecutingBeforeToolAndDoesNotRetry(t *testing.T) {
	r := waitingRecord()
	calls := 0
	driver := NewProcessDriver(ProcessConfig{PendingDir: t.TempDir(), MaxFrameBytes: 4096})
	driver.config.Tools = fakeToolCaller{call: func(_ context.Context, server, tool string, args map[string]any) (*mcp.CallToolResult, error) {
		calls++
		stored, err := driver.loadPending()
		if err != nil || stored.Phase != phaseExecuting || stored.Decision == nil || !stored.Decision.Approved {
			t.Fatalf("operation not journaled before execution: %#v %v", stored, err)
		}
		if server != "protected" || tool != "write" || args["value"] != float64(7) {
			t.Fatal("saved operation changed")
		}
		return nil, errors.New("connection lost after possible execution")
	}}
	if err := driver.savePending(&r); err != nil {
		t.Fatal(err)
	}
	pending := &deferredTurn{driver: driver, record: r}
	decision := &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: true}
	_, err := pending.Resume(t.Context(), decision, &recordingSink{})
	require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
	saved, err := driver.loadPending()
	require.NoError(t, err)
	require.Equal(t, phaseAbandoned, saved.Phase)
	restored := NewProcessDriver(ProcessConfig{PendingDir: driver.config.PendingDir, MaxFrameBytes: 4096})
	taskID, contextID, pendingTurn, err := restored.RestorePending()
	require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
	require.Empty(t, taskID)
	require.Empty(t, contextID)
	require.Nil(t, pendingTurn)
	require.ErrorIs(t, pending.Cancel(t.Context(), &recordingSink{}), runtime.ErrSessionAbandoned)
	require.Equal(t, 1, calls)
}

var _ ToolCaller = fakeToolCaller{}

func TestDeferredMCPUsesCurrentCredentialsAndAllowedTools(t *testing.T) {
	var calls atomic.Int32
	server := mcp.NewServer(&mcp.Implementation{Name: "credential-test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "write"}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "restored-secret" {
			http.Error(w, "bad credentials", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(endpoint.Close)
	servers := map[string]config.MCPServer{"protected": {URL: endpoint.URL, RequireApproval: true, EnabledTools: []string{"write"}, Headers: map[string]string{"Authorization": "${TOOL_TOKEN}"}}}
	caller := NewMCPCaller(servers, []string{"TOOL_TOKEN=restored-secret"})
	for _, tool := range []string{"not-allowed", "write"} {
		result, err := caller.Call(t.Context(), "protected", tool, map[string]any{})
		if tool == "not-allowed" {
			if err == nil || calls.Load() != 0 {
				t.Fatalf("unconfigured tool executed: %v calls=%d", err, calls.Load())
			}
		} else if err != nil || result == nil || calls.Load() != 1 {
			t.Fatalf("approved call failed: %v result=%#v calls=%d", err, result, calls.Load())
		}
	}
	missing := NewMCPCaller(servers, nil)
	if _, err := missing.Call(t.Context(), "protected", "write", nil); err == nil || !strings.Contains(err.Error(), "unavailable") || calls.Load() != 1 {
		t.Fatalf("missing credential was not rejected before execution: %v", err)
	}
	servers["protected"] = config.MCPServer{URL: endpoint.URL}
	if _, err := caller.Call(t.Context(), "protected", "write", nil); err == nil || calls.Load() != 1 {
		t.Fatalf("removed approval boundary was not rejected: %v", err)
	}
}

func TestDeferredTurnReparksKnownMCPNonExecution(t *testing.T) {
	r := waitingRecord()
	d := NewProcessDriver(ProcessConfig{PendingDir: t.TempDir(), MaxFrameBytes: 4096,
		Tools: NewMCPCaller(map[string]config.MCPServer{"protected": {
			URL: "http://127.0.0.1:1", RequireApproval: true, Headers: map[string]string{"Authorization": "${MISSING_TOKEN}"},
		}}, nil)})
	require.NoError(t, d.savePending(&r))
	for range 2 {
		_, _, pending, err := d.RestorePending()
		require.NoError(t, err)
		sink := &recordingSink{}
		outcome, err := pending.Resume(t.Context(), &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: true}, sink)
		require.NoError(t, err)
		require.NotNil(t, outcome.Pending)
		require.Equal(t, pending.Request(), outcome.Pending.Request())
		require.Empty(t, sink.results)
		saved, err := d.loadPending()
		require.NoError(t, err)
		require.Equal(t, r, *saved)
	}
}

func TestDeferredTurnCancellationDuringMCPExecutionReleasesReservation(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		name := "tombstone"
		if corrupt {
			name = "cleanup failure"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			r := waitingRecord()
			directory := t.TempDir()
			calls := 0
			d := NewProcessDriver(ProcessConfig{PendingDir: directory, MaxFrameBytes: 4096, Tools: fakeToolCaller{call: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
				calls++
				if corrupt {
					require.NoError(t, os.WriteFile(filepath.Join(directory, "codex-pending.json"), []byte("corrupt"), 0o600))
				}
				cancel()
				return nil, context.Canceled
			}}})
			require.NoError(t, d.savePending(&r))
			_, _, pending, err := d.RestorePending()
			require.NoError(t, err)
			outcome, err := pending.Resume(ctx, &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: true}, &recordingSink{})
			require.Nil(t, outcome.Pending)
			require.Equal(t, 1, calls)
			if corrupt {
				var cleanupErr *runtime.CancellationError
				require.ErrorAs(t, err, &cleanupErr)
			} else {
				require.ErrorIs(t, err, context.Canceled)
				saved, err := d.loadPending()
				require.NoError(t, err)
				require.Equal(t, phaseCanceled, saved.Phase)
				_, _, recovered, err := d.RestorePending()
				require.NoError(t, err)
				require.Nil(t, recovered)
			}
		})
	}
}

func TestDeferredMCPKeepsReceivedResultWhenSessionCloseFails(t *testing.T) {
	var calls, deletes atomic.Int32
	server := mcp.NewServer(&mcp.Implementation{Name: "close-failure", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "write"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "committed"}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			if err := conn.Close(); err != nil {
				t.Error(err)
			}
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(endpoint.Close)
	caller := NewMCPCaller(map[string]config.MCPServer{"protected": {URL: endpoint.URL, RequireApproval: true}}, nil)
	result, err := caller.Call(t.Context(), "protected", "write", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "committed", result.Content[0].(*mcp.TextContent).Text)
	require.EqualValues(t, 1, calls.Load())
	require.Positive(t, deletes.Load(), "fixture did not exercise session termination")
}

func TestDeferredTurnAbandonsChangedCachedAnswerAfterStartupFailure(t *testing.T) {
	for _, question := range []bool{false, true} {
		name := "approval"
		if question {
			name = "question"
		}
		t.Run(name, func(t *testing.T) {
			r := waitingRecord()
			r.Phase = phaseResult
			var response runtime.InputResponse
			if question {
				r.Server, r.Tool, r.Approval = "", "", nil
				r.Question = &runtime.AskUserRequest{ID: "question", Questions: []runtime.AskUserQuestion{{ID: "database", Question: "Which database?"}}}
				r.Answer = &runtime.AskUserResponse{ID: "question", Answers: [][]string{{"PostgreSQL"}}}
				response = r.Answer
			} else {
				r.Decision = &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: true}
				r.Result = &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "committed"}}}
				response = r.Decision
			}
			d := NewProcessDriver(ProcessConfig{PendingDir: t.TempDir(), Workspace: t.TempDir(), Executable: "missing-codex", MaxFrameBytes: 4096})
			require.NoError(t, d.savePending(&r))
			_, _, pending, err := d.RestorePending()
			require.NoError(t, err)
			outcome, err := pending.Resume(t.Context(), response, &recordingSink{})
			require.NoError(t, err)
			require.NotNil(t, outcome.Pending)
			if question {
				response = &runtime.AskUserResponse{ID: "question", Answers: [][]string{{"MySQL"}}}
			} else {
				response = &runtime.ApprovalDecision{ID: r.Approval.ID, Approved: false}
			}
			_, err = outcome.Pending.Resume(t.Context(), response, &recordingSink{})
			require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			fresh := NewProcessDriver(d.config)
			_, _, restored, err := fresh.RestorePending()
			require.ErrorIs(t, err, runtime.ErrSessionAbandoned)
			require.Nil(t, restored)
		})
	}
}
