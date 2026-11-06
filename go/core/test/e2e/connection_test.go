package e2e_test

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func newControllerConn(t *testing.T, target string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.NoError(t, waitForControllerAPI(t.Context(), conn), "controller API %s did not become reachable", target)
	return conn
}

func waitForControllerAPI(ctx context.Context, conn *grpc.ClientConn) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// NewClient connects lazily. A fresh connection can time out while Service
	// routing settles after a controller rollout, even if a previous probe passed.
	// Wait only on this read-only probe; mutations keep their normal retry contract.
	response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{}, grpc.WaitForReady(true))
	if err != nil {
		return err
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("controller gRPC health is %s", response.GetStatus())
	}
	return nil
}

func TestControllerConnectionReadiness(t *testing.T) {
	for _, test := range []struct {
		name        string
		failedDials int32
		serverCode  codes.Code
		health      grpc_health_v1.HealthCheckResponse_ServingStatus
		wantCode    codes.Code
		wantCalls   int32
		wantError   string
	}{
		{name: "healthy", health: grpc_health_v1.HealthCheckResponse_SERVING, wantCalls: 1},
		{name: "first dial times out", failedDials: 1, health: grpc_health_v1.HealthCheckResponse_SERVING, wantCalls: 1},
		{name: "unreachable until deadline", failedDials: -1, wantCode: codes.DeadlineExceeded},
		{name: "server error is not retried", serverCode: codes.Unavailable, wantCode: codes.Unavailable, wantCalls: 1},
		{name: "not serving", health: grpc_health_v1.HealthCheckResponse_NOT_SERVING, wantCode: codes.Unknown, wantCalls: 1, wantError: "controller gRPC health is NOT_SERVING"},
		{name: "unknown health", health: grpc_health_v1.HealthCheckResponse_UNKNOWN, wantCode: codes.Unknown, wantCalls: 1, wantError: "controller gRPC health is UNKNOWN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			service := &readinessHealthServer{code: test.serverCode, health: test.health}
			grpc_health_v1.RegisterHealthServer(server, service)
			serveErr := make(chan error, 1)
			go func() { serveErr <- server.Serve(listener) }()
			t.Cleanup(func() {
				server.Stop()
				require.NoError(t, <-serveErr)
			})

			var dials atomic.Int32
			retryBackoff := backoff.DefaultConfig
			retryBackoff.BaseDelay = 10 * time.Millisecond
			retryBackoff.MaxDelay = 10 * time.Millisecond
			conn, err := grpc.NewClient("passthrough:///controller",
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithConnectParams(grpc.ConnectParams{Backoff: retryBackoff, MinConnectTimeout: time.Second}),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
					attempt := dials.Add(1)
					if test.failedDials < 0 || attempt <= test.failedDials {
						return nil, &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}
					}
					return listener.DialContext(ctx)
				}),
			)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			timeout := 5 * time.Second
			if test.wantCode == codes.DeadlineExceeded {
				timeout = time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			err = waitForControllerAPI(ctx, conn)
			require.Equal(t, test.wantCode, status.Code(err), "%v", err)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			}
			require.Equal(t, test.wantCalls, service.calls.Load())
			if test.failedDials >= 0 {
				require.Equal(t, test.failedDials+1, dials.Load())
			}
		})
	}
}

type readinessHealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	code   codes.Code
	health grpc_health_v1.HealthCheckResponse_ServingStatus
	calls  atomic.Int32
}

var _ grpc_health_v1.HealthServer = (*readinessHealthServer)(nil)

func (s *readinessHealthServer) Check(context.Context, *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	s.calls.Add(1)
	return &grpc_health_v1.HealthCheckResponse{Status: s.health}, status.Error(s.code, "controller unavailable")
}

// restartTestController replaces the single test controller and waits for
// its public API. Call only from sequential tests with an endpoint that survives
// pod replacement.
func restartTestController(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	kube := interactionKubeClient(t)
	pods := &corev1.PodList{}
	require.NoError(t, kube.List(ctx, pods, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	require.Len(t, pods.Items, 1, "restart test requires a single controller replica")
	restartControllerPod(t, kube, pods.Items[0])
}

func restartControllerPod(t *testing.T, kube ctrlclient.Client, old corev1.Pod) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	require.NoError(t, kube.Delete(ctx, &old))
	require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
		current := &corev1.PodList{}
		if err := kube.List(ctx, current, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}); err != nil {
			return false, err
		}
		for _, pod := range current.Items {
			if pod.UID == old.UID {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	}), "wait for replacement controller")
	newControllerConn(t, interactionTarget(t))
}
