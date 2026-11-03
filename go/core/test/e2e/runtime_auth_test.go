package e2e_test

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

// Runtime writes traverse real Substrate JWT injection. Direct callers cannot
// acquire the same authority by supplying actor metadata or public credentials.
func TestRuntimeActorJWTAuthentication(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		target := interactionTarget(t)
		fixture := newInteractionFixture(t, harness, target, startInteractionMock(t))
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		stored, err := fixture.client.GetTask(fixture.ctx, &a2apb.GetTaskRequest{Tenant: fixture.tenant, Id: string(task.ID)})
		require.NoError(t, err)
		require.Equal(t, string(task.ID), stored.GetId(), "the Actor must persist through authenticated runtime RPCs")
		runtimeConn := newControllerConn(t, target)
		private := runtimev1.NewTaskStoreServiceClient(runtimeConn)
		memory := runtimev1.NewMemoryServiceClient(runtimeConn)
		request := &runtimev1.TaskStoreServiceGetTaskRequest{SessionId: fixture.sessionID, TaskId: string(task.ID)}
		// The E2E installation uses insecure user auth. These callers become
		// users, but cannot acquire the actor identity required by runtime RPCs.
		for _, test := range []struct {
			name    string
			headers []string
		}{
			{"missing", nil},
			{"unsigned identity", []string{"x-kagent-insecure-runtime-identity", "kagent/session-" + fixture.sessionID + "/forged-uid"}},
			{"public identity", []string{"x-user-id", "e2e", "x-agent-name", fixture.sessionID}},
			{"uninjected placeholder", []string{"authorization", "Bearer placeholder"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(test.headers...))
				_, err := private.GetTask(ctx, request)
				require.Equal(t, codes.PermissionDenied, status.Code(err), "%v", err)
				_, err = memory.Search(ctx, &runtimev1.MemoryServiceSearchRequest{})
				require.Equal(t, codes.PermissionDenied, status.Code(err), "%v", err)
			})
		}
	})
}
