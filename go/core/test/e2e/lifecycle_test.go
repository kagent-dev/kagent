package e2e_test

import (
	"testing"

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
