// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agent-substrate/env/guest"
	ateenvv1alpha "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestRunStartupFailures(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	file := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	for _, tt := range []struct {
		name    string
		address string
		logDir  string
		wantErr string
	}{
		{name: "occupied listener", address: listener.Addr().String(), logDir: t.TempDir(), wantErr: "failed to listen for guest API"},
		{name: "invalid log directory", address: "127.0.0.1:0", logDir: file, wantErr: "failed to initialize guest services"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := run(t.Context(), guest.Config{
				ListenAddr: tt.address, LogDir: tt.logDir, Workspace: t.TempDir(),
				EnableProcess: true, EnableFileSystem: true,
			})
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestServe(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	cfg := guest.Config{
		Workspace: filepath.Join(t.TempDir(), "workspace"), LogDir: t.TempDir(),
		EnableProcess: true, EnableFileSystem: true,
	}
	done := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = serve(ctx, cfg, listener)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
			require.NoError(t, serveErr)
		case <-time.After(5 * time.Second):
			t.Fatal("guest server did not stop after cancellation")
		}
	})

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listener.Addr().String()+"/readyz", nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "ok\n", string(body))

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	// NotFound distinguishes a registered process service from Unimplemented.
	_, err = ateenvv1alpha.NewProcessServiceClient(conn).GetProcess(ctx, &ateenvv1alpha.GetProcessRequest{ProcessId: "missing"})
	require.Equal(t, codes.NotFound, status.Code(err))
	processes := ateenvv1alpha.NewProcessServiceClient(conn)
	started, err := processes.StartProcess(ctx, &ateenvv1alpha.StartProcessRequest{Command: []string{"sh", "-c", "printf initialized > first.txt"}})
	require.NoError(t, err, "first operation must not depend on a preceding file write")
	require.Eventually(t, func() bool {
		result, err := processes.GetProcess(ctx, &ateenvv1alpha.GetProcessRequest{ProcessId: started.ProcessId})
		return err == nil && result.Status == ateenvv1alpha.ProcessStatus_PROCESS_STATUS_COMPLETED
	}, time.Second, 10*time.Millisecond)
	first, err := os.ReadFile(filepath.Join(cfg.Workspace, "first.txt"))
	require.NoError(t, err)
	require.Equal(t, "initialized", string(first))

	files := ateenvv1alpha.NewFileSystemServiceClient(conn)
	writer, err := files.WriteFile(ctx)
	require.NoError(t, err)
	require.NoError(t, writer.Send(&ateenvv1alpha.WriteFileRequest{Path: "test.bin", Mode: 0o600}))
	payload := []byte{0, 1, 127, 128, 255}
	require.NoError(t, writer.Send(&ateenvv1alpha.WriteFileRequest{Chunk: payload}))
	written, err := writer.CloseAndRecv()
	require.NoError(t, err)
	require.EqualValues(t, len(payload), written.BytesWritten)
	stored, err := os.ReadFile(filepath.Join(cfg.Workspace, "test.bin"))
	require.NoError(t, err)
	require.Equal(t, payload, stored, "file service must use the configured workspace")
	reader, err := files.ReadFile(ctx, &ateenvv1alpha.ReadFileRequest{Path: "test.bin"})
	require.NoError(t, err)
	chunk, err := reader.Recv()
	require.NoError(t, err)
	require.Equal(t, payload, chunk.Data)
	_, err = reader.Recv()
	require.ErrorIs(t, err, io.EOF)
}
