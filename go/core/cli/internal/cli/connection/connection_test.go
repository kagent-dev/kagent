package connection

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/client"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type failingVersionClient struct {
	err error
}

func (c failingVersionClient) GetVersion(context.Context) (*api.VersionResponse, error) {
	return nil, c.err
}

func TestCheckServerPreservesCause(t *testing.T) {
	permissionErr := status.Error(codes.PermissionDenied, "denied")
	err := checkServer(t.Context(), &client.ClientSet{Version: failingVersionClient{err: permissionErr}})

	require.Error(t, err)
	assert.ErrorIs(t, err, errServerConnection)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestShouldPortForward(t *testing.T) {
	defaultConfig := Options{KAgentURL: defaultKAgentURL, KAgentGRPCURL: defaultKAgentGRPCURL}
	tests := []struct {
		name   string
		config Options
		err    error
		want   bool
	}{
		{name: "default endpoint unavailable", config: defaultConfig, err: status.Error(codes.Unavailable, "offline"), want: true},
		{name: "default endpoint gRPC deadline", config: defaultConfig, err: status.Error(codes.DeadlineExceeded, "deadline"), want: true},
		{name: "default endpoint context deadline", config: defaultConfig, err: context.DeadlineExceeded, want: true},
		{name: "empty gRPC endpoint uses client default", config: Options{KAgentURL: defaultKAgentURL}, err: status.Error(codes.Unavailable, "offline"), want: true},
		{name: "authentication failure", config: defaultConfig, err: status.Error(codes.Unauthenticated, "unauthenticated")},
		{name: "authorization failure", config: defaultConfig, err: status.Error(codes.PermissionDenied, "denied")},
		{name: "explicit TLS", config: Options{KAgentURL: defaultKAgentURL, KAgentGRPCURL: defaultKAgentGRPCURL, KAgentGRPCTLS: true}, err: status.Error(codes.Unavailable, "TLS failed")},
		{name: "explicit gRPC endpoint", config: Options{KAgentURL: defaultKAgentURL, KAgentGRPCURL: "api.example.test:443"}, err: status.Error(codes.Unavailable, "offline")},
		{name: "explicit HTTP endpoint", config: Options{KAgentURL: "https://api.example.test", KAgentGRPCURL: defaultKAgentGRPCURL}, err: status.Error(codes.Unavailable, "offline")},
		{name: "other error", config: defaultConfig, err: errors.New("invalid CA")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldPortForward(&tt.config, tt.err))
		})
	}
}

func TestConnectionRuntimeConnectStartsPortForwardAndRedials(t *testing.T) {
	var output bytes.Buffer
	var clients []*client.ClientSet
	attempts := 0
	runtime := testConnectionRuntime(t, "wait", func(_ context.Context, clientSet *client.ClientSet) error {
		clients = append(clients, clientSet)
		attempts++
		if attempts < 3 {
			return status.Error(codes.Unavailable, "not ready")
		}
		return nil
	})
	runtime.stderr = &output

	portForward, err := runtime.connect(t.Context(), defaultTestConfig(true))
	require.NoError(t, err)
	require.NotNil(t, portForward)
	t.Cleanup(portForward.Stop)

	assert.Len(t, clients, 3)
	assert.NotSame(t, clients[0], clients[1])
	assert.NotSame(t, clients[1], clients[2])
	assert.Contains(t, output.String(), `Using caller identity "test-user"`)
	portForward.Stop()
	assert.NotNil(t, portForward.cmd.ProcessState)
}

func TestConnectionRuntimeConnectDoesNotPortForwardExplicitFailures(t *testing.T) {
	tests := []struct {
		name   string
		config *Options
		err    error
	}{
		{name: "authentication failure", config: defaultTestConfig(false), err: status.Error(codes.Unauthenticated, "unauthenticated")},
		{name: "TLS endpoint", config: func() *Options {
			cfg := defaultTestConfig(false)
			cfg.KAgentGRPCTLS = true
			return cfg
		}(), err: status.Error(codes.Unavailable, "TLS failed")},
		{name: "remote gRPC endpoint", config: func() *Options {
			cfg := defaultTestConfig(false)
			cfg.KAgentGRPCURL = "api.example.test:443"
			return cfg
		}(), err: status.Error(codes.Unavailable, "offline")},
		{name: "remote HTTP endpoint", config: func() *Options {
			cfg := defaultTestConfig(false)
			cfg.KAgentURL = "https://api.example.test"
			return cfg
		}(), err: status.Error(codes.Unavailable, "offline")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := testConnectionRuntime(t, "wait", func(context.Context, *client.ClientSet) error {
				return tt.err
			})
			runtime.commandContext = func(context.Context, string, ...string) *exec.Cmd {
				t.Fatal("kubectl must not start for this failure")
				return nil
			}

			portForward, err := runtime.connect(t.Context(), tt.config)
			require.Error(t, err)
			assert.Nil(t, portForward)
			assert.Equal(t, status.Code(tt.err), status.Code(err))
		})
	}
}

