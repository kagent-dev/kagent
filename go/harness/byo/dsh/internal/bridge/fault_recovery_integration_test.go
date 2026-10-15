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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// Losing DSH is different from losing the adapter: the live adapter still owns
// the pending task and can confirm process cleanup before settling its failure.
func TestRealDSHApprovalProcessDeathAllowsNextTask(t *testing.T) {
	executable := os.Getenv("DSH_TEST_BIN")
	if executable == "" {
		t.Skip("DSH_TEST_BIN required")
	}
	for _, action := range []string{"allow", "reject", "cancel"} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			pidFile, marker := filepath.Join(dir, "dsh.pid"), filepath.Join(dir, "executed")
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
			script := filepath.Join(dir, "dsh")
			require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$$\" > "+quote(pidFile)+"\nexec "+quote(executable)+" \"$@\"\n"), 0700))
			var mu sync.Mutex
			var requests []json.RawMessage
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "bad request", 400)
					return
				}
				mu.Lock()
				requests = append(requests, body)
				index := len(requests)
				mu.Unlock()
				if index == 1 {
					args, _ := json.Marshal(map[string]string{
						"command":             "printf executed >> " + quote(marker),
						"description":         "Write a test marker only after approval",
						"sandbox_permissions": "danger-full-access",
						"justification":       "Exercise a pending approval with a local test file.",
					})
					writeCompletion(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
						"index": 0, "id": "pending-write", "type": "function",
						"function": map[string]string{"name": "bash", "arguments": string(args)},
					}}}, "tool_calls")
					return
				}
				writeCompletion(w, map[string]string{"role": "assistant", "content": "continued after process loss"}, "stop")
			}))
			defer model.Close()
			cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock","base_url":%q},"runtime":{"control_plane_url":"http://controller:8083"}}`, model.URL+"/v1")))
			require.NoError(t, err)
			dataDir := filepath.Join(dir, "data")
			runner, err := New(cfg, script, dataDir, append(os.Environ(), "OPENAI_API_KEY=mock", "DSH_PERMISSION_MODE=workspace-write"))
			require.NoError(t, err)
			store, err := continuation.New(filepath.Join(dataDir, "adapter"), "dsh-byo", ValidateSessionID)
			require.NoError(t, err)
			executor, err := NewExecutor(runner, store, tracing.RuntimeTelemetry{})
			require.NoError(t, err)
			client, api := newSettlementClient(t, executor, func(task *a2apb.Task) error {
				if task.Status.State == a2apb.TaskState_TASK_STATE_INPUT_REQUIRED {
					return nil
				}
				data, err := os.ReadFile(pidFile)
				if err != nil {
					return err
				}
				pid, err := strconv.Atoi(string(data))
				if err != nil {
					return err
				}
				if syscall.Kill(pid, 0) != syscall.ESRCH {
					return fmt.Errorf("DSH still exists at settlement")
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					return fmt.Errorf("unapproved tool may have executed: %v", err)
				}
				return nil
			})
			t.Cleanup(func() {
				id, err := executor.reservedTask()
				require.NoError(t, err)
				if id != "" {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					req, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{ID: id})
					require.NoError(t, err)
					_, err = client.CancelTask(ctx, req)
					require.NoError(t, err)
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			contextID := uuid.NewString()
			message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Request the guarded marker write."))
			message.ContextID = contextID
			request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: message})
			require.NoError(t, err)
			first, err := client.SendMessage(ctx, request)
			require.NoError(t, err)
			task, err := pbconv.FromProtoTask(first.GetTask())
			require.NoError(t, err)
			require.Equal(t, a2atype.TaskStateInputRequired, task.Status.State)
			select {
			case <-api.settled:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			nativeID, bound, err := store.Load()
			require.NoError(t, err)
			require.True(t, bound)
			data, err := os.ReadFile(pidFile)
			require.NoError(t, err)
			pid, err := strconv.Atoi(string(data))
			require.NoError(t, err)
			require.NoError(t, syscall.Kill(pid, syscall.SIGKILL))
			require.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, 5*time.Second, 10*time.Millisecond)
			want := a2apb.TaskState_TASK_STATE_FAILED
			if action == "cancel" {
				want = a2apb.TaskState_TASK_STATE_CANCELED
				request, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{ID: task.ID})
				require.NoError(t, err)
				result, err := client.CancelTask(ctx, request)
				require.NoError(t, err)
				require.Equal(t, want, result.Status.State)
			} else {
				approval, err := apia2a.ParseToolApprovalRequest(task.Status.Message)
				require.NoError(t, err)
				require.NotNil(t, approval)
				decision := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Approval decision"))
				decision.TaskID, decision.ContextID = task.ID, contextID
				require.NoError(t, apia2a.AttachHITL(decision, apia2a.ToolApprovalResponse{
					Type:      apia2a.HITLTypeToolApprovalResponse,
					Approvals: []apia2a.ToolApproval{{ID: approval.Tools[0].ID, Approved: action == "allow"}},
				}))
				request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: decision})
				require.NoError(t, err)
				result, err := client.SendMessage(ctx, request)
				require.NoError(t, err)
				require.Equal(t, want, result.GetTask().Status.State)
			}
			select {
			case settled := <-api.settled:
				require.Equal(t, want, settled.Status.State)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			message = a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Continue without executing the interrupted tool."))
			message.ContextID = contextID
			request, err = pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: message})
			require.NoError(t, err)
			next, err := client.SendMessage(ctx, request)
			require.NoError(t, err)
			require.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, next.GetTask().Status.State)
			require.NotEqual(t, string(task.ID), next.GetTask().Id)
			require.Equal(t, contextID, next.GetTask().ContextId)
			select {
			case <-api.settled:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			resumedID, _, err := store.Load()
			require.NoError(t, err)
			require.Equal(t, nativeID, resumedID)
			_, err = os.Stat(marker)
			require.True(t, os.IsNotExist(err), "pending tool must never be replayed")
			mu.Lock()
			defer mu.Unlock()
			require.Len(t, requests, 2, "answering the lost approval must not start another model turn")
			require.Contains(t, string(requests[1]), "Request the guarded marker write.")
		})
	}
}
