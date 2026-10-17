package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	mcpProtocolVersion = "2026-07-28"
	mcpTasksExtension  = "io.modelcontextprotocol/tasks"
)

func TestMCPSessionInteraction(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		endpoint := mcpEndpoint(t)

		found := false
		for pageToken := ""; !found; {
			listed := mcpCall(t, endpoint, "tools/call", map[string]any{
				"name": "list_sessions", "arguments": map[string]any{"page_token": pageToken},
			}, false)
			output := listed["result"].(map[string]any)["structuredContent"].(map[string]any)
			for _, item := range output["sessions"].([]any) {
				if item.(map[string]any)["id"] == fixture.sessionID {
					found = true
					break
				}
			}
			if found {
				break
			}
			// Filtering to ready Sessions can leave an empty page with a next token.
			next, _ := output["next_page_token"].(string)
			if next == "" || next == pageToken {
				t.Fatalf("list_sessions omitted %s after following pagination", fixture.sessionID)
			}
			pageToken = next
		}

		_, completed := fixture.mcpInvokeTask(t, endpoint, "What is 2+2?", "completed")
		result := completed["result"].(map[string]any)
		structured := result["structuredContent"].(map[string]any)
		if !strings.Contains(mcpResultText(result), "The answer is 4.") {
			t.Fatalf("MCP task result = %#v", result)
		}

		request, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{Tenant: fixture.tenant, ID: a2atype.TaskID(structured["task_id"].(string))})
		if err != nil {
			t.Fatal(err)
		}
		persisted, err := fixture.client.GetTask(fixture.ctx, request)
		if err != nil || persisted.GetId() != structured["task_id"] {
			t.Fatalf("A2A task = %#v, error %v", persisted, err)
		}

		synchronous := fixture.mcpInvoke(t, endpoint, fixture.sessionID, "What is 2+2?", false)
		if synchronous["resultType"] != "complete" || !strings.Contains(mcpResultText(synchronous), "The answer is 4.") {
			t.Fatalf("synchronous MCP result = %#v", synchronous)
		}
	})
}

func TestMCPAskUserContinuation(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		switch harness.name {
		case codexE2EHarness, claudeE2EHarness:
			t.Skip("native ask-user model fixtures are not available yet; this fixture calls the Go ADK ask_user tool")
		}
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startMockLLM(t, "mocks/invoke_golang_hitl_ask_user.json"))
		endpoint := mcpEndpoint(t)
		handle, waiting := fixture.mcpInvokeTask(t, endpoint, "Which database should we use for storage?", "input_required")
		requests := waiting["inputRequests"].(map[string]any)
		if len(requests) != 1 {
			t.Fatalf("inputRequests = %#v", requests)
		}
		var key string
		for key = range requests {
		}
		mcpCall(t, endpoint, "tasks/update", map[string]any{
			"taskId": handle,
			"inputResponses": map[string]any{key: map[string]any{
				"action": "accept", "content": map[string]any{"response": "PostgreSQL"},
			}},
		}, true)
		completed := fixture.waitMCPTask(t, endpoint, handle, "completed")
		if text := mcpResultText(completed["result"].(map[string]any)); !strings.Contains(text, "Using PostgreSQL") {
			t.Fatalf("continued MCP result = %q", text)
		}
	})
}

func TestMCPCancelTask(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		target := interactionTarget(t)
		modelURL, started := startBlockingInteractionMock(t)
		fixture := newInteractionFixture(t, harness, target, modelURL)
		endpoint := mcpEndpoint(t)
		handle := fixture.mcpInvoke(t, endpoint, fixture.sessionID, "Wait for cancellation", true)["taskId"].(string)
		select {
		case <-started:
		case <-time.After(time.Minute):
			t.Fatal("runtime did not call the blocking model")
		}
		mcpCall(t, endpoint, "tasks/cancel", map[string]any{"taskId": handle}, true)
		fixture.waitMCPTask(t, endpoint, handle, "cancelled")
	})
}

