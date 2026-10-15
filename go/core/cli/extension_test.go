package cli_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"

	"github.com/kagent-dev/kagent/go/core/cli"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

// addAuthorization stands in for an embedding binary's interceptor.
func addAuthorization(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	return invoker(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer user-token"), method, req, reply, cc, opts...)
}

// startAuthorizationRecorder serves gRPC health on loopback and records the
// authorization metadata of every call.
func startAuthorizationRecorder(t *testing.T) (string, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		mu.Lock()
		seen = append(seen, md.Get("authorization")...)
		mu.Unlock()
		return handler(ctx, req)
	}))
	healthpb.RegisterHealthServer(server, health.NewServer())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return "http://" + listener.Addr().String(), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// An embedding binary adds a command that calls its own service over Dial.
// Its interceptor must run on that call and on the reachability check, and the
// command must see the resolved configuration.
func TestEmbeddedCommandDialsWithDialOptions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	endpoint, seen := startAuthorizationRecorder(t)
	root := cli.Root(cli.WithGRPCDialOptions(grpc.WithChainUnaryInterceptor(addAuthorization)))
	root.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cli.ConfigFromContext(cmd.Context()) == nil {
				return errors.New("configuration was not resolved")
			}
			conn, closeConn, err := cli.Dial(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = closeConn() }()
			_, err = healthpb.NewHealthClient(conn).Check(cmd.Context(), &healthpb.HealthCheckRequest{})
			return err
		},
	})
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"--api-url", endpoint, "probe"})

	require.NoError(t, root.ExecuteContext(t.Context()))

	// The reachability check and the command's own call both carry it.
	require.Equal(t, []string{"Bearer user-token", "Bearer user-token"}, seen())
}

func TestExtraMigrationsRunWithTheCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	called := false
	root := cli.Root(cli.WithExtraMigrations(func(ctx context.Context) ([]migrations.Source, error) {
		called = true
		if cli.ConfigFromContext(ctx) == nil {
			return nil, errors.New("configuration was not resolved")
		}
		return nil, errors.New("extra tracks resolved")
	}))
	require.False(t, called, "the tracks resolve when a db command runs, not when the tree is built")
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"db", "migrate", "version", "--db-url", "postgres://unused", "--db-role", "kagent_owner"})

	require.ErrorContains(t, root.ExecuteContext(t.Context()), "extra tracks resolved")
}
