package e2e_test

import (
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestAgentInstanceLifecycle verifies the synchronous public lifecycle contract
// against a clean cluster, owning both the template and instance it creates.
func TestAgentInstanceLifecycle(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		deleted, err := fixture.instances.DeleteAgentInstance(fixture.ctx, &apiv1alpha1.DeleteAgentInstanceRequest{
			AgentInstanceId: fixture.instanceID,
		})
		if err != nil {
			t.Fatalf("delete AgentInstance: %v", err)
		}
		if deleted.GetAgentInstance().GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED {
			t.Fatalf("deleted AgentInstance state = %s, want DELETED", deleted.GetAgentInstance().GetState())
		}

		_, err = fixture.instances.GetAgentInstance(fixture.ctx, &apiv1alpha1.GetAgentInstanceRequest{
			AgentInstanceId: fixture.instanceID,
		})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("get deleted AgentInstance error = %v, want NotFound", err)
		}
	})
}
