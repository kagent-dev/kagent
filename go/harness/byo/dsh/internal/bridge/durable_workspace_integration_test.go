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
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

func TestRealDSHRestartsWithDurableWorkspace(t *testing.T) {
	executable := os.Getenv("DSH_TEST_BIN")
	if executable == "" {
		t.Skip("DSH_TEST_BIN required")
	}
	dataDir := t.TempDir()
	file := filepath.Join(dataDir, "workspace", "continuity.txt")
	var mu sync.Mutex
	var requests []json.RawMessage
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		mu.Lock()
		requests = append(requests, body)
		index := len(requests)
		mu.Unlock()
		step := map[int]int{2: 1, 5: 2, 7: 3}[index]
		if step != 0 {
			writeCompletion(w, map[string]string{"role": "assistant", "content": fmt.Sprintf("finished step %d", step)}, "stop")
			return
		}
		args := map[string]string{"file_path": file}
		name := "read"
		switch index {
		case 1:
			name, args["content"] = "write", "first\n"
		case 3, 6:
		case 4:
			name, args["old_string"], args["new_string"] = "edit", "first", "second"
		default:
			http.Error(w, "unexpected model call", 500)
			return
		}
		encoded, _ := json.Marshal(args)
		writeCompletion(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("file-%d", index), "type": "function", "function": map[string]string{"name": name, "arguments": string(encoded)}}}}, "tool_calls")
	}))
	defer model.Close()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock","base_url":%q},"runtime":{"control_plane_url":"http://controller:8083"}}`, model.URL+"/v1")))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	nativeID, contextID := "", uuid.NewString()
	for i, prompt := range []string{"Create the continuity file.", "Replace first with second.", "Read the continuity file."} {
		// Rebuild both adapter and continuation objects; only the durable directory
		// carries conversation identity, history and files into the next turn.
		runner, err := New(cfg, executable, dataDir, append(os.Environ(), "OPENAI_API_KEY=mock", "DSH_PERMISSION_MODE=workspace-write"))
		require.NoError(t, err)
		store, err := continuation.New(filepath.Join(dataDir, "adapter"), "dsh-byo", ValidateSessionID)
		require.NoError(t, err)
		executor, err := runtimea2a.New(runner, store, tracing.RuntimeTelemetry{})
		require.NoError(t, err)
		client, api := newSettlementClient(t, executor, nil)
		message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(prompt))
		message.ContextID = contextID
		request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: message})
		require.NoError(t, err)
		_, err = client.SendMessage(ctx, request)
		require.NoError(t, err)
		select {
		case task := <-api.settled:
			require.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, task.Status.State)
		case <-ctx.Done():
			t.Fatal("file turn never settled", ctx.Err())
		}
		id, bound, err := store.Load()
		require.NoError(t, err)
		require.True(t, bound)
		if nativeID == "" {
			nativeID = id
		}
		require.Equal(t, nativeID, id)
		content, err := os.ReadFile(file)
		require.NoError(t, err)
		want := "second\n"
		if i == 0 {
			want = "first\n"
		}
		require.Equal(t, want, string(content))
	}
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requests, 7)
	require.Contains(t, string(requests[2]), "finished step 1")
	require.Contains(t, string(requests[5]), "finished step 2")
	var last struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(requests[6], &last))
	require.NotEmpty(t, last.Messages)
	message := last.Messages[len(last.Messages)-1]
	require.Equal(t, "tool", message.Role)
	require.Contains(t, message.Content, "second")
}
