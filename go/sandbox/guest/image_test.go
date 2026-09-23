// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package guest_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"testing"
	"time"

	ateenvv1alpha "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestE2ESandboxGuest(t *testing.T) {
	image := os.Getenv("KAGENT_SANDBOX_GUEST_IMAGE")
	require.NotEmpty(t, image, "build the guest image and set KAGENT_SANDBOX_GUEST_IMAGE")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts("80/tcp"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/readyz").WithPort("80/tcp")),
	)
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err)

	connect := func() *grpc.ClientConn {
		t.Helper()
		host, err := container.Host(ctx)
		require.NoError(t, err)
		port, err := container.MappedPort(ctx, "80/tcp")
		require.NoError(t, err)
		conn, err := grpc.NewClient(net.JoinHostPort(host, port.Port()), grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, conn.Close()) })
		return conn
	}
	conn := connect()
	files := ateenvv1alpha.NewFileSystemServiceClient(conn)
	processes := ateenvv1alpha.NewProcessServiceClient(conn)

	t.Run("binary file transfer", func(t *testing.T) {
		payload := bytes.Repeat([]byte{0, 1, 127, 128, 255}, 32*1024)
		writeFile(t, ctx, files, "input.bin", payload)
		require.Equal(t, payload, readFile(t, ctx, files, "input.bin"))
		stream, err := files.ReadFile(ctx, &ateenvv1alpha.ReadFileRequest{Path: "../outside"})
		require.NoError(t, err)
		_, err = stream.Recv()
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("working directory environment and output", func(t *testing.T) {
		writeFile(t, ctx, files, "script.sh", []byte("pwd; id -u; printf '%s' \"$GUEST_TEST_VALUE\" > result.txt; printf stdout; printf stderr >&2"))
		started, err := processes.StartProcess(ctx, &ateenvv1alpha.StartProcessRequest{
			Command: []string{"sh", "script.sh"},
			Env:     map[string]string{"GUEST_TEST_VALUE": "from-process"},
		})
		require.NoError(t, err)
		result := waitForProcess(t, ctx, processes, started.ProcessId)
		require.Equal(t, ateenvv1alpha.ProcessStatus_PROCESS_STATUS_COMPLETED, result.Status)
		require.Zero(t, result.ExitCode)
		require.NotNil(t, result.FinishedAt)
		require.Equal(t, "from-process", string(readFile(t, ctx, files, "result.txt")))
		stdout, stderr := readOutput(t, ctx, processes, &ateenvv1alpha.StreamProcessOutputsRequest{ProcessId: started.ProcessId})
		require.Equal(t, "/data/workspace\n65532\nstdout", stdout)
		require.Equal(t, "stderr", stderr)

		other := ateenvv1alpha.NewProcessServiceClient(connect())
		stdout, stderr = readOutput(t, ctx, other, &ateenvv1alpha.StreamProcessOutputsRequest{
			ProcessId: started.ProcessId, StdoutOffset: int64(len(stdout) - 3), StderrOffset: 3,
		})
		require.Equal(t, "out", stdout)
		require.Equal(t, "err", stderr)
	})

	t.Run("nonzero exit", func(t *testing.T) {
		started, err := processes.StartProcess(ctx, &ateenvv1alpha.StartProcessRequest{Command: []string{"sh", "-c", "exit 7"}})
		require.NoError(t, err)
		result := waitForProcess(t, ctx, processes, started.ProcessId)
		require.Equal(t, ateenvv1alpha.ProcessStatus_PROCESS_STATUS_FAILED, result.Status)
		require.EqualValues(t, 7, result.ExitCode)
	})

	t.Run("observer disconnect and explicit termination", func(t *testing.T) {
		started, err := processes.StartProcess(ctx, &ateenvv1alpha.StartProcessRequest{Command: []string{"sh", "-c", "printf started; exec sleep 120"}})
		require.NoError(t, err)
		observeCtx, stopObserving := context.WithCancel(ctx)
		defer stopObserving()
		stream, err := processes.StreamProcessOutputs(observeCtx, &ateenvv1alpha.StreamProcessOutputsRequest{ProcessId: started.ProcessId, Follow: true})
		require.NoError(t, err)
		chunk, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, "started", string(chunk.Data))
		stopObserving()
		_, err = stream.Recv()
		require.Equal(t, codes.Canceled, status.Code(err))
		result, err := processes.GetProcess(ctx, &ateenvv1alpha.GetProcessRequest{ProcessId: started.ProcessId})
		require.NoError(t, err)
		require.Equal(t, ateenvv1alpha.ProcessStatus_PROCESS_STATUS_RUNNING, result.Status)
		_, err = processes.KillProcess(ctx, &ateenvv1alpha.KillProcessRequest{ProcessId: started.ProcessId})
		require.NoError(t, err)
		result = waitForProcess(t, ctx, processes, started.ProcessId)
		require.Equal(t, ateenvv1alpha.ProcessStatus_PROCESS_STATUS_TERMINATED, result.Status)
		require.NotNil(t, result.FinishedAt)
		require.EqualValues(t, 137, result.ExitCode)
	})

	t.Run("restart preserves files but not process registry", func(t *testing.T) {
		writeFile(t, ctx, files, "retained.txt", []byte("retained file"))
		started, err := processes.StartProcess(ctx, &ateenvv1alpha.StartProcessRequest{Command: []string{"true"}})
		require.NoError(t, err)
		waitForProcess(t, ctx, processes, started.ProcessId)
		timeout := 5 * time.Second
		require.NoError(t, container.Stop(ctx, &timeout))
		require.NoError(t, container.Start(ctx))
		restarted := connect()
		require.Equal(t, "retained file", string(readFile(t, ctx, ateenvv1alpha.NewFileSystemServiceClient(restarted), "retained.txt")))
		_, err = ateenvv1alpha.NewProcessServiceClient(restarted).GetProcess(ctx, &ateenvv1alpha.GetProcessRequest{ProcessId: started.ProcessId})
		require.Equal(t, codes.NotFound, status.Code(err))
	})
}