func TestMCPCheckpointFork(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		endpoint := mcpEndpoint(t)
		initial := fixture.mcpInvoke(t, endpoint, fixture.sessionID, "What is 2+2?", false)
		if initial["resultType"] != "complete" {
			t.Fatalf("initial invocation = %#v", initial)
		}
		taskID := initial["structuredContent"].(map[string]any)["task_id"].(string)

		request := map[string]any{
			"name":      "create_session_checkpoint",
			"arguments": map[string]any{"session_id": fixture.sessionID, "request_id": uuid.NewString(), "expected_head_task_id": taskID},
		}
		// Task completion precedes its idle snapshot. Retry the same checkpoint
		// request until that snapshot is available; report the last tool error.
		var result map[string]any
		err := waitForStep(t.Context(), mcpStep("tools/call", request), "a checkpoint of task "+taskID, mcpStepTimeout, 100*time.Millisecond,
			func(ctx context.Context) (bool, string, error) {
				response, err := mcpRequest(ctx, endpoint, "tools/call", request, false)
				if err != nil {
					return false, "", err
				}
				result = response["result"].(map[string]any)
				if result["isError"] != true {
					return true, "", nil
				}
				if meta, ok := result["_meta"].(map[string]any); ok && meta["kagent.dev/error-reason"] == "KAGENT_CHECKPOINT_SNAPSHOT_PENDING" {
					return false, fmt.Sprintf("snapshot pending: %#v", result), nil
				}
				return false, "", fmt.Errorf("checkpoint failed: %#v", result)
			})
		if err != nil {
			t.Fatal(err)
		}
		created := result["structuredContent"].(map[string]any)["checkpoint"].(map[string]any)
		checkpointID := created["id"].(string)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cancel()
			_, err := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1alpha1.DeleteCheckpointRequest{CheckpointId: checkpointID})
			if err != nil && status.Code(err) != codes.NotFound {
				t.Errorf("delete checkpoint: %v", err)
			}
		})

		listed := mcpCall(t, endpoint, "tools/call", map[string]any{
			"name":      "list_session_checkpoints",
			"arguments": map[string]any{"session_id": fixture.sessionID},
		}, false)["result"].(map[string]any)["structuredContent"].(map[string]any)["checkpoints"].([]any)
		if len(listed) != 1 || listed[0].(map[string]any)["id"] != checkpointID {
			t.Fatalf("listed checkpoints = %#v", listed)
		}

		if !checkpointForksSupported {
			result := mcpCall(t, endpoint, "tools/call", map[string]any{
				"name":      "fork_session",
				"arguments": map[string]any{"checkpoint_id": checkpointID},
			}, false)["result"].(map[string]any)
			if result["isError"] != true {
				t.Fatalf("fork_session accepted a process-memory checkpoint: %#v", result)
			}
			return
		}
		forked := mcpCall(t, endpoint, "tools/call", map[string]any{
			"name":      "fork_session",
			"arguments": map[string]any{"checkpoint_id": checkpointID},
		}, false)["result"].(map[string]any)["structuredContent"].(map[string]any)["session"].(map[string]any)
		forkID := forked["id"].(string)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cancel()
			if err := deleteIdleSession(ctx, fixture.sessions, forkID); err != nil {
				t.Errorf("delete fork Session: %v", err)
			}
		})

		if result := fixture.mcpInvoke(t, endpoint, forkID, "What is 2+2?", false); !strings.Contains(mcpResultText(result), "The answer is 4.") {
			t.Fatalf("fork invocation = %#v", result)
		}
	})
}

func mcpEndpoint(t *testing.T) string {
	t.Helper()
	return "http://" + interactionTarget(t) + "/mcp"
}

// mcpStepTimeout bounds each MCP step a test waits on: one call, including the
// turn a synchronous invocation waits for, or a poll for a state.
const mcpStepTimeout = 2 * time.Minute

// mcpInvoke follows the gateway's rejection contract as sendMessageWithRetry
// does: a completed task can precede the session settling its turn, and the
// MCP tool reports the refusal that proves the input was not accepted as its
// error text. Only that refusal is sent again. A synchronous invocation waits
// for its turn, so a turn that never ends fails the step with the Session's
// state and its tasks'.
func (f *interactionFixture) mcpInvoke(t *testing.T, endpoint, sessionID, message string, tasks bool) map[string]any {
	t.Helper()
	params := map[string]any{
		"name":      "invoke_session",
		"arguments": map[string]any{"session_id": sessionID, "message": message},
	}
	want := "the turn's result"
	if tasks {
		want = "a task handle"
	}
	var result map[string]any
	err := waitForStep(t.Context(), mcpStep("tools/call", params), want, mcpStepTimeout, 250*time.Millisecond,
		func(ctx context.Context) (bool, string, error) {
			response, err := mcpRequest(ctx, endpoint, "tools/call", params, tasks)
			if err != nil {
				return false, "", err
			}
			result, _ = response["result"].(map[string]any)
			if result == nil || result["isError"] != true || mcpResultText(result) != sessionsvc.ErrSendNotAccepted.Error() {
				return true, "", nil
			}
			return false, "the send was not accepted", nil
		})
	if err != nil {
		t.Fatalf("%v; %s", err, f.turnState(sessionID))
	}
	if result == nil || result["isError"] == true || (tasks && result["taskId"] == nil) {
		t.Fatalf("MCP invocation failed: %#v", result)
	}
	return result
}

