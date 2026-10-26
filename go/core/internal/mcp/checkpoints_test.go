package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCheckpointSummary(t *testing.T) {
	created := time.Date(2026, time.August, 26, 10, 0, 0, 123, time.UTC)
	got := checkpointSummary(&apiv1.Checkpoint{
		Id:        "22222222-2222-4222-8222-222222222222",
		SessionId: testSessionID, HeadTaskId: testTaskID, HistorySequence: 7,
		State: apiv1.CheckpointState_CHECKPOINT_STATE_READY, CreatedAt: timestamppb.New(created),
		Failure: &apiv1.Failure{Message: "failed"},
	})
	if got.ID != "22222222-2222-4222-8222-222222222222" || got.SessionID != testSessionID ||
		got.HistorySequence != 7 || got.State != "CHECKPOINT_STATE_READY" || got.CreatedAt != created.Format(time.RFC3339Nano) || got.Failure.Message != "failed" {
		t.Fatalf("checkpointSummary() = %#v", got)
	}
}

func TestCheckpointToolsAreRegistered(t *testing.T) {
	h, err := New(testSessionService(), testCheckpointService(), &a2asrv.InterceptedHandler{Handler: &fakeGateway{}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	defer server.Close()
	response := rawMCPCall(t, server.URL, "tools/list", map[string]any{}, false)
	tools := response["result"].(map[string]any)["tools"].([]any)
	want := map[string]bool{createCheckpointToolName: false, listCheckpointsToolName: false, forkSessionToolName: false}
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
	result, _, err := h.createCheckpoint(t.Context(), nil, CreateCheckpointInput{SessionID: "invalid"})
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

type checkpointToolDenialPolicy struct {
	verb     auth.Verb
	resource auth.Resource
	calls    int
}

func (c *checkpointToolDenialPolicy) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if verb == c.verb && resource == c.resource {
		c.calls++
		return errors.New("checkpoint permission denied")
	}
	return nil
}

func TestCheckpointToolsEnforcePermissionsOverMCP(t *testing.T) {
	const checkpointID = "22222222-2222-4222-8222-222222222222"
	for _, test := range []struct {
		tool      string
		arguments map[string]any
		verb      auth.Verb
		name      string
	}{
		{createCheckpointToolName, map[string]any{"session_id": testSessionID, "expected_head_task_id": testTaskID, "request_id": "create-request"}, auth.VerbCreate, ""},
		{listCheckpointsToolName, map[string]any{"session_id": testSessionID}, auth.VerbList, ""},
		{forkSessionToolName, map[string]any{"checkpoint_id": checkpointID, "request_id": "fork-request"}, auth.VerbGet, checkpointID},
	} {
		t.Run(test.tool, func(t *testing.T) {
			policy := &checkpointToolDenialPolicy{verb: test.verb, resource: auth.Resource{Type: "Checkpoint", Name: test.name}}
			// Nil dependencies prove a denied tool cannot reach storage or runtime work.
			h, err := New(testSessionService(), checkpoint.NewService(nil, policy, nil, nil), &a2asrv.InterceptedHandler{Handler: &fakeGateway{}}, nil, nil)
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.ServeHTTP(w, r.WithContext(authContext()))
			}))
			t.Cleanup(server.Close)
			response := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": test.tool, "arguments": test.arguments}, false)
			result := response["result"].(map[string]any)
			require.Equal(t, true, result["isError"])
			require.Contains(t, result["content"].([]any)[0].(map[string]any)["text"], "Not authorized")
			require.Equal(t, 1, policy.calls)
		})
	}
}
