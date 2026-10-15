package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// Stage the cleanup gap through TaskStore while a real runtime is blocked at its
// model request. No settlement acknowledgement is sent. Explicit deletion must
// stop and remove that Actor even though the public task is still WORKING.
func TestSessionUnpublishedBoundaryDeletion(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		for _, boundary := range []a2apb.TaskState{a2apb.TaskState_TASK_STATE_COMPLETED, a2apb.TaskState_TASK_STATE_INPUT_REQUIRED} {
			t.Run(boundary.String(), func(t *testing.T) {
				target := interactionTarget(t)
				modelURL, started := startBlockingInteractionMock(t)
				fixture := newInteractionFixture(t, harness, target, modelURL)
				_, request := newMessageRequest(t, "Wait for cancellation")
				request.Tenant, request.Message.ContextId = fixture.tenant, fixture.contextID
				stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
				require.NoError(t, err)
				select {
				case <-started:
				case <-time.After(time.Minute):
					t.Fatal("runtime did not start its model request")
				}
				var taskID string
				for taskID == "" {
					event, err := stream.Recv()
					require.NoError(t, err)
					taskID = event.GetTask().GetId()
				}
				actor, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", substrate.ActorName(fixture.sessionID))
				require.NoError(t, err)
				require.NotNil(t, actor)
				require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RUNNING, actor.GetStatus().GetState())
				privateCtx := metadata.NewOutgoingContext(fixture.ctx, metadata.Pairs(apia2a.InsecureRuntimeIdentityHeader,
					fmt.Sprintf("kagent/%s/%s", actor.GetMetadata().GetName(), actor.GetMetadata().GetUid())))
				store := runtimev1.NewTaskStoreServiceClient(newControllerConn(t, target))
				stored, err := store.GetTask(privateCtx, &runtimev1.TaskStoreServiceGetTaskRequest{SessionId: fixture.sessionID, TaskId: taskID})
				require.NoError(t, err)
				task := proto.CloneOf(stored.Stored.Task)
				task.Status = &a2apb.TaskStatus{State: boundary}
				saved, err := store.UpdateTask(privateCtx, &runtimev1.TaskStoreServiceUpdateTaskRequest{
					SessionId: fixture.sessionID, Task: task, ExpectedVersion: stored.Stored.Version,
				})
				require.NoError(t, err)
				public, err := fixture.client.GetTask(fixture.ctx, &a2apb.GetTaskRequest{Tenant: fixture.tenant, Id: taskID})
				require.NoError(t, err)
				require.Equal(t, a2apb.TaskState_TASK_STATE_WORKING, public.Status.State)
				remove := &apiv1.DeleteSessionRequest{SessionId: fixture.sessionID}
				if boundary == a2apb.TaskState_TASK_STATE_INPUT_REQUIRED {
					// Lose the public reply after the real backend deletion commits.
					// A fresh retry observes NotFound and cannot recreate compute.
					loss := &lostDeleteReplyConn{ClientConnInterface: newControllerConn(t, target)}
					_, err = apiv1.NewSessionServiceClient(loss).DeleteSession(fixture.ctx, remove)
					require.True(t, loss.lost)
					require.Equal(t, codes.Unavailable, status.Code(err))
					_, err = fixture.sessions.DeleteSession(fixture.ctx, remove)
					require.Equal(t, codes.NotFound, status.Code(err))
				} else {
					deleted, err := fixture.sessions.DeleteSession(fixture.ctx, remove)
					require.NoError(t, err)
					require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.Session.State)
				}
				actor, err = findSubstrateActor(fixture.ctx, fixture.system, "kagent", substrate.ActorName(fixture.sessionID))
				require.NoError(t, err)
				require.Nil(t, actor)
				_, err = fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
				require.Equal(t, codes.NotFound, status.Code(err))
				_, err = store.SettleTask(privateCtx, &runtimev1.TaskStoreServiceSettleTaskRequest{SessionId: fixture.sessionID, TaskId: taskID, Version: saved.Version})
				require.Equal(t, codes.NotFound, status.Code(err), "deleted runtime authority cannot publish the staged boundary")
				task.Status.State = a2apb.TaskState_TASK_STATE_WORKING
				_, err = store.UpdateTask(privateCtx, &runtimev1.TaskStoreServiceUpdateTaskRequest{SessionId: fixture.sessionID, Task: task, ExpectedVersion: saved.Version})
				require.Equal(t, codes.NotFound, status.Code(err))
				_, send := newMessageRequest(t, "Do not recreate deleted compute")
				send.Tenant, send.Message.ContextId = fixture.tenant, fixture.contextID
				_, err = fixture.client.SendMessage(fixture.ctx, send)
				require.Equal(t, codes.PermissionDenied, status.Code(err), "deleted contexts no longer authorize execution")
				actor, err = findSubstrateActor(fixture.ctx, fixture.system, "kagent", substrate.ActorName(fixture.sessionID))
				require.NoError(t, err)
				require.Nil(t, actor, "rejected sends must not recreate the Actor")
				t.Logf("unpublished %s deleted with its live %s Actor; stale writes rejected", boundary, harness.runtimeLabel)
			})
		}
	})
}

type lostDeleteReplyConn struct {
	grpc.ClientConnInterface
	lost bool
}

var _ grpc.ClientConnInterface = (*lostDeleteReplyConn)(nil)

func (c *lostDeleteReplyConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	err := c.ClientConnInterface.Invoke(ctx, method, args, reply, opts...)
	if err == nil && method == apiv1.SessionService_DeleteSession_FullMethodName && !c.lost {
		c.lost = true
		return status.Error(codes.Unavailable, "injected loss of a committed deletion reply")
	}
	return err
}
