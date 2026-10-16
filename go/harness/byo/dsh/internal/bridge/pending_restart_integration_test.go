//go:build linux

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// Session resume repairs an interrupted turn; it does not restore its pending
// approval. Keep this capability probe separate from approval-resume acceptance.
func TestRealDSHRestartClosesPendingCallWithoutReplay(t *testing.T) {
	executable := os.Getenv("DSH_TEST_BIN")
	if executable == "" {
		t.Skip("DSH_TEST_BIN required")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "must-not-execute")
	var mu sync.Mutex
	var requests []json.RawMessage
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, body)
		index := len(requests)
		mu.Unlock()
		if index == 1 {
			args, _ := json.Marshal(map[string]string{
				"command":     "printf executed >> '" + strings.ReplaceAll(marker, "'", "'\\''") + "'",
				"description": "Write after approval", "sandbox_permissions": "danger-full-access",
				"justification": "Check approval persistence with a test marker.",
			})
			writeCompletion(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
				"index": 0, "id": "interrupted-write", "type": "function",
				"function": map[string]string{"name": "bash", "arguments": string(args)},
			}}}, "tool_calls")
			return
		}
		writeCompletion(w, map[string]string{"role": "assistant", "content": "The old call was interrupted."}, "stop")
	}))
	defer model.Close()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock","base_url":%q},"runtime":{"control_plane_url":"http://controller:8083"}}`, model.URL+"/v1")))
	require.NoError(t, err)
	environment := append(os.Environ(), "OPENAI_API_KEY=mock", "DSH_PERMISSION_MODE=workspace-write")
	runner, err := New(cfg, executable, dir, environment)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	first := &recordingSink{}
	out, err := runner.Run(ctx, runtime.Turn{Prompt: "Request the guarded write."}, first)
	require.NoError(t, err)
	pending, ok := out.Pending.(*pending)
	require.True(t, ok)
	require.Equal(t, "interrupted-write", pending.request.CallID)
	cleanup := sync.OnceValue(pending.process.cleanup)
	t.Cleanup(func() { _ = cleanup() })
	// Terminate without sending any permission response or session/cancel.
	require.NoError(t, pending.process.control.Close())
	select {
	case <-pending.process.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, cleanup())

	// A fresh adapter can only provide the durable native session ID. It has
	// no original ACP promise, request ID, or in-memory PendingTurn to resume.
	runner, err = New(cfg, executable, dir, environment)
	require.NoError(t, err)
	resumed := &recordingSink{}
	out, err = runner.Run(ctx, runtime.Turn{Prompt: "Explain the interrupted call without retrying it.", ContinuationID: first.id}, resumed)
	require.NoError(t, err)
	require.Nil(t, out.Pending, "native resume did not restore the permission request")
	require.Equal(t, first.id, resumed.id)
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "unapproved command must not execute")
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requests, 2)
	var second struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			Content    string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(requests[1], &second))
	var result string
	for _, message := range second.Messages {
		if message.Role == "tool" && message.ToolCallID == "interrupted-write" {
			result = message.Content
		}
	}
	require.Contains(t, result, "outcome is unknown")
	require.Contains(t, result, "Do not retry blindly")
}
