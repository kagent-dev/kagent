package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// fakeRemoteAgent is an A2A server whose send and task behavior each test
// scripts. Unscripted methods panic through the nil embedded handler.
type fakeRemoteAgent struct {
	a2asrv.RequestHandler

	mu    sync.Mutex
	sends []*a2atype.SendMessageRequest
	// send answers the nth send (1-based).
	send func(n int, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error)
	// tasks are returned by successive GetTask calls; the last one repeats.
	tasks     []*a2atype.Task
	getTasks  int
	subscribe int
}

func (f *fakeRemoteAgent) SendMessage(_ context.Context, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	f.mu.Lock()
	f.sends = append(f.sends, req)
	n := len(f.sends)
	f.mu.Unlock()
	return f.send(n, req)
}

func (f *fakeRemoteAgent) GetTask(context.Context, *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tasks) == 0 {
		return nil, a2atype.ErrTaskNotFound
	}
	task := f.tasks[min(f.getTasks, len(f.tasks)-1)]
	f.getTasks++
	return task, nil
}

func (f *fakeRemoteAgent) SubscribeToTask(context.Context, *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	f.mu.Lock()
	f.subscribe++
	f.mu.Unlock()
	return func(func(a2atype.Event, error) bool) {}
}

func (f *fakeRemoteAgent) sent() []*a2atype.SendMessageRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sends)
}

// newFakeRemoteAgentClient serves agent over JSON-RPC. With loseSendResponse,
// every SendMessage is fully handled by the agent and then the connection is
// dropped before the response is written, so the client sees a transport
// error for input the agent already applied.
func newFakeRemoteAgentClient(t *testing.T, agent *fakeRemoteAgent, loseSendResponse bool) *a2aclient.Client {
	t.Helper()
	jsonrpc := a2asrv.NewJSONRPCHandler(agent)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var envelope struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &envelope)
		r.Body = io.NopCloser(bytes.NewReader(body))
		if !loseSendResponse || envelope.Method != "SendMessage" {
			jsonrpc.ServeHTTP(w, r)
			return
		}
		jsonrpc.ServeHTTP(httptest.NewRecorder(), r)
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(server.Close)
	client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2atype.AgentInterface{{
		URL: server.URL, ProtocolBinding: a2atype.TransportProtocolJSONRPC, ProtocolVersion: a2atype.Version,
	}}, a2aclient.WithJSONRPCTransport(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Destroy() })
	return client
}

func notAccepted(meta map[string]string) error {
	return a2atype.NewError(a2atype.ErrUnsupportedOperation, "input was not accepted").WithErrorInfoMeta(meta)
}

func TestSendMessageWithRetry(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		wantSends int
		wantErr   bool
	}{
		{name: "proven rejection", err: notAccepted(map[string]string{"reason": sendNotAcceptedReason, "retryAfterMs": "1"}), wantSends: 2},
		{name: "proven rejection without delay", err: notAccepted(map[string]string{"reason": sendNotAcceptedReason}), wantSends: 2},
		{name: "other reason", err: notAccepted(map[string]string{"reason": "OTHER"}), wantSends: 1, wantErr: true},
		{name: "unsupported operation", err: a2atype.ErrUnsupportedOperation, wantSends: 1, wantErr: true},
		{
			name:      "reason on wrong error",
			err:       a2atype.NewError(a2atype.ErrInternalError, "boom").WithErrorInfoMeta(map[string]string{"reason": sendNotAcceptedReason}),
			wantSends: 1,
			wantErr:   true,
		},
		{name: "internal error", err: a2atype.ErrInternalError, wantSends: 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			done := &a2atype.Task{ID: "task", ContextID: "ctx", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}
			agent := &fakeRemoteAgent{send: func(n int, _ *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
				if n == 1 {
					return nil, test.err
				}
				return done, nil
			}}
			client := newFakeRemoteAgentClient(t, agent, false)
			req := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi"))}

			result, err := sendMessageWithRetry(t.Context(), client, req)
			if (err != nil) != test.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr {
				if task, ok := result.(*a2atype.Task); !ok || task.ID != done.ID {
					t.Errorf("result = %#v, want task %q", result, done.ID)
				}
			}
			sends := agent.sent()
			if len(sends) != test.wantSends {
				t.Fatalf("sends = %d, want %d", len(sends), test.wantSends)
			}
			for _, sent := range sends {
				if sent.Message.ID != req.Message.ID {
					t.Errorf("resent message ID %q, want %q", sent.Message.ID, req.Message.ID)
				}
			}
		})
	}
}

func TestSendMessageWithRetryNeverResendsDeliveredInput(t *testing.T) {
	agent := &fakeRemoteAgent{send: func(int, *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
		return &a2atype.Task{ID: "task", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}, nil
	}}
	client := newFakeRemoteAgentClient(t, agent, true)
	req := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi"))}

	if _, err := sendMessageWithRetry(t.Context(), client, req); err == nil {
		t.Fatal("expected the lost response to surface as an error")
	}
	if got := len(agent.sent()); got != 1 {
		t.Errorf("sends = %d, want 1", got)
	}
}