func TestConnectionRuntimeConnectRejectsInvalidClientConfig(t *testing.T) {
	runtime := testConnectionRuntime(t, "wait", func(context.Context, *client.ClientSet) error {
		t.Fatal("server must not be checked with invalid client configuration")
		return nil
	})
	cfg := defaultTestConfig(false)
	cfg.UserID = "invalid user"

	portForward, err := runtime.connect(t.Context(), cfg)
	require.Error(t, err)
	assert.Nil(t, portForward)
	assert.Contains(t, err.Error(), "caller identity")
}

func TestConnectionRuntimeNewPortForwardReportsStartFailure(t *testing.T) {
	runtime := testConnectionRuntime(t, "wait", func(context.Context, *client.ClientSet) error {
		t.Fatal("health probe must not run when kubectl cannot start")
		return nil
	})
	runtime.commandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, t.TempDir()+"/missing-kubectl")
	}

	portForward, err := runtime.newPortForward(t.Context(), defaultTestConfig(false))
	require.Error(t, err)
	assert.Nil(t, portForward)
	assert.Contains(t, err.Error(), "start kubectl port-forward")
}

func TestConnectionRuntimeNewPortForwardReportsKubectlStderr(t *testing.T) {
	runtime := testConnectionRuntime(t, "fail", func(context.Context, *client.ClientSet) error {
		return status.Error(codes.Unavailable, "not ready")
	})

	portForward, err := runtime.newPortForward(t.Context(), defaultTestConfig(false))
	require.Error(t, err)
	assert.Nil(t, portForward)
	assert.Contains(t, err.Error(), "kubectl denied port-forward")
}

func TestConnectionRuntimeNewPortForwardStopsProcessOnTimeout(t *testing.T) {
	var command *exec.Cmd
	runtime := testConnectionRuntime(t, "wait", func(context.Context, *client.ClientSet) error {
		return status.Error(codes.Unavailable, "not ready")
	})
	runtime.readyTimeout = 30 * time.Millisecond
	commandFactory := runtime.commandContext
	runtime.commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		command = commandFactory(ctx, name, args...)
		return command
	}

	portForward, err := runtime.newPortForward(t.Context(), defaultTestConfig(false))
	require.Error(t, err)
	assert.Nil(t, portForward)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotNil(t, command)
	assert.NotNil(t, command.ProcessState)
}

func TestBoundedBuffer(t *testing.T) {
	buffer := newBoundedBuffer(4)
	written, err := buffer.Write([]byte("abcdef"))
	require.NoError(t, err)
	assert.Equal(t, 6, written)
	assert.Equal(t, "abcd", buffer.String())
}

func TestPortForwardHelperProcess(t *testing.T) {
	if os.Getenv("KAGENT_PORT_FORWARD_HELPER") != "1" {
		return
	}

	switch os.Getenv("KAGENT_PORT_FORWARD_BEHAVIOR") {
	case "fail":
		_, _ = fmt.Fprintln(os.Stderr, "kubectl denied port-forward")
		os.Exit(2)
	case "wait":
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(3)
	}
}

func testConnectionRuntime(
	t *testing.T,
	behavior string,
	checkServer func(context.Context, *client.ClientSet) error,
) connectionRuntime {
	t.Helper()
	return connectionRuntime{
		checkServer: checkServer,
		commandContext: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPortForwardHelperProcess$")
			command.Env = append(os.Environ(),
				"KAGENT_PORT_FORWARD_HELPER=1",
				"KAGENT_PORT_FORWARD_BEHAVIOR="+behavior,
			)
			return command
		},
		stderr:       io.Discard,
		readyTimeout: time.Second,
		retryDelay:   time.Millisecond,
	}
}

func defaultTestConfig(verbose bool) *Options {
	return &Options{
		KAgentURL:     defaultKAgentURL,
		KAgentGRPCURL: defaultKAgentGRPCURL,
		Namespace:     "kagent",
		UserID:        "test-user",
		Verbose:       verbose,
	}
}
