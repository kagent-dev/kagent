// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/agent-substrate/env/guest"
	"github.com/stretchr/testify/require"
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
		{name: "occupied listener", address: listener.Addr().String(), logDir: t.TempDir(), wantErr: "failed to serve guest API"},
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
