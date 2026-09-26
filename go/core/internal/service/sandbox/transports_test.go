package sandbox_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	sandboxapi "github.com/kagent-dev/kagent/go/api/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	toolserver "github.com/kagent-dev/kagent/go/core/internal/mcp"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
	"github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/service/system"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestSandboxTransports(t *testing.T) {
	service, _, actors, headers := guestFixture(t)
	listener := bufconn.Listen(1 << 20)
	server, err := grpcserver.New(grpcserver.Config{
		Listener: listener, Authenticator: testAuth{},
		SystemService:  system.NewService(nil, nil, nil, nil),
		SandboxService: service,
	})
	require.NoError(t, err)
	serverCtx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(serverCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("gRPC server did not stop")
		}
	})
	conn, err := grpc.NewClient("passthrough:///sandbox-test", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	client := apiv1alpha1.NewSandboxServiceClient(conn)
	processes := guestpb.NewProcessServiceClient(conn)
	files := guestpb.NewFileSystemServiceClient(conn)
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "alice", "ate-target-actor", "other/victim"))
	created, err := client.CreateSandbox(ctx, createRequest())
	require.NoError(t, err)
	id := created.Sandbox.Id
	ctx = metadata.AppendToOutgoingContext(ctx, sandboxapi.IDHeader, id)
	require.Equal(t, []string{"create", "policy", "resume"}, actors.observedCalls())
	writer, err := files.WriteFile(ctx)
	require.NoError(t, err)
	require.NoError(t, writer.Send(&guestpb.WriteFileRequest{Path: "grpc.bin", Mode: 0o600, Chunk: []byte{0}}))
	require.NoError(t, writer.Send(&guestpb.WriteFileRequest{Chunk: []byte{128, 255}}))
	written, err := writer.CloseAndRecv()
	require.NoError(t, err)
	require.EqualValues(t, 3, written.BytesWritten)
	reader, err := files.ReadFile(ctx, &guestpb.ReadFileRequest{Path: "grpc.bin"})
	require.NoError(t, err)
	var contents []byte
	for {
		chunk, err := reader.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		contents = append(contents, chunk.Data...)
	}
	require.Equal(t, []byte{0, 128, 255}, contents)
	require.Equal(t, "team-a/"+substrate.ActorName(id), headers().Get("ate-target-actor"))
	require.Equal(t, "alice", headers().Get("x-user-id"))
	started, err := processes.StartProcess(ctx, &guestpb.StartProcessRequest{Command: []string{"sh", "-c", "printf grpc"}})
	require.NoError(t, err)
	outputs, err := processes.StreamProcessOutputs(ctx, &guestpb.StreamProcessOutputsRequest{ProcessId: started.ProcessId, Follow: true})
	require.NoError(t, err)
	var output []byte
	for {
		chunk, err := outputs.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		output = append(output, chunk.Data...)
	}
	require.Equal(t, "grpc", string(output))
	// Every upstream method must apply kagent routing and ownership checks.
	calls := []struct {
		name string
		call func(context.Context) error
	}{
		{"start", func(ctx context.Context) error {
			_, err := processes.StartProcess(ctx, &guestpb.StartProcessRequest{Command: []string{"true"}})
			return err
		}},
		{"get", func(ctx context.Context) error {
			_, err := processes.GetProcess(ctx, &guestpb.GetProcessRequest{ProcessId: started.ProcessId})
			return err
		}},
		{"kill", func(ctx context.Context) error {
			_, err := processes.KillProcess(ctx, &guestpb.KillProcessRequest{ProcessId: started.ProcessId})
			return err
		}},
		{"outputs", func(ctx context.Context) error {
			stream, err := processes.StreamProcessOutputs(ctx, &guestpb.StreamProcessOutputsRequest{ProcessId: started.ProcessId})
			if err != nil {
				return err
			}
			_, err = stream.Recv()
			return err
		}},
		{"read", func(ctx context.Context) error {
			stream, err := files.ReadFile(ctx, &guestpb.ReadFileRequest{Path: "grpc.bin"})
			if err != nil {
				return err
			}
			_, err = stream.Recv()
			return err
		}},
		{"write", func(ctx context.Context) error {
			stream, err := files.WriteFile(ctx)
			if err != nil {
				return err
			}
			_, err = stream.CloseAndRecv()
			return err
		}},
	}
	for _, test := range []struct {
		name string
		user string
		ids  []string
		code codes.Code
	}{
		{"missing route", "alice", nil, codes.InvalidArgument},
		{"empty route", "alice", []string{""}, codes.InvalidArgument},
		{"invalid route", "alice", []string{"other/victim"}, codes.InvalidArgument},
		{"duplicate route", "alice", []string{id, id}, codes.InvalidArgument},
		{"conflicting routes", "alice", []string{id, uuid.NewString()}, codes.InvalidArgument},
		{"unknown sandbox", "alice", []string{uuid.NewString()}, codes.NotFound},
		{"other owner", "mallory", []string{id}, codes.NotFound},
		{"unauthenticated", "", []string{id}, codes.Unauthenticated},
	} {
		t.Run(test.name, func(t *testing.T) {
			md := metadata.Pairs("x-user-id", test.user, "ate-target-actor", "other/victim")
			md[sandboxapi.IDHeader] = test.ids
			ctx := metadata.NewOutgoingContext(t.Context(), md)
			for _, call := range calls {
				t.Run(call.name, func(t *testing.T) {
					require.Equal(t, test.code, status.Code(call.call(ctx)))
				})
			}
		})
	}

	t.Run("guest write errors are preserved", func(t *testing.T) {
		stream, err := files.WriteFile(ctx)
		require.NoError(t, err)
		_, err = stream.CloseAndRecv()
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	handler, err := toolserver.New(&session.Service{}, &checkpoint.Service{}, &a2asrv.InterceptedHandler{}, service, nil)
	require.NoError(t, err)
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(auth.AuthSessionTo(r.Context(), testSession("alice"))))
	}))
	t.Cleanup(mcpServer.Close)
	mcpClient := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "sandbox-test", Version: "test"}, nil)
	session, err := mcpClient.Connect(t.Context(), &sdkmcp.StreamableClientTransport{Endpoint: mcpServer.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	result, err := session.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: "write_sandbox_file", Arguments: map[string]any{
		"sandbox_id": id, "path": "mcp.txt", "data_base64": "bWNw",
	}})
	require.NoError(t, err)
	require.False(t, result.IsError, "%+v", result.Content)
	reader, err = files.ReadFile(ctx, &guestpb.ReadFileRequest{Path: "mcp.txt"})
	require.NoError(t, err)
	contents = nil
	for {
		chunk, err := reader.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		contents = append(contents, chunk.Data...)
	}
	require.Equal(t, "mcp", string(contents))
	result, err = session.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: "start_sandbox_process", Arguments: map[string]any{
		"sandbox_id": id, "command": []string{"cat", "mcp.txt"},
	}})
	require.NoError(t, err)
	require.False(t, result.IsError, "%+v", result.Content)
	var process struct {
		ID string `json:"process_id"`
	}
	content, ok := result.Content[0].(*sdkmcp.TextContent)
	require.True(t, ok)
	require.NoError(t, json.Unmarshal([]byte(content.Text), &process))
	outputs, err = processes.StreamProcessOutputs(ctx, &guestpb.StreamProcessOutputsRequest{ProcessId: process.ID, Follow: true})
	require.NoError(t, err)
	output = nil
	for {
		chunk, err := outputs.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		output = append(output, chunk.Data...)
	}
	require.Equal(t, "mcp", string(output))
	_, err = processes.GetProcess(ctx, &guestpb.GetProcessRequest{ProcessId: "unknown-guest-process"})
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Equal(t, []string{"create", "policy", "resume"}, actors.observedCalls(), "guest traffic must never call the Substrate control-plane client")
	actors.mu.Lock()
	actors.mutationErr = status.Error(codes.Unavailable, "suspend response lost after effect")
	actors.mu.Unlock()
	_, err = client.SuspendSandbox(ctx, &apiv1alpha1.SuspendSandboxRequest{SandboxId: id})
	require.Equal(t, codes.Unavailable, status.Code(err))
	pending, err := client.GetSandbox(ctx, &apiv1alpha1.GetSandboxRequest{SandboxId: id})
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, pending.Sandbox.Operation)
	actors.mu.Lock()
	actors.mutationErr = nil
	actors.mu.Unlock()
	suspended, err := client.SuspendSandbox(ctx, &apiv1alpha1.SuspendSandboxRequest{SandboxId: id})
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED, suspended.Sandbox.State)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, suspended.Sandbox.Operation)
	_, err = client.ResumeSandbox(ctx, &apiv1alpha1.ResumeSandboxRequest{SandboxId: id})
	require.NoError(t, err)
	_, err = client.DeleteSandbox(ctx, &apiv1alpha1.DeleteSandboxRequest{SandboxId: id})
	require.NoError(t, err)
	require.Equal(t, []string{"create", "policy", "resume", "suspend", "suspend", "resume", "suspend", "delete"}, actors.observedCalls())
}
