package commands

import (
	"bytes"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/internal/version"
)

func TestVersionClientOnly(t *testing.T) {
	t.Run("Given a reachable API address, when version runs with --client, assert that it prints the CLI version without contacting the server", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = listener.Close() })
		var connections atomic.Int32
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				connections.Add(1)
				_ = conn.Close()
			}
		}()
		previous := version.Version
		version.Version = "v9.8.7"
		t.Cleanup(func() { version.Version = previous })
		root := &cobra.Command{Use: "kagent"}
		connection.RegisterFlags(root.PersistentFlags())
		root.AddCommand(NewVersionCmd())
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs([]string{"--api-url", "http://" + listener.Addr().String(), "version", "--client"})

		require.NoError(t, root.ExecuteContext(t.Context()))

		var printed map[string]string
		require.NoError(t, json.Unmarshal(out.Bytes(), &printed))
		require.Equal(t, "v9.8.7", printed["kagent_version"])
		require.NotContains(t, printed, "backend_version")
		require.Zero(t, connections.Load(), "--client must not connect to the API server")
	})
}
