package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCheckpointSummary(t *testing.T) {
	created := time.Date(2026, time.August, 26, 10, 0, 0, 123, time.UTC)
	got := checkpointSummary(&apiv1alpha1.Checkpoint{
		Id: "22222222-2222-4222-8222-222222222222", Namespace: "team-a",
		AgentInstanceId: testInstanceID, HeadTaskId: testTaskID, HistorySequence: 7,
		State: apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY, CreatedAt: timestamppb.New(created),
		Failure: &apiv1alpha1.Failure{Message: "failed"},
	})
	if got.ID != "22222222-2222-4222-8222-222222222222" || got.AgentInstanceID != testInstanceID ||
		got.HistorySequence != 7 || got.State != "CHECKPOINT_STATE_READY" || got.CreatedAt != created.Format(time.RFC3339Nano) || got.Failure.Message != "failed" {
		t.Fatalf("checkpointSummary() = %#v", got)
	}
}

func TestCheckpointToolsAreRegistered(t *testing.T) {
	h, err := New(testAgentInstanceService(), testCheckpointService(), &a2asrv.InterceptedHandler{Handler: &fakeGateway{}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer server.Close()
	response := rawMCPCall(t, server.URL, "tools/list", map[string]any{}, false)
	tools := response["result"].(map[string]any)["tools"].([]any)
	want := map[string]bool{createCheckpointToolName: false, listCheckpointsToolName: false, forkAgentInstanceToolName: false}
	for _, value := range tools {
		tool := value.(map[string]any)
		if _, ok := want[tool["name"].(string)]; ok {
			want[tool["name"].(string)] = len(tool["inputSchema"].(map[string]any)["properties"].(map[string]any)) > 0
		}
	}
	for name, valid := range want {
		if !valid {
			t.Fatalf("tool %q missing or has no input schema: %#v", name, tools)
		}
	}
}

func TestCheckpointToolErrorsAreToolResults(t *testing.T) {
	h := &Handler{checkpoints: testCheckpointService()}
	result, _, err := h.createCheckpoint(t.Context(), nil, CreateCheckpointInput{Namespace: "team-a", AgentInstanceID: "invalid"})
	if err != nil || !result.IsError {
		t.Fatalf("createCheckpoint() result = %#v, error = %v", result, err)
	}
}

func TestStableRequestID(t *testing.T) {
	if got := stableRequestID("caller-id"); got != "caller-id" {
		t.Fatalf("stableRequestID() = %q", got)
	}
	if got := stableRequestID(""); got == "" {
		t.Fatal("stableRequestID() returned an empty generated ID")
	}
}