func writeFile(t *testing.T, ctx context.Context, client ateenvv1alpha.FileSystemServiceClient, path string, data []byte) {
	t.Helper()
	stream, err := client.WriteFile(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(&ateenvv1alpha.WriteFileRequest{Path: path, Mode: 0o644}))
	for start := 0; start < len(data); start += 16 * 1024 {
		end := min(start+16*1024, len(data))
		require.NoError(t, stream.Send(&ateenvv1alpha.WriteFileRequest{Chunk: data[start:end]}))
	}
	response, err := stream.CloseAndRecv()
	require.NoError(t, err)
	require.EqualValues(t, len(data), response.BytesWritten)
}

func readFile(t *testing.T, ctx context.Context, client ateenvv1alpha.FileSystemServiceClient, path string) []byte {
	t.Helper()
	stream, err := client.ReadFile(ctx, &ateenvv1alpha.ReadFileRequest{Path: path})
	require.NoError(t, err)
	var data []byte
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			return data
		}
		require.NoError(t, err)
		data = append(data, chunk.Data...)
	}
}

func waitForProcess(t *testing.T, ctx context.Context, client ateenvv1alpha.ProcessServiceClient, id string) *ateenvv1alpha.Process {
	t.Helper()
	var result *ateenvv1alpha.Process
	require.Eventually(t, func() bool {
		var err error
		result, err = client.GetProcess(ctx, &ateenvv1alpha.GetProcessRequest{ProcessId: id})
		return err == nil && result.Status != ateenvv1alpha.ProcessStatus_PROCESS_STATUS_RUNNING && result.FinishedAt != nil
	}, 10*time.Second, 20*time.Millisecond, "process did not finish")
	return result
}

func readOutput(t *testing.T, ctx context.Context, client ateenvv1alpha.ProcessServiceClient, request *ateenvv1alpha.StreamProcessOutputsRequest) (string, string) {
	t.Helper()
	stream, err := client.StreamProcessOutputs(ctx, request)
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			return stdout.String(), stderr.String()
		}
		require.NoError(t, err)
		switch chunk.Source {
		case ateenvv1alpha.OutputSource_OUTPUT_SOURCE_STDOUT:
			stdout.Write(chunk.Data)
		case ateenvv1alpha.OutputSource_OUTPUT_SOURCE_STDERR:
			stderr.Write(chunk.Data)
		default:
			t.Fatalf("unexpected output source: %s", chunk.Source)
		}
	}
}
