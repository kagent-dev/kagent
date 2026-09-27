package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/agent-substrate/env/guest"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	sandboxapi "github.com/kagent-dev/kagent/go/api/sandbox"
	"github.com/kagent-dev/kagent/go/core/cli"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const sandboxTestID = "33333333-3333-4333-8333-333333333333"

type sandboxTestServer struct {
	apiv1alpha1.UnimplementedSandboxServiceServer
	apiv1alpha1.UnimplementedSandboxTemplateServiceServer
	guestpb.UnimplementedProcessServiceServer
	guestpb.UnimplementedFileSystemServiceServer
	mu           sync.Mutex
	created      *apiv1alpha1.CreateSandboxRequest
	starts       int
	command      *guestpb.StartProcessRequest
	startErr     error
	outputErr    error
	readErr      error
	writeErr     error
	running      bool
	exitCode     int32
	stdout       []byte
	stderr       []byte
	files        map[string][]byte
	wrongReceipt bool
}

func newSandboxTestServer(t *testing.T) (*sandboxTestServer, string) {
	t.Helper()
	s := &sandboxTestServer{files: map[string][]byte{}, stdout: []byte("hello"), stderr: []byte("warning")}
	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, health.NewServer())
	apiv1alpha1.RegisterSandboxServiceServer(server, s)
	apiv1alpha1.RegisterSandboxTemplateServiceServer(server, s)
	guestpb.RegisterProcessServiceServer(server, s)
	guestpb.RegisterFileSystemServiceServer(server, s)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return s, "http://" + listener.Addr().String()
}

func runSandboxCLI(t *testing.T, endpoint string, args ...string) (string, string, error) {
	t.Helper()
	cmd := cli.Root()
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"--api-url", endpoint, "--user-id", "cli-test"}, args...))
	// Guest calls must replace stale routing metadata while preserving auth.
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer test", sandboxapi.IDHeader, "stale", sandboxapi.IDHeader, "also-stale"))
	err := cmd.ExecuteContext(ctx)
	return out.String(), stderr.String(), err
}

func checkGuestMetadata(ctx context.Context) error {
	md, _ := metadata.FromIncomingContext(ctx)
	if len(md.Get(sandboxapi.IDHeader)) != 1 || md.Get(sandboxapi.IDHeader)[0] != sandboxTestID ||
		len(md.Get("x-user-id")) != 1 || md.Get("x-user-id")[0] != "cli-test" ||
		len(md.Get("authorization")) != 1 || md.Get("authorization")[0] != "Bearer test" {
		return status.Error(codes.PermissionDenied, "unexpected routing or authentication metadata")
	}
	if _, ok := ctx.Deadline(); !ok {
		return status.Error(codes.InvalidArgument, "missing deadline")
	}
	return nil
}

func (s *sandboxTestServer) CreateSandbox(ctx context.Context, request *apiv1alpha1.CreateSandboxRequest) (*apiv1alpha1.CreateSandboxResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = proto.Clone(request).(*apiv1alpha1.CreateSandboxRequest)
	md, _ := metadata.FromIncomingContext(ctx)
	return &apiv1alpha1.CreateSandboxResponse{Sandbox: &apiv1alpha1.Sandbox{
		Id: sandboxTestID, Creator: md.Get("x-user-id")[0], SandboxTemplate: request.SandboxTemplate,
		State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)),
	}}, nil
}

func (s *sandboxTestServer) ListSandboxTemplates(_ context.Context, request *apiv1alpha1.ListSandboxTemplatesRequest) (*apiv1alpha1.ListSandboxTemplatesResponse, error) {
	if request.Namespace != "team-a" {
		return nil, status.Error(codes.InvalidArgument, "unexpected namespace")
	}
	return &apiv1alpha1.ListSandboxTemplatesResponse{SandboxTemplates: []*apiv1alpha1.SandboxTemplate{{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "python"}, WorkloadImage: "python-image",
	}}}, nil
}

func (s *sandboxTestServer) GetSandbox(_ context.Context, request *apiv1alpha1.GetSandboxRequest) (*apiv1alpha1.GetSandboxResponse, error) {
	if request.SandboxId != sandboxTestID {
		return nil, status.Error(codes.NotFound, "sandbox not found")
	}
	return &apiv1alpha1.GetSandboxResponse{Sandbox: &apiv1alpha1.Sandbox{Id: sandboxTestID, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY}}, nil
}

func (s *sandboxTestServer) ListSandboxes(_ context.Context, request *apiv1alpha1.ListSandboxesRequest) (*apiv1alpha1.ListSandboxesResponse, error) {
	if request.GetPage().GetLimit() != 2 || request.GetPage().GetPageToken() != "cursor" {
		return nil, status.Error(codes.InvalidArgument, "unexpected pagination")
	}
	return &apiv1alpha1.ListSandboxesResponse{
		Sandboxes: []*apiv1alpha1.Sandbox{{Id: sandboxTestID}},
		Page:      &apiv1alpha1.PageResponse{NextPageToken: "next-cursor"},
	}, nil
}

