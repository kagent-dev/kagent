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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const sandboxTestID = "33333333-3333-4333-8333-333333333333"

type sandboxTestServer struct {
	apiv1alpha1.UnimplementedSandboxServiceServer
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
	cmd.SetArgs(append([]string{"--api-url", endpoint, "--user-id", "cli-test", "sandbox"}, args...))
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
		out, _, err := runSandboxCLI(t, endpoint, "create", "python", "-n", "team-a", "--request-id", "retained-request", "--ttl", "15m", "-o", "json")
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

func TestSandboxCLIExecAndExitStatus(t *testing.T) {
	for _, code := range []int32{0, 7} {
		t.Run(strconv.Itoa(int(code)), func(t *testing.T) {
			s, endpoint := newSandboxTestServer(t)
			s.exitCode = code
			out, stderr, err := runSandboxCLI(t, endpoint, "exec", sandboxTestID, "--env", "MODE=test", "--", "python3", "-c", "print('hello')")
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
	_, _, err := runSandboxCLI(t, endpoint, "exec", sandboxTestID, "--", "command")
	require.ErrorContains(t, err, "may have started")
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Equal(t, 1, s.starts)
}

func TestSandboxCLIEmptyIDDoesNotUseStaleRouting(t *testing.T) {
	s, endpoint := newSandboxTestServer(t)
	_, _, err := runSandboxCLI(t, endpoint, "exec", "", "--", "command")
	require.ErrorContains(t, err, "sandbox ID is required")
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Zero(t, s.starts)
}

func TestSandboxCLIWaitContinuesOutputWithoutRestart(t *testing.T) {
	s, endpoint := newSandboxTestServer(t)
	s.outputErr = status.Error(codes.Unavailable, "output disconnected")
	out, _, err := runSandboxCLI(t, endpoint, "exec", sandboxTestID, "-o", "json", "--", "command")
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
	out, stderr, err := runSandboxCLI(t, endpoint, "wait", sandboxTestID, event.ProcessID, "--stdout-offset", "5", "--stderr-offset", "7")
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
	_, _, err := runSandboxCLI(t, endpoint, "exec", sandboxTestID, "--timeout", "300ms", "--", "command")
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
	_, _, err := runSandboxCLI(t, endpoint, "upload", sandboxTestID, input, "input.bin")
	require.NoError(t, err)
	_, _, err = runSandboxCLI(t, endpoint, "download", sandboxTestID, "input.bin", output)
	require.NoError(t, err)
	got, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, data, got)

	s.mu.Lock()
	s.readErr = status.Error(codes.Unavailable, "transfer interrupted")
	s.mu.Unlock()
	require.NoError(t, os.WriteFile(output, []byte("existing artifact"), 0600))
	_, _, err = runSandboxCLI(t, endpoint, "download", sandboxTestID, "input.bin", output)
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
	_, _, err = runSandboxCLI(t, endpoint, "upload", sandboxTestID, input, "other.bin")
	require.ErrorContains(t, err, "acknowledged")
	s.mu.Lock()
	s.writeErr = status.Error(codes.PermissionDenied, "write denied")
	s.mu.Unlock()
	_, _, err = runSandboxCLI(t, endpoint, "upload", sandboxTestID, input, "denied.bin")
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
	_, _, err = runSandboxCLI(t, endpoint, "upload", sandboxTestID, input, "input.txt")
	require.NoError(t, err)
	out, stderr, err := runSandboxCLI(t, endpoint, "exec", sandboxTestID, "--cwd", cfg.Workspace, "--", "sh", "-c", "tr a-z A-Z < input.txt > output.txt; printf done; printf warning >&2; exit 7")
	var exitError interface{ ExitCode() int }
	require.ErrorAs(t, err, &exitError)
	require.Equal(t, 7, exitError.ExitCode())
	require.Equal(t, "done", out)
	require.Contains(t, stderr, "warning")
	_, _, err = runSandboxCLI(t, endpoint, "download", sandboxTestID, "output.txt", output)
	require.NoError(t, err)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, "HELLO", string(data))
}
