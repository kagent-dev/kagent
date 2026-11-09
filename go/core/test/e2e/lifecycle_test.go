package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// TestSessionLifecycle verifies the synchronous public lifecycle contract
// against a clean cluster, owning both the template and session it creates.
func TestSessionLifecycle(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		deleted, err := fixture.sessions.DeleteSession(fixture.ctx, &apiv1.DeleteSessionRequest{
			SessionId: fixture.sessionID,
		})
		if err != nil {
			t.Fatalf("delete Session: %v", err)
		}
		if deleted.GetSession().GetState() != apiv1.RuntimeState_RUNTIME_STATE_DELETED {
			t.Fatalf("deleted Session state = %s, want DELETED", deleted.GetSession().GetState())
		}

		_, err = fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{
			SessionId: fixture.sessionID,
		})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("get deleted Session error = %v, want NotFound", err)
		}
	})
}

func TestSessionDeletionDuringActiveTurn(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		modelURL, started := startBlockingInteractionMock(t)
		fixture := newInteractionFixture(t, harness, interactionTarget(t), modelURL)
		ctx, cancel := context.WithCancel(fixture.ctx)
		defer cancel()
		_, request := newMessageRequest(t, "Keep working until the Session is deleted")
		request.Tenant, request.Message.ContextId = fixture.tenant, fixture.sessionID
		_, err := fixture.client.SendStreamingMessage(ctx, request)
		require.NoError(t, err)
		select {
		case <-started:
		case <-time.After(time.Minute):
			t.Fatal("runtime did not start its turn")
		}
		_, err = fixture.sessions.DeleteSession(fixture.ctx, &apiv1.DeleteSessionRequest{SessionId: fixture.sessionID})
		require.Equal(t, codes.Aborted, status.Code(err), "ordinary deletion must return an error during an active turn")
		current, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
		require.NoError(t, err)
		require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.GetSession().GetState())
		deleted, err := fixture.sessions.DeleteSession(fixture.ctx, &apiv1.DeleteSessionRequest{SessionId: fixture.sessionID, Force: true})
		require.NoError(t, err)
		require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.GetSession().GetState())
		_, err = fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
		require.Equal(t, codes.NotFound, status.Code(err))
	})
}
