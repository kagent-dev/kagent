package e2e_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestAgentInstanceLifecycle verifies the synchronous public lifecycle contract
// against a clean cluster. The cluster installation owns the fixed kagent/smoke
// Harness and AgentTemplate fixtures; this test owns only the AgentInstance it
// creates through the public API.
func TestE2EAgentInstanceLifecycle(t *testing.T) {
	target := os.Getenv("KAGENT_E2E_GRPC_TARGET")
	if target == "" {
		target = os.Getenv("KAGENT_GRPC_URL")
	}
	if target == "" {
		t.Fatalf("KAGENT_E2E_GRPC_TARGET or KAGENT_GRPC_URL must be set to run e2e tests.\n" +
			"To run e2e tests locally:\n" +
			"  1. Create a Kind cluster: make create-kind-cluster\n" +
			"  2. Install Substrate and Kagent (see CI workflow in .github/workflows/ci.yaml)\n" +
			"  3. Set the gRPC target:\n" +
			"     export KAGENT_E2E_GRPC_TARGET=<controller-address>:8084\n" +
			"  4. Run tests: go test -v ./core/test/e2e/...\n" +
			"See go/core/test/e2e/README.md for full instructions.")
	}

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("connect to kagent gRPC API: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 3*time.Minute)
	defer cancel()
	client := apiv1alpha1.NewAgentInstanceServiceClient(conn)

	created, err := client.CreateAgentInstance(ctx, &apiv1alpha1.CreateAgentInstanceRequest{
		Namespace: "kagent", AgentTemplate: "smoke", Harness: "kagent", RequestId: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create AgentInstance: %v", err)
	}
	instance := created.GetAgentInstance()
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		t.Fatalf("created AgentInstance state = %s, want READY", instance.GetState())
	}

	deleted, err := client.DeleteAgentInstance(ctx, &apiv1alpha1.DeleteAgentInstanceRequest{
		Namespace: "kagent", AgentInstanceId: instance.GetId(),
	})
	if err != nil {
		t.Fatalf("delete AgentInstance: %v", err)
	}
	if deleted.GetAgentInstance().GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED {
		t.Fatalf("deleted AgentInstance state = %s, want DELETED", deleted.GetAgentInstance().GetState())
	}

	_, err = client.GetAgentInstance(ctx, &apiv1alpha1.GetAgentInstanceRequest{
		Namespace: "kagent", AgentInstanceId: instance.GetId(),
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("get deleted AgentInstance error = %v, want NotFound", err)
	}
}
