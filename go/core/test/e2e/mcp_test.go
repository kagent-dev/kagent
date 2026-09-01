package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	mcpProtocolVersion = "2026-07-28"
	mcpTasksExtension  = "io.modelcontextprotocol/tasks"
)

func TestMCPAgentInstanceInteraction(t *testing.T) {
	fixture := newInteractionFixture(t, interactionTarget(t), startInteractionMock(t))
	endpoint := mcpEndpoint(t)

	listed := mcpCall(t, endpoint, "tools/call", map[string]any{
		"name": "list_agent_instances", "arguments": map[string]any{"namespace": "kagent"},
	}, false)
	instances := listed["result"].(map[string]any)["structuredContent"].(map[string]any)["agent_instances"].([]any)
	found := false
	for _, item := range instances {
		if item.(map[string]any)["id"] == fixture.instanceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list_agent_instances omitted %s: %#v", fixture.instanceID, instances)
	}

	created := mcpInvoke(t, endpoint, fixture.instanceID, "What is 2+2?", true)
	handle, ok := created["taskId"].(string)
	if !ok || handle == "" {
		t.Fatalf("task-capable MCP invocation = %#v", created)
	}
	completed := waitMCPTask(t, endpoint, handle, "completed")
	result := completed["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if !strings.Contains(mcpResultText(result), "The answer is 4.") {
		t.Fatalf("MCP task result = %#v", result)
	}

	request, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: a2atype.TaskID(structured["task_id"].(string))})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := fixture.client.GetTask(fixture.ctx, request)
	if err != nil || persisted.GetId() != structured["task_id"] {
		t.Fatalf("A2A task = %#v, error %v", persisted, err)
	}

	synchronous := mcpInvoke(t, endpoint, fixture.instanceID, "What is 2+2?", false)
	if synchronous["resultType"] != "complete" || !strings.Contains(mcpResultText(synchronous), "The answer is 4.") {
		t.Fatalf("synchronous MCP result = %#v", synchronous)
	}
}

func TestMCPAskUserContinuation(t *testing.T) {
	fixture := newInteractionFixture(t, interactionTarget(t), startMockLLM(t, "mocks/invoke_golang_hitl_ask_user.json"))
	endpoint := mcpEndpoint(t)
	handle := mcpInvoke(t, endpoint, fixture.instanceID, "Which database should we use for storage?", true)["taskId"].(string)
	waiting := waitMCPTask(t, endpoint, handle, "input_required")
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
	completed := waitMCPTask(t, endpoint, handle, "completed")
	if text := mcpResultText(completed["result"].(map[string]any)); !strings.Contains(text, "Using PostgreSQL") {
		t.Fatalf("continued MCP result = %q", text)
	}
}

func TestMCPCancelTask(t *testing.T) {
	modelURL, started := startBlockingInteractionMock(t)
	fixture := newInteractionFixture(t, interactionTarget(t), modelURL)
	endpoint := mcpEndpoint(t)
	handle := mcpInvoke(t, endpoint, fixture.instanceID, "Wait for cancellation", true)["taskId"].(string)
	select {
	case <-started:
	case <-time.After(time.Minute):
		t.Fatal("runtime did not call the blocking model")
	}
	mcpCall(t, endpoint, "tasks/cancel", map[string]any{"taskId": handle}, true)
	waitMCPTask(t, endpoint, handle, "cancelled")
}

func TestMCPCheckpointFork(t *testing.T) {
	fixture := newInteractionFixture(t, interactionTarget(t), startInteractionMock(t))
	endpoint := mcpEndpoint(t)
	if result := mcpInvoke(t, endpoint, fixture.instanceID, "What is 2+2?", false); result["resultType"] != "complete" {
		t.Fatalf("initial invocation = %#v", result)
	}

	created := mcpCall(t, endpoint, "tools/call", map[string]any{
		"name":      "create_agent_instance_checkpoint",
		"arguments": map[string]any{"namespace": "kagent", "agent_instance_id": fixture.instanceID},
	}, false)["result"].(map[string]any)["structuredContent"].(map[string]any)["checkpoint"].(map[string]any)
	checkpointID := created["id"].(string)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, err := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1alpha1.DeleteCheckpointRequest{Namespace: "kagent", CheckpointId: checkpointID})
		if err != nil && status.Code(err) != codes.NotFound {
			t.Errorf("delete checkpoint: %v", err)
		}
	})

	listed := mcpCall(t, endpoint, "tools/call", map[string]any{
		"name":      "list_agent_instance_checkpoints",
		"arguments": map[string]any{"namespace": "kagent", "agent_instance_id": fixture.instanceID},
	}, false)["result"].(map[string]any)["structuredContent"].(map[string]any)["checkpoints"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["id"] != checkpointID {
		t.Fatalf("listed checkpoints = %#v", listed)
	}

	forked := mcpCall(t, endpoint, "tools/call", map[string]any{
		"name":      "fork_agent_instance",
		"arguments": map[string]any{"namespace": "kagent", "checkpoint_id": checkpointID},
	}, false)["result"].(map[string]any)["structuredContent"].(map[string]any)["agent_instance"].(map[string]any)
	forkID := forked["id"].(string)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, err := fixture.instances.DeleteAgentInstance(ctx, &apiv1alpha1.DeleteAgentInstanceRequest{Namespace: "kagent", AgentInstanceId: forkID})
		if err != nil && status.Code(err) != codes.NotFound {
			t.Errorf("delete fork AgentInstance: %v", err)
		}
	})

	if result := mcpInvoke(t, endpoint, forkID, "What is 2+2?", false); !strings.Contains(mcpResultText(result), "The answer is 4.") {
		t.Fatalf("fork invocation = %#v", result)
	}
}

func mcpEndpoint(t *testing.T) string {
	t.Helper()
	host, _, err := net.SplitHostPort(interactionTarget(t))
	if err != nil {
		t.Fatalf("parse controller target: %v", err)
	}
	return "http://" + net.JoinHostPort(host, "8083") + "/mcp"
}

func mcpInvoke(t *testing.T, endpoint, instanceID, message string, tasks bool) map[string]any {
	t.Helper()
	response := mcpCall(t, endpoint, "tools/call", map[string]any{
		"name": "invoke_agent_instance",
		"arguments": map[string]any{
			"namespace": "kagent", "agent_instance_id": instanceID, "message": message,
		},
	}, tasks)
	return response["result"].(map[string]any)
}

func waitMCPTask(t *testing.T, endpoint, taskID, status string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		result := mcpCall(t, endpoint, "tasks/get", map[string]any{"taskId": taskID}, true)["result"].(map[string]any)
		if result["status"] == status {
			return result
		}
		if result["status"] == "failed" || result["status"] == "cancelled" || result["status"] == "completed" {
			t.Fatalf("MCP task reached %q, want %q: %#v", result["status"], status, result)
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("MCP task did not reach %q", status)
	return nil
}

func mcpCall(t *testing.T, endpoint, method string, params map[string]any, tasks bool) map[string]any {
	t.Helper()
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
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(body, []byte("event:")) {
		_, data, ok := bytes.Cut(body, []byte("data: "))
		if !ok {
			t.Fatalf("invalid MCP event stream: %s", body)
		}
		body = bytes.TrimSpace(data)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode MCP response (status %d): %v; body = %s", response.StatusCode, err, body)
	}
	if rpcErr := result["error"]; rpcErr != nil {
		t.Fatalf("MCP error: %#v", rpcErr)
	}
	return result
}

func mcpResultText(result map[string]any) string {
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	return text
}