// turnState reports the Session's runtime state and the state of each of its
// tasks: what a step that waited on a turn last saw of it.
func (f *interactionFixture) turnState(sessionID string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(f.ctx), 10*time.Second)
	defer cancel()
	response, err := f.sessions.GetSession(ctx, &apiv1alpha1.GetSessionRequest{SessionId: sessionID})
	if err != nil {
		return fmt.Sprintf("Session %s: %v", sessionID, err)
	}
	session := response.GetSession()
	state := fmt.Sprintf("Session %s %s, operation %s", sessionID, session.GetState(), session.GetOperation())
	request, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{Tenant: f.tenant, ContextID: session.GetContextId()})
	if err != nil {
		return fmt.Sprintf("%s; tasks: %v", state, err)
	}
	listed, err := f.client.ListTasks(ctx, request)
	if err != nil {
		return fmt.Sprintf("%s; tasks: %v", state, err)
	}
	for _, task := range listed.GetTasks() {
		state += fmt.Sprintf("; task %s %s", task.GetId(), task.GetStatus().GetState())
	}
	return state
}

// mcpTurnLostAfter bounds how long the task of a task-capable invocation may
// stay "working" before mcpInvokeTask takes its turn as lost. The mock model
// answers in milliseconds. Under gVisor a model response the egress gateway
// delivered can fail to reach the runtime, which then waits on it for the model
// client's timeout; the task stays "working" and its Actor idles.
const mcpTurnLostAfter = time.Minute

// mcpInvokeTask invokes a turn on the fixture's Session as an MCP task and
// waits until the task reaches status; it returns the task's handle and its
// tasks/get result. A turn lost in "working" is cancelled
// and invoked once more (retryLostMCPTurn); a task that does not reach status
// fails the step with the Session's state, its tasks' and its Actor's.
func (f *interactionFixture) mcpInvokeTask(t *testing.T, endpoint, message, status string) (string, map[string]any) {
	t.Helper()
	invoke := func() string {
		created := f.mcpInvoke(t, endpoint, f.sessionID, message, true)
		handle, ok := created["taskId"].(string)
		if !ok || handle == "" {
			t.Fatalf("task-capable MCP invocation = %#v", created)
		}
		return handle
	}
	poll := func(handle, want string, timeout time.Duration) (map[string]any, string, error) {
		return pollMCPTask(t.Context(), endpoint, handle, want, timeout)
	}
	cancel := func(handle string) error {
		ctx, cancel := context.WithTimeout(t.Context(), mcpStepTimeout)
		defer cancel()
		_, err := mcpRequest(ctx, endpoint, "tasks/cancel", map[string]any{"taskId": handle}, true)
		return err
	}
	lost := func(handle string) {
		t.Logf("MCP task %s still working after %s, cancelled and invoked once more; %s; %s", handle, mcpTurnLostAfter, f.turnState(f.sessionID), f.actorReport())
	}
	handle, result, err := retryLostMCPTurn(mcpTurnLostAfter, status, invoke, poll, cancel, lost)
	if err != nil {
		t.Fatalf("%v; %s; %s", err, f.turnState(f.sessionID), f.actorReport())
	}
	return handle, result
}

// retryLostMCPTurn runs one task-capable turn to want. Only a task last seen
// "working" when lostAfter runs out is taken as lost: it is cancelled, its
// cancellation awaited, and the turn invoked once more under mcpStepTimeout.
// Any other outcome, a task stuck in "submitted" included, is returned as it is.
func retryLostMCPTurn(lostAfter time.Duration, want string,
	invoke func() string,
	poll func(handle, want string, timeout time.Duration) (map[string]any, string, error),
	cancel func(handle string) error,
	lost func(handle string),
) (string, map[string]any, error) {
	handle := invoke()
	result, last, err := poll(handle, want, lostAfter)
	if err == nil || last != "working" || !errors.Is(err, context.DeadlineExceeded) {
		return handle, result, err
	}
	lost(handle)
	if err := cancel(handle); err != nil {
		return handle, nil, fmt.Errorf("cancel lost MCP task %s: %w", handle, err)
	}
	if _, _, err := poll(handle, "cancelled", mcpStepTimeout); err != nil {
		return handle, nil, err
	}
	handle = invoke()
	result, _, err = poll(handle, want, mcpStepTimeout)
	return handle, result, err
}