func (s *sandboxTestServer) DeleteSandbox(_ context.Context, request *apiv1alpha1.DeleteSandboxRequest) (*apiv1alpha1.DeleteSandboxResponse, error) {
	if request.SandboxId != sandboxTestID {
		return nil, status.Error(codes.NotFound, "sandbox not found")
	}
	return &apiv1alpha1.DeleteSandboxResponse{Sandbox: &apiv1alpha1.Sandbox{Id: sandboxTestID, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED}}, nil
}

func (s *sandboxTestServer) StartProcess(ctx context.Context, request *guestpb.StartProcessRequest) (*guestpb.StartProcessResponse, error) {
	if err := checkGuestMetadata(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts++
	s.command = proto.Clone(request).(*guestpb.StartProcessRequest)
	if s.startErr != nil {
		return nil, s.startErr
	}
	return &guestpb.StartProcessResponse{ProcessId: "process-1"}, nil
}

func (s *sandboxTestServer) GetProcess(ctx context.Context, request *guestpb.GetProcessRequest) (*guestpb.Process, error) {
	if err := checkGuestMetadata(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := guestpb.ProcessStatus_PROCESS_STATUS_COMPLETED
	if s.running {
		state = guestpb.ProcessStatus_PROCESS_STATUS_RUNNING
	} else if s.exitCode != 0 {
		state = guestpb.ProcessStatus_PROCESS_STATUS_FAILED
	}
	return &guestpb.Process{ProcessId: request.ProcessId, Status: state, ExitCode: s.exitCode}, nil
}

func (s *sandboxTestServer) StreamProcessOutputs(request *guestpb.StreamProcessOutputsRequest, stream grpc.ServerStreamingServer[guestpb.OutputChunk]) error {
	if err := checkGuestMetadata(stream.Context()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, output := range []struct {
		source guestpb.OutputSource
		data   []byte
		offset int64
	}{{guestpb.OutputSource_OUTPUT_SOURCE_STDOUT, s.stdout, request.StdoutOffset}, {guestpb.OutputSource_OUTPUT_SOURCE_STDERR, s.stderr, request.StderrOffset}} {
		if output.offset < 0 || output.offset > int64(len(output.data)) {
			return status.Error(codes.OutOfRange, "invalid continuation offset")
		}
		if len(output.data[output.offset:]) > 0 {
			if err := stream.Send(&guestpb.OutputChunk{Source: output.source, Data: output.data[output.offset:]}); err != nil {
				return err
			}
		}
	}
	return s.outputErr
}

func (s *sandboxTestServer) WriteFile(stream grpc.ClientStreamingServer[guestpb.WriteFileRequest, guestpb.WriteFileResponse]) error {
	if err := checkGuestMetadata(stream.Context()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	header, err := stream.Recv()
	if err != nil {
		return err
	}
	data := append([]byte(nil), header.Chunk...)
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		data = append(data, chunk.Chunk...)
	}
	s.files[header.Path] = data
	written := int64(len(data))
	if s.wrongReceipt {
		written++
	}
	return stream.SendAndClose(&guestpb.WriteFileResponse{BytesWritten: written})
}

func (s *sandboxTestServer) ReadFile(request *guestpb.ReadFileRequest, stream grpc.ServerStreamingServer[guestpb.FileChunk]) error {
	if err := checkGuestMetadata(stream.Context()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.files[request.Path]
	if !ok {
		return status.Error(codes.NotFound, "file missing")
	}
	for len(data) > 0 {
		n := min(len(data), 64<<10)
		if err := stream.Send(&guestpb.FileChunk{Data: data[:n]}); err != nil {
			return err
		}
		if s.readErr != nil {
			return s.readErr
		}
		data = data[n:]
	}
	return nil
}

func TestSandboxCLICreateRetainsRequestIdentity(t *testing.T) {
	s, endpoint := newSandboxTestServer(t)
	for range 2 {
		out, _, err := runSandboxCLI(t, endpoint, "create", "sandbox", "python", "-n", "team-a", "--request-id", "retained-request", "--ttl", "15m", "-o", "json")
		require.NoError(t, err)
		var value struct{ ID, Creator string }
		require.NoError(t, json.Unmarshal([]byte(out), &value))
		require.Equal(t, sandboxTestID, value.ID)
		require.Equal(t, "cli-test", value.Creator)
		s.mu.Lock()
		request := proto.Clone(s.created).(*apiv1alpha1.CreateSandboxRequest)
		s.mu.Unlock()
		require.Equal(t, "retained-request", request.RequestId)
		require.Equal(t, "team-a", request.SandboxTemplate.Namespace)
		require.Equal(t, "python", request.SandboxTemplate.Name)
		require.Equal(t, 15*time.Minute, request.Ttl.AsDuration())
	}
}

func TestSandboxCLIResourceCommands(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want proto.Message
	}{
		{
			name: "templates",
			args: []string{"get", "sandbox-template", "-n", "team-a"},
			want: &apiv1alpha1.ListSandboxTemplatesResponse{SandboxTemplates: []*apiv1alpha1.SandboxTemplate{{
				Ref: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "python"}, WorkloadImage: "python-image",
			}}},
		},
		{
			name: "get",
			args: []string{"get", "sandbox", sandboxTestID},
			want: &apiv1alpha1.Sandbox{Id: sandboxTestID, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY},
		},
		{
			name: "list",
			args: []string{"get", "sandbox", "--page-size", "2", "--page-token", "cursor"},
			want: &apiv1alpha1.ListSandboxesResponse{
				Sandboxes: []*apiv1alpha1.Sandbox{{Id: sandboxTestID}},
				Page:      &apiv1alpha1.PageResponse{NextPageToken: "next-cursor"},
			},
		},
		{
			name: "delete",
			args: []string{"delete", "sandbox", sandboxTestID},
			want: &apiv1alpha1.Sandbox{Id: sandboxTestID, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, endpoint := newSandboxTestServer(t)
			out, _, err := runSandboxCLI(t, endpoint, append(tt.args, "-o", "json")...)
			require.NoError(t, err)
			actual := tt.want.ProtoReflect().New().Interface()
			require.NoError(t, protojson.Unmarshal([]byte(out), actual))
			require.True(t, proto.Equal(tt.want, actual), "got %v, want %v", actual, tt.want)
		})
	}
}

func TestSandboxCLIGetRejectsPaginationWithID(t *testing.T) {
	for _, flag := range []string{"--page-size=2", "--page-token=cursor"} {
		t.Run(flag, func(t *testing.T) {
			_, _, err := runSandboxCLI(t, "http://127.0.0.1:1", "get", "sandbox", sandboxTestID, flag)
			require.ErrorContains(t, err, "pagination flags cannot be used when getting one sandbox")
		})
	}
}

func TestSandboxCLIExecAndExitStatus(t *testing.T) {
	for _, code := range []int32{0, 7} {
		t.Run(strconv.Itoa(int(code)), func(t *testing.T) {
			s, endpoint := newSandboxTestServer(t)
			s.exitCode = code
			out, stderr, err := runSandboxCLI(t, endpoint, "sandbox", "exec", sandboxTestID, "--env", "MODE=test", "--", "python3", "-c", "print('hello')")
			if code == 0 {
				require.NoError(t, err)
			} else {
				var exitError interface{ ExitCode() int }
				require.ErrorAs(t, err, &exitError)
				require.Equal(t, int(code), exitError.ExitCode())
			}
			require.Equal(t, "hello", out)
			require.Contains(t, stderr, "warning")
			require.Contains(t, stderr, "process-1")
			s.mu.Lock()
			defer s.mu.Unlock()
			require.Equal(t, 1, s.starts)
			require.Equal(t, []string{"python3", "-c", "print('hello')"}, s.command.Command)
			require.Equal(t, "/data/workspace", s.command.Cwd)
			require.Equal(t, map[string]string{"MODE": "test"}, s.command.Env)
		})
	}
}

func TestSandboxCLIUncertainStartIsNotRetried(t *testing.T) {
	s, endpoint := newSandboxTestServer(t)
	s.startErr = status.Error(codes.Unavailable, "response lost")
	_, _, err := runSandboxCLI(t, endpoint, "sandbox", "exec", sandboxTestID, "--", "command")
	require.ErrorContains(t, err, "may have started")
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Equal(t, 1, s.starts)
}

func TestSandboxCLIEmptyIDDoesNotUseStaleRouting(t *testing.T) {
	s, endpoint := newSandboxTestServer(t)
	_, _, err := runSandboxCLI(t, endpoint, "sandbox", "exec", "", "--", "command")
	require.ErrorContains(t, err, "sandbox ID is required")
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Zero(t, s.starts)
}

func TestSandboxCLIWaitContinuesOutputWithoutRestart(t *testing.T) {
	s, endpoint := newSandboxTestServer(t)
	s.outputErr = status.Error(codes.Unavailable, "output disconnected")
	out, _, err := runSandboxCLI(t, endpoint, "sandbox", "exec", sandboxTestID, "-o", "json", "--", "command")
	require.ErrorContains(t, err, "sandbox wait")
	var event struct {
		Event, ProcessID           string
		StdoutOffset, StderrOffset int64
	}
	var records []map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewBufferString(out))
	for decoder.More() {
		var record map[string]json.RawMessage
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	last := records[len(records)-1]
	require.NoError(t, json.Unmarshal(last["event"], &event.Event))
	require.NoError(t, json.Unmarshal(last["process_id"], &event.ProcessID))
	require.NoError(t, json.Unmarshal(last["stdout_offset"], &event.StdoutOffset))
	require.NoError(t, json.Unmarshal(last["stderr_offset"], &event.StderrOffset))
	require.Equal(t, "interrupted", event.Event)
	require.Equal(t, "process-1", event.ProcessID)
	require.EqualValues(t, 5, event.StdoutOffset)
	require.EqualValues(t, 7, event.StderrOffset)
	s.mu.Lock()
	s.outputErr = nil
	s.stdout = []byte("hello world")
	s.mu.Unlock()
	out, stderr, err := runSandboxCLI(t, endpoint, "sandbox", "wait", sandboxTestID, event.ProcessID, "--stdout-offset", "5", "--stderr-offset", "7")
	require.NoError(t, err)
	require.Equal(t, " world", out)
	require.NotContains(t, stderr, "warning")
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Equal(t, 1, s.starts)
}

func TestSandboxCLIWaitTimeoutPreservesProcess(t *testing.T) {
	s, endpoint := newSandboxTestServer(t)
	s.running = true
	_, _, err := runSandboxCLI(t, endpoint, "sandbox", "exec", sandboxTestID, "--timeout", "300ms", "--", "command")
	require.ErrorContains(t, err, "process-1")
	require.ErrorContains(t, err, "sandbox wait")
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Equal(t, 1, s.starts)
	require.True(t, s.running)
}

func TestSandboxCLIFileTransfer(t *testing.T) {
	s, endpoint := newSandboxTestServer(t)
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.bin"), filepath.Join(dir, "output.bin")
	data := bytes.Repeat([]byte{0, 255, 1, 2, 3}, 300000) // Above the MCP limit; multiple gRPC chunks.
	require.NoError(t, os.WriteFile(input, data, 0600))
	_, _, err := runSandboxCLI(t, endpoint, "sandbox", "upload", sandboxTestID, input, "input.bin")
	require.NoError(t, err)
	_, _, err = runSandboxCLI(t, endpoint, "sandbox", "download", sandboxTestID, "input.bin", output)
	require.NoError(t, err)
	got, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, data, got)

	s.mu.Lock()
	s.readErr = status.Error(codes.Unavailable, "transfer interrupted")
	s.mu.Unlock()
	require.NoError(t, os.WriteFile(output, []byte("existing artifact"), 0600))
	_, _, err = runSandboxCLI(t, endpoint, "sandbox", "download", sandboxTestID, "input.bin", output)
	require.Error(t, err)
	got, err = os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, "existing artifact", string(got))
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 2, "failed download must remove its staging file")

	s.mu.Lock()
	s.wrongReceipt = true
	s.mu.Unlock()
	_, _, err = runSandboxCLI(t, endpoint, "sandbox", "upload", sandboxTestID, input, "other.bin")
	require.ErrorContains(t, err, "acknowledged")
	s.mu.Lock()
	s.writeErr = status.Error(codes.PermissionDenied, "write denied")
	s.mu.Unlock()
	_, _, err = runSandboxCLI(t, endpoint, "sandbox", "upload", sandboxTestID, input, "denied.bin")
	require.ErrorContains(t, err, "write denied")
}

