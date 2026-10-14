//go:build linux

package bridge

import (
	"context"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

func TestLostExecutionOwnerFailsWithoutChangingStoredTask(t *testing.T) {
	for _, state := range []a2atype.TaskState{a2atype.TaskStateWorking, a2atype.TaskStateInputRequired} {
		t.Run(string(state), func(t *testing.T) {
			runner := testRunner(t, "")
			store, err := continuation.New(t.TempDir(), "dsh-byo", ValidateSessionID)
			require.NoError(t, err)
			executor, err := NewExecutor(runner, store, tracing.RuntimeTelemetry{})
			require.NoError(t, err)
			client, api := newSettlementClient(t, executor, nil)
			task := &a2atype.Task{ID: a2atype.TaskID(uuid.NewString()), ContextID: uuid.NewString(), Status: a2atype.TaskStatus{State: state}}
			if state == a2atype.TaskStateInputRequired {
				task.Status.Message = a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Allow tool?"))
				require.NoError(t, apia2a.AttachHITL(task.Status.Message, apia2a.ToolApprovalRequest{
					Type:  apia2a.HITLTypeToolApprovalRequest,
					Tools: []apia2a.HITLTool{{ID: "approval-1", CallID: "tool-1", Name: "write"}},
				}))
			}
			persisted, err := pbconv.ToProtoTask(task)
			require.NoError(t, err)
			// Simulate controller state surviving the loss of the runtime owner.
			_, err = api.CreateTask(t.Context(), &apiv1alpha1.TaskStoreServiceCreateTaskRequest{Task: persisted})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			for range 2 {
				if state == a2atype.TaskStateWorking {
					req, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{ID: task.ID})
					require.NoError(t, err)
					_, err = client.CancelTask(ctx, req)
					require.ErrorContains(t, err, "DSH task has no live execution owner")
				} else {
					decision := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Approval decision"))
					decision.TaskID, decision.ContextID = task.ID, task.ContextID
					require.NoError(t, apia2a.AttachHITL(decision, apia2a.ToolApprovalResponse{
						Type:      apia2a.HITLTypeToolApprovalResponse,
						Approvals: []apia2a.ToolApproval{{ID: "approval-1", Approved: true}},
					}))
					req, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: decision})
					require.NoError(t, err)
					_, err = client.SendMessage(ctx, req)
					require.ErrorContains(t, err, "DSH task has no live continuation owner")
				}
				require.NoError(t, ctx.Err(), "request must fail before its deadline")
			}
			get, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: task.ID})
			require.NoError(t, err)
			got, err := client.GetTask(ctx, get)
			require.NoError(t, err)
			require.True(t, proto.Equal(persisted, got), "do not append a decision or fabricate a task transition")
			api.mu.Lock()
			require.Equal(t, int64(1), api.tasks[string(task.ID)].Version)
			require.Empty(t, api.settled, "unknown cleanup must never be settled")
			api.mu.Unlock()
			if state == a2atype.TaskStateInputRequired {
				reply := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("continue"))
				reply.TaskID, reply.ContextID = task.ID, task.ContextID
				req, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: reply})
				require.NoError(t, err)
				_, err = client.SendMessage(ctx, req)
				require.ErrorContains(t, err, "DSH task has no live continuation owner")
				got, err = client.GetTask(ctx, get)
				require.NoError(t, err)
				require.True(t, proto.Equal(persisted, got), "a plain reply must not change the waiting task")
				api.mu.Lock()
				defer api.mu.Unlock()
				require.Equal(t, int64(1), api.tasks[string(task.ID)].Version, "reject plain replies before the SDK appends history")
				require.Empty(t, api.settled)
			}
		})
	}
}

func TestLiveApprovalThroughDSHExecutor(t *testing.T) {
	for _, action := range []string{"allow", "reject", "cancel"} {
		t.Run(action, func(t *testing.T) {
			runner := testRunner(t, "approval")
			store, err := continuation.New(t.TempDir(), "dsh-byo", ValidateSessionID)
			require.NoError(t, err)
			executor, err := NewExecutor(runner, store, tracing.RuntimeTelemetry{})
			require.NoError(t, err)
			client, api := newSettlementClient(t, executor, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			msg := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("work"))
			request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: msg})
			require.NoError(t, err)
			first, err := client.SendMessage(ctx, request)
			require.NoError(t, err)
			task := first.GetTask()
			require.NotNil(t, task)
			require.Equal(t, a2apb.TaskState_TASK_STATE_INPUT_REQUIRED, task.Status.State)
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
				defer stop()
				req, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{ID: a2atype.TaskID(task.Id)})
				require.NoError(t, err)
				// A completed task is already cleaned; only cancel a parked test turn.
				if executor.ReservedTaskID() != "" {
					_, err = client.CancelTask(cleanup, req)
					require.NoError(t, err)
				}
			})
			select {
			case <-api.settled:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			req, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{ID: a2atype.TaskID(task.Id)})
			require.NoError(t, err)
			if action == "cancel" {
				canceled, err := client.CancelTask(ctx, req)
				require.NoError(t, err)
				require.Equal(t, a2apb.TaskState_TASK_STATE_CANCELED, canceled.Status.State)
			} else {
				nativeTask, err := pbconv.FromProtoTask(task)
				require.NoError(t, err)
				approval, err := apia2a.ParseToolApprovalRequest(nativeTask.Status.Message)
				require.NoError(t, err)
				require.NotNil(t, approval)
				decision := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Approval decision"))
				decision.TaskID, decision.ContextID = nativeTask.ID, nativeTask.ContextID
				require.NoError(t, apia2a.AttachHITL(decision, apia2a.ToolApprovalResponse{
					Type:      apia2a.HITLTypeToolApprovalResponse,
					Approvals: []apia2a.ToolApproval{{ID: approval.Tools[0].ID, Approved: action == "allow"}},
				}))
				request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: decision})
				require.NoError(t, err)
				completed, err := client.SendMessage(ctx, request)
				require.NoError(t, err)
				require.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, completed.GetTask().Status.State)
			}
			select {
			case <-api.settled:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			_, err = client.CancelTask(ctx, req)
			require.NotEqual(t, codes.DeadlineExceeded, status.Code(err))
		})
	}
}