// waitMCPTask waits until the MCP task reaches status; a task that does not
// fails the step with the Session's state, its tasks' and its Actor's.
func (f *interactionFixture) waitMCPTask(t *testing.T, endpoint, taskID, status string) map[string]any {
	t.Helper()
	result, _, err := pollMCPTask(t.Context(), endpoint, taskID, status, mcpStepTimeout)
	if err != nil {
		t.Fatalf("%v; %s; %s", err, f.turnState(f.sessionID), f.actorReport())
	}
	return result
}

// pollMCPTask polls tasks/get until the task reaches status, for at most
// timeout, and returns the last status it saw. A task that reaches another
// terminal status ends the poll at once.
func pollMCPTask(ctx context.Context, endpoint, taskID, status string, timeout time.Duration) (map[string]any, string, error) {
	params := map[string]any{"taskId": taskID}
	var result map[string]any
	var last string
	err := waitForStep(ctx, mcpStep("tasks/get", params), fmt.Sprintf("status %q", status), timeout, 250*time.Millisecond,
		func(ctx context.Context) (bool, string, error) {
			response, err := mcpRequest(ctx, endpoint, "tasks/get", params, true)
			if err != nil {
				return false, "", err
			}
			result = response["result"].(map[string]any)
			last, _ = result["status"].(string)
			seen := fmt.Sprintf("status %q", result["status"])
			if last == status {
				return true, seen, nil
			}
			if last == "failed" || last == "cancelled" || last == "completed" {
				return false, seen, fmt.Errorf("MCP task reached %q, want %q: %#v", last, status, result)
			}
			return false, seen, nil
		})
	return result, last, err
}

// mcpStep names an MCP call for a failure: the method, the tool and the
// Session, checkpoint or task it is about.
func mcpStep(method string, params map[string]any) string {
	step := "MCP " + method
	if name, ok := params["name"].(string); ok {
		step += " " + name
	}
	if taskID, ok := params["taskId"].(string); ok {
		step += " of task " + taskID
	}
	arguments, _ := params["arguments"].(map[string]any)
	for _, key := range []string{"session_id", "checkpoint_id"} {
		if value, ok := arguments[key].(string); ok {
			step += " " + key + "=" + value
		}
	}
	return step
}

// mcpCall makes one MCP call that must answer within mcpStepTimeout.
func mcpCall(t *testing.T, endpoint, method string, params map[string]any, tasks bool) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), mcpStepTimeout)
	defer cancel()
	response, err := mcpRequest(ctx, endpoint, method, params, tasks)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%s: no answer within %s: %v", mcpStep(method, params), mcpStepTimeout, err)
	}
	if err != nil {
		t.Fatalf("%s: %v", mcpStep(method, params), err)
	}
	return response
}

// mcpRequest posts one MCP request under ctx and decodes its JSON-RPC result.
func mcpRequest(ctx context.Context, endpoint, method string, params map[string]any, tasks bool) (map[string]any, error) {
	extensions := map[string]any{}
	if tasks {
		extensions[mcpTasksExtension] = map[string]any{}
	}
	params["_meta"] = map[string]any{
		mcp.MetaKeyProtocolVersion: mcpProtocolVersion,
		mcp.MetaKeyClientInfo:      map[string]any{"name": "kagent-e2e", "version": "1"},
		mcp.MetaKeyClientCapabilities: map[string]any{
			"extensions": extensions,
		},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Protocol-Version", mcpProtocolVersion)
	request.Header.Set("Mcp-Method", method)
	request.Header.Set("X-User-Id", "e2e")
	if method == "tools/call" {
		request.Header.Set("Mcp-Name", params["name"].(string))
	} else if strings.HasPrefix(method, "tasks/") {
		request.Header.Set("Mcp-Name", params["taskId"].(string))
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err = io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(body, []byte("event:")) {
		_, data, ok := bytes.Cut(body, []byte("data: "))
		if !ok {
			return nil, fmt.Errorf("invalid MCP event stream: %s", body)
		}
		body = bytes.TrimSpace(data)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode MCP response (status %d): %w; body = %s", response.StatusCode, err, body)
	}
	if rpcErr := result["error"]; rpcErr != nil {
		return nil, fmt.Errorf("MCP error: %#v", rpcErr)
	}
	return result, nil
}

func mcpResultText(result map[string]any) string {
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	return text
}
