package e2e_test

import (
	"context"
	"testing"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// Delete a real runtime while its model request is blocked. The authenticated
// TaskStore fault injection is covered by TestGoADKDeletionAfterSettlementOutage;
// callers outside the Actor cannot stage runtime boundaries with unsigned headers.
func TestSessionDeletionStopsLiveRuntime(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		for _, loseReply := range []bool{false, true} {
			name := "normal_reply"
			if loseReply {
				name = "lost_reply"
			}
			t.Run(name, func(t *testing.T) {
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
				public, err := fixture.client.GetTask(fixture.ctx, &a2apb.GetTaskRequest{Tenant: fixture.tenant, Id: taskID})
				require.NoError(t, err)
				require.Equal(t, a2apb.TaskState_TASK_STATE_WORKING, public.Status.State)
				remove := &apiv1.DeleteSessionRequest{SessionId: fixture.sessionID}
				if loseReply {
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
				_, send := newMessageRequest(t, "Do not recreate deleted compute")
				send.Tenant, send.Message.ContextId = fixture.tenant, fixture.contextID
				_, err = fixture.client.SendMessage(fixture.ctx, send)
				require.Equal(t, codes.PermissionDenied, status.Code(err), "deleted contexts no longer authorize execution")
				actor, err = findSubstrateActor(fixture.ctx, fixture.system, "kagent", substrate.ActorName(fixture.sessionID))
				require.NoError(t, err)
				require.Nil(t, actor, "rejected sends must not recreate the Actor")
				t.Logf("live %s Actor deleted; subsequent sends rejected (lost reply: %t)", harness.runtimeLabel, loseReply)
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
