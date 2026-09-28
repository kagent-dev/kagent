package e2e_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
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
	_, err := apiv1alpha1.NewSystemServiceClient(conn).GetVersion(ctx, &apiv1alpha1.GetVersionRequest{}, grpc.WaitForReady(true))
	return err
}

func TestControllerConnectionReadiness(t *testing.T) {
	for _, test := range []struct {
		name        string
		failedDials int32
		serverCode  codes.Code
		wantCode    codes.Code
		wantCalls   int32
	}{
		{name: "healthy", wantCalls: 1},
		{name: "first dial times out", failedDials: 1, wantCalls: 1},
		{name: "unreachable until deadline", failedDials: -1, wantCode: codes.DeadlineExceeded},
		{name: "server error is not retried", serverCode: codes.Unavailable, wantCode: codes.Unavailable, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			service := &readinessSystemServer{code: test.serverCode}
			apiv1alpha1.RegisterSystemServiceServer(server, service)
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
			require.Equal(t, test.wantCalls, service.calls.Load())
			if test.failedDials >= 0 {
				require.Equal(t, test.failedDials+1, dials.Load())
			}
		})
	}
}

type readinessSystemServer struct {
	apiv1alpha1.UnimplementedSystemServiceServer
	code  codes.Code
	calls atomic.Int32
}

var _ apiv1alpha1.SystemServiceServer = (*readinessSystemServer)(nil)

func (s *readinessSystemServer) GetVersion(context.Context, *apiv1alpha1.GetVersionRequest) (*apiv1alpha1.GetVersionResponse, error) {
	s.calls.Add(1)
	return &apiv1alpha1.GetVersionResponse{}, status.Error(s.code, "controller unavailable")
}
