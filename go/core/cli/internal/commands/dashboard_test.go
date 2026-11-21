package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDashboardPortForward(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell scripts as subprocess stand-ins")
	}

	for _, tc := range []struct {
		name        string
		portForward string
		wantErr     string
	}{
		{
			name:        "opens the browser once forwarding is ready",
			portForward: "printf 'Forwarding from 127.0.0.1:8082 -> 8080\\n'\nexec sleep 30\n",
		},
		{
			name:        "reports kubectl failure without opening a browser",
			portForward: "echo 'service not found' >&2\nexit 1\n",
			wantErr:     "kubectl port-forward: exit status 1: service not found",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			browser := "xdg-open"
			if runtime.GOOS == "darwin" {
				browser = "open"
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte("#!/bin/sh\n"+tc.portForward), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, browser), []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$DASHBOARD_TEST_URL\"\n"), 0o700))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			openedURL := filepath.Join(dir, "url")
			t.Setenv("DASHBOARD_TEST_URL", openedURL)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := NewDashboardCmd()
			cmd.SetContext(ctx)
			cmd.SetIn(strings.NewReader("\n"))
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)

			err := runDashboard(cmd, "kagent")
			require.NoError(t, ctx.Err())
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Empty(t, stdout.String())
				require.NoFileExists(t, openedURL)
				return
			}
			require.NoError(t, err)
			url, err := os.ReadFile(openedURL)
			require.NoError(t, err)
			require.Equal(t, "http://localhost:8082", string(url))
			require.Contains(t, stdout.String(), string(url))
		})
	}
}
