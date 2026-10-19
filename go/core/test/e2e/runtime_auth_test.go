package e2e_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
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
		runtimeConn := runtimeControllerConn(t, target)
		private := runtimev1.NewTaskStoreServiceClient(runtimeConn)
		memory := runtimev1.NewMemoryServiceClient(runtimeConn)
		request := &runtimev1.TaskStoreServiceGetTaskRequest{SessionId: fixture.sessionID, TaskId: string(task.ID)}
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
				require.Equal(t, codes.Unauthenticated, status.Code(err))
				_, err = memory.List(ctx, &runtimev1.MemoryServiceListRequest{AgentName: "forged", UserId: "forged"})
				require.Equal(t, codes.Unauthenticated, status.Code(err))
			})
		}
		_, err = apiv1.NewSessionServiceClient(runtimeConn).ListSessions(t.Context(), &apiv1.ListSessionsRequest{})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	})
}

func runtimeControllerConn(t *testing.T, target string) *grpc.ClientConn {
	t.Helper()
	services := &corev1.ServiceList{}
	require.NoError(t, interactionKubeClient(t).List(t.Context(), services, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	port := int32(0)
	for _, service := range services.Items {
		for _, candidate := range service.Spec.Ports {
			if candidate.Name == "runtime" {
				port = candidate.Port
			}
		}
	}
	require.NotZero(t, port, "controller must expose its actor-only runtime service port")
	host, _, err := net.SplitHostPort(target)
	require.NoError(t, err)
	conn, err := grpc.NewClient(net.JoinHostPort(host, strconv.Itoa(int(port))), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	_, err = runtimev1.NewMemoryServiceClient(conn).List(ctx, &runtimev1.MemoryServiceListRequest{}, grpc.WaitForReady(true))
	require.Equal(t, codes.Unauthenticated, status.Code(err), "runtime listener must be reachable and require authentication")
	return conn
}
