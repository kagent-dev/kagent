//go:build linux

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// Replaces the entire runtime without copying pending handles. Only DATA and
// the controller task-store fixture survive. This is not a Substrate test.
type replaceableDSHExecutor struct {
	a2asrv.PassthroughCallInterceptor
	current atomic.Pointer[Executor]
}

func (e *replaceableDSHExecutor) Execute(ctx context.Context, req *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return e.current.Load().Execute(ctx, req)
}
func (e *replaceableDSHExecutor) Cancel(ctx context.Context, req *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return e.current.Load().Cancel(ctx, req)
}
func (e *replaceableDSHExecutor) Before(ctx context.Context, call *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	return e.current.Load().Before(ctx, call, req)
}

func TestRealDSHProtectedMCPApproval(t *testing.T) {
	executable := os.Getenv("DSH_TEST_BIN")
	if executable == "" {
		t.Skip("DSH_TEST_BIN required")
	}
	for _, mode := range []string{"live", "waiting", "result"} {
		for _, action := range []string{"allow", "deny", "cancel", "cancel-dispatch", "cancel-continuation"} {
			if mode == "result" && action != "allow" {
				continue
			}
			t.Run(mode+"/"+action, func(t *testing.T) {
				t.Parallel() // Distinct Actors must not share scope, effects or native state.
				restart := mode != "live"
				var effects, modelCalls atomic.Int32
				dispatched := make(chan struct{})
				continued := make(chan struct{})
				release := make(chan struct{})
				remote := mcp.NewServer(&mcp.Implementation{Name: "protected", Version: "1"}, nil)
				mcp.AddTool(remote, &mcp.Tool{Name: "deploy"}, func(_ context.Context, _ *mcp.CallToolRequest, args struct {
					Version string `json:"version"`
				}) (*mcp.CallToolResult, map[string]string, error) {
					assert.Equal(t, "version-7", args.Version)
					effects.Add(1)
					if action == "cancel-dispatch" {
						close(dispatched)
						<-release // Deliberately ignore HTTP cancellation like a remote effect can.
					}
					return nil, map[string]string{"receipt": "deployment-receipt-7"}, nil
				})
				upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
				defer func() { close(release); upstream.Close() }()
				model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var input struct {
						Tools []struct {
							Function struct {
								Name string `json:"name"`
							} `json:"function"`
						} `json:"tools"`
						Messages json.RawMessage `json:"messages"`
					}
					if json.NewDecoder(r.Body).Decode(&input) != nil {
						http.Error(w, "bad request", 400)
						return
					}
					if modelCalls.Add(1) == 1 {
						name := ""
						for _, tool := range input.Tools {
							if strings.HasSuffix(tool.Function.Name, "__deploy") {
								name = tool.Function.Name
							}
						}
						assert.NotEmpty(t, name)
						writeCompletion(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "deploy-call", "type": "function", "function": map[string]string{"name": name, "arguments": `{"version":"version-7"}`}}}}, "tool_calls")
					} else {
						if action == "allow" || action == "cancel-continuation" {
							assert.Contains(t, string(input.Messages), "deployment-receipt-7")
						} else {
							assert.Contains(t, string(input.Messages), "Tool call denied")
						}
						if action == "cancel-continuation" {
							close(continued)
							<-release
						}
						writeCompletion(w, map[string]string{"role": "assistant", "content": "Deployment decision handled."}, "stop")
					}
				}))
				// Release blocked fixture handlers before waiting for HTTP shutdown.
				defer model.CloseClientConnections()
				t.Cleanup(model.Close)
				cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock","base_url":%q},"runtime":{"control_plane_url":"http://controller:8083"},"http_tools":[{"params":{"url":%q},"require_approval":true}]}`, model.URL+"/v1", upstream.URL)))
				require.NoError(t, err)
				directory := t.TempDir()
				var runner *Runner
				newExecutor := func() *Executor {
					var err error
					runner, err = New(cfg, executable, directory, append(os.Environ(), "OPENAI_API_KEY=mock"))
					require.NoError(t, err)
					store, err := continuation.New(filepath.Join(directory, "adapter"), "dsh-byo", ValidateSessionID)
					require.NoError(t, err)
					executor, err := NewExecutor(runner, store, tracing.RuntimeTelemetry{})
					require.NoError(t, err)
					return executor
				}
				switcher := &replaceableDSHExecutor{}
				switcher.current.Store(newExecutor())
				if restart {
					if _, ok := any(switcher.current.Load().Executor).(interface {
						ReservedTaskID() (a2atype.TaskID, error)
					}); !ok {
						t.Skip("DATA-only public task restoration requires shared kagent PR #3165")
					}
				}
				client, api := newSettlementClient(t, switcher, nil)
				ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
				defer cancel()
				msg := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Deploy version-7."))
				msg.ContextID = uuid.NewString()
				request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: msg})
				require.NoError(t, err)
				first, err := client.SendMessage(ctx, request)
				require.NoError(t, err)
				task := first.GetTask()
				require.NotNil(t, task)
				require.Equal(t, a2apb.TaskState_TASK_STATE_INPUT_REQUIRED, task.Status.State, "%v", task)
				require.Zero(t, effects.Load())
				select {
				case <-api.settled:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				saved, err := runner.loadApproval()
				require.NoError(t, err)
				require.Equal(t, approvalOwner{TaskID: task.Id, ContextID: task.ContextId}, saved.Owner)
				if mode == "result" {
					_, _, pending, err := runner.RestorePending()
					require.NoError(t, err)
					runner.executable = "/missing-test-dsh"
					out, err := pending.Resume(context.WithValue(ctx, approvalOwnerKey{}, saved.Owner), &runtime.ApprovalDecision{ID: saved.Request.ID, Approved: true}, &recordingSink{})
					require.NoError(t, err)
					require.NotNil(t, out.Pending)
					require.Equal(t, int32(1), effects.Load())
				}
				if restart {
					switcher.current.Store(newExecutor())
				}
				get, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: a2atype.TaskID(task.Id)})
				require.NoError(t, err)
				// A foreign conversation must be rejected before SDK history writes.
				foreign := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("continue"))
				foreign.TaskID, foreign.ContextID = a2atype.TaskID(task.Id), uuid.NewString()
				bad, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: foreign})
				require.NoError(t, err)
				_, err = client.SendMessage(ctx, bad)
				require.Error(t, err)
				unchanged, err := client.GetTask(ctx, get)
				require.NoError(t, err)
				require.True(t, proto.Equal(task, unchanged))
				wrong := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Wrong decision"))
				wrong.TaskID, wrong.ContextID = a2atype.TaskID(task.Id), task.ContextId
				require.NoError(t, apia2a.AttachHITL(wrong, apia2a.ToolApprovalResponse{Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{{ID: "foreign-approval", Approved: true}}}))
				bad, err = pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: wrong})
				require.NoError(t, err)
				_, err = client.SendMessage(ctx, bad)
				require.Error(t, err)
				unchanged, err = client.GetTask(ctx, get)
				require.NoError(t, err)
				require.True(t, proto.Equal(task, unchanged), "wrong approval must not append task history")
				if action == "cancel" {
					req, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{ID: a2atype.TaskID(task.Id)})
					require.NoError(t, err)
					canceled, err := client.CancelTask(ctx, req)
					require.NoError(t, err)
					require.Equal(t, a2apb.TaskState_TASK_STATE_CANCELED, canceled.Status.State)
				} else {
					native, err := pbconv.FromProtoTask(task)
					require.NoError(t, err)
					approval, err := apia2a.ParseToolApprovalRequest(native.Status.Message)
					require.NoError(t, err)
					require.NotNil(t, approval)
					decision := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Approval decision"))
					decision.TaskID, decision.ContextID = native.ID, native.ContextID
					require.NoError(t, apia2a.AttachHITL(decision, apia2a.ToolApprovalResponse{Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{{ID: approval.Tools[0].ID, Approved: action == "allow" || action == "cancel-dispatch" || action == "cancel-continuation"}}}))
					req, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: decision})
					require.NoError(t, err)
					if action == "cancel-dispatch" || action == "cancel-continuation" {
						_, err := client.SendStreamingMessage(ctx, req)
						require.NoError(t, err)
						started := dispatched
						if action == "cancel-continuation" {
							started = continued
						}
						select {
						case <-started:
						case <-ctx.Done():
							t.Fatal(ctx.Err())
						}
						cancelReq, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{ID: native.ID})
						require.NoError(t, err)
						canceled, err := client.CancelTask(ctx, cancelReq)
						if action == "cancel-dispatch" {
							require.ErrorContains(t, err, "uncertain")
						} else {
							require.NoError(t, err)
							require.Equal(t, a2apb.TaskState_TASK_STATE_CANCELED, canceled.Status.State)
						}
						require.Equal(t, int32(1), effects.Load())
						_, _, pending, err := runner.RestorePending()
						if action == "cancel-dispatch" {
							require.ErrorContains(t, err, "uncertain")
						} else {
							require.NoError(t, err)
						}
						require.Nil(t, pending)
						return
					}
					completed, err := client.SendMessage(ctx, req)
					require.NoError(t, err)
					require.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, completed.GetTask().Status.State, "%v", completed)
					require.Equal(t, task.Id, completed.GetTask().Id)
					require.Equal(t, task.ContextId, completed.GetTask().ContextId)
					_, err = client.SendMessage(ctx, req)
					require.Error(t, err, "late decision must not execute again")
				}
				want := int32(0)
				if action == "allow" {
					want = 1
				}
				require.Equal(t, want, effects.Load())
				_, _, pending, err := runner.RestorePending()
				require.NoError(t, err)
				require.Nil(t, pending)
			})
		}
	}
}