func TestSandboxCLIWithGuest(t *testing.T) {
	cfg := guest.DefaultConfig()
	cfg.Workspace, cfg.LogDir = t.TempDir(), t.TempDir()
	server, cleanup, err := guest.NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	t.Cleanup(server.Stop)
	healthpb.RegisterHealthServer(server, health.NewServer())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	endpoint := "http://" + listener.Addr().String()
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.txt"), filepath.Join(dir, "output.txt")
	require.NoError(t, os.WriteFile(input, []byte("hello"), 0600))
	_, _, err = runSandboxCLI(t, endpoint, "sandbox", "upload", sandboxTestID, input, "input.txt")
	require.NoError(t, err)
	out, stderr, err := runSandboxCLI(t, endpoint, "sandbox", "exec", sandboxTestID, "--cwd", cfg.Workspace, "--", "sh", "-c", "tr a-z A-Z < input.txt > output.txt; printf done; printf warning >&2; exit 7")
	var exitError interface{ ExitCode() int }
	require.ErrorAs(t, err, &exitError)
	require.Equal(t, 7, exitError.ExitCode())
	require.Equal(t, "done", out)
	require.Contains(t, stderr, "warning")
	_, _, err = runSandboxCLI(t, endpoint, "sandbox", "download", sandboxTestID, "output.txt", output)
	require.NoError(t, err)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, "HELLO", string(data))
}