func TestSendMessageWithRetryStopsWhenContextDone(t *testing.T) {
	agent := &fakeRemoteAgent{send: func(int, *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
		return nil, notAccepted(map[string]string{"reason": sendNotAcceptedReason, "retryAfterMs": "10"})
	}}
	client := newFakeRemoteAgentClient(t, agent, false)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	req := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi"))}

	_, err := sendMessageWithRetry(ctx, client, req)
	if !errors.Is(err, a2atype.ErrUnsupportedOperation) {
		t.Errorf("err = %v, want the last rejection", err)
	}
}

// resumeContext is the minimal agent context handleResume reads.
type resumeContext struct {
	adkagent.StrictContextMock
	confirmation *toolconfirmation.ToolConfirmation
}

func (c *resumeContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return c.confirmation }
func (c *resumeContext) UserID() string                                       { return "user" }
func (c *resumeContext) SessionID() string                                    { return "parent-session" }

func newResumeContext(t *testing.T, taskID, contextID string) *resumeContext {
	t.Helper()
	state := a2a.RemoteHitlState{
		TaskID:       taskID,
		ContextID:    contextID,
		SubagentName: "worker",
		ToolApprovalRequest: &apia2a.ToolApprovalRequest{
			Type:  a2a.HITLTypeToolApprovalRequest,
			Tools: []apia2a.HITLTool{{ID: "confirm", CallID: "call", Name: "delete_pod", Args: map[string]any{}}},
		},
		ToolApprovalResponse: &apia2a.ToolApprovalResponse{
			Type:      a2a.HITLTypeToolApprovalResponse,
			Approvals: []apia2a.ToolApproval{{ID: "confirm", Approved: true}},
		},
	}
	return &resumeContext{
		StrictContextMock: adkagent.NewStrictContextMock(t.Context()),
		confirmation:      &toolconfirmation.ToolConfirmation{Confirmed: true, Payload: state.ToMap()},
	}
}

func newResumeState(client *a2aclient.Client) *remoteA2AState {
	s := &remoteA2AState{name: "worker", a2aClient: client}
	s.initOnce.Do(func() {})
	return s
}

func taskWithHistory(state a2atype.TaskState, text string, history ...*a2atype.Message) *a2atype.Task {
	return &a2atype.Task{
		ID:        "child-task",
		ContextID: "child-context",
		History:   history,
		Status: a2atype.TaskStatus{
			State:   state,
			Message: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(text)),
		},
	}
}

func TestHandleResumeAfterLostResponse(t *testing.T) {
	for _, test := range []struct {
		name          string
		tasks         func(resume *a2atype.Message) []*a2atype.Task
		wantResult    string
		wantSubscribe bool
	}{
		{
			name: "decision applied and task completed",
			tasks: func(resume *a2atype.Message) []*a2atype.Task {
				return []*a2atype.Task{taskWithHistory(a2atype.TaskStateCompleted, "pod deleted", resume)}
			},
			wantResult: "pod deleted",
		},
		{
			name: "decision applied and task still working",
			tasks: func(resume *a2atype.Message) []*a2atype.Task {
				return []*a2atype.Task{
					taskWithHistory(a2atype.TaskStateWorking, "", resume),
					taskWithHistory(a2atype.TaskStateCompleted, "pod deleted", resume),
				}
			},
			wantResult:    "pod deleted",
			wantSubscribe: true,
		},
		{
			name: "decision not in task history",
			tasks: func(*a2atype.Message) []*a2atype.Task {
				return []*a2atype.Task{taskWithHistory(a2atype.TaskStateInputRequired, "approval required")}
			},
		},
		{
			name: "task never settles",
			tasks: func(resume *a2atype.Message) []*a2atype.Task {
				return []*a2atype.Task{taskWithHistory(a2atype.TaskStateWorking, "", resume)}
			},
			wantSubscribe: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := &fakeRemoteAgent{}
			agent.send = func(_ int, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
				agent.mu.Lock()
				agent.tasks = test.tasks(req.Message)
				agent.mu.Unlock()
				return agent.tasks[len(agent.tasks)-1], nil
			}
			s := newResumeState(newFakeRemoteAgentClient(t, agent, true))

			resp, err := s.handleResume(newResumeContext(t, "child-task", "child-context"))
			if err != nil {
				t.Fatalf("handleResume: %v", err)
			}
			if got := len(agent.sent()); got != 1 {
				t.Errorf("sends = %d, want exactly 1: a delivered decision must never be resent", got)
			}
			if resp.Result != test.wantResult {
				t.Errorf("Result = %q, want %q", resp.Result, test.wantResult)
			}
			if test.wantResult == "" && !strings.Contains(resp.Error, "resume failed") {
				t.Errorf("Error = %q, want the original resume failure", resp.Error)
			}
			if (agent.subscribe > 0) != test.wantSubscribe {
				t.Errorf("subscribed = %d, want subscribe %v", agent.subscribe, test.wantSubscribe)
			}
			if test.wantResult != "" && resp.SubagentSessionID != "child-context" {
				t.Errorf("SubagentSessionID = %q, want %q", resp.SubagentSessionID, "child-context")
			}
		})
	}
}
