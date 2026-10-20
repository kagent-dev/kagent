package e2e_test

import (
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestSessionSharesAreReadOnly(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		_, _, task := fixture.send(t, "What is 2+2?")
		created, err := fixture.sessions.CreateSessionShare(fixture.ctx, &apiv1.CreateSessionShareRequest{
			SessionId: fixture.sessionID,
		})
		require.NoError(t, err)
		require.NotEmpty(t, created.Token)
		visitor := metadata.NewOutgoingContext(fixture.ctx, metadata.Pairs("x-user-id", "share-reader", "x-share-token", created.Token))
		read, err := fixture.sessions.GetSession(visitor, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
		require.NoError(t, err)
		require.Equal(t, fixture.sessionID, read.Session.Id)
		_, err = fixture.client.GetTask(visitor, &a2apb.GetTaskRequest{Tenant: fixture.tenant, Id: string(task.ID)})
		require.NoError(t, err)

		_, request := newMessageRequest(t, "What is 2+2?")
		request.Tenant = fixture.tenant
		request.Message.ContextId = fixture.sessionID
		for _, mutation := range []struct {
			name string
			call func() error
		}{
			{name: "send", call: func() error {
				_, err := fixture.client.SendMessage(visitor, request)
				return err
			}},
			{name: "stream", call: func() error {
				stream, err := fixture.client.SendStreamingMessage(visitor, request)
				if err != nil {
					return err
				}
				_, err = stream.Recv()
				return err
			}},
			{name: "cancel", call: func() error {
				_, err := fixture.client.CancelTask(visitor, &a2apb.CancelTaskRequest{Tenant: fixture.tenant, Id: string(task.ID)})
				return err
			}},
			{name: "rename", call: func() error {
				_, err := fixture.sessions.UpdateSessionName(visitor, &apiv1.UpdateSessionNameRequest{SessionId: fixture.sessionID, Name: "changed"})
				return err
			}},
			{name: "suspend", call: func() error {
				_, err := fixture.sessions.SuspendSession(visitor, &apiv1.SuspendSessionRequest{SessionId: fixture.sessionID})
				return err
			}},
		} {
			t.Run(mutation.name, func(t *testing.T) {
				require.Equal(t, codes.PermissionDenied, status.Code(mutation.call()))
			})
		}
		_, err = fixture.sessions.RevokeSessionShare(fixture.ctx, &apiv1.RevokeSessionShareRequest{ShareId: created.Share.Id})
		require.NoError(t, err)
		_, err = fixture.sessions.GetSession(visitor, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	})
}
