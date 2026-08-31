package connection

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsFromCommandReadsInheritedFlags(t *testing.T) {
	var got Options
	root := &cobra.Command{Use: "root"}
	RegisterFlags(root.PersistentFlags())
	root.AddCommand(&cobra.Command{
		Use: "child",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			got, err = OptionsFromCommand(cmd)
			return err
		},
	})
	root.SetArgs([]string{
		"child",
		"--kagent-url", "https://api.example.test",
		"--grpc-url", "grpc.example.test:443",
		"--grpc-tls",
		"--grpc-ca-file", "/tmp/ca.pem",
		"--grpc-server-name", "grpc.example.test",
		"--namespace", "agents",
		"--verbose",
		"--timeout", "12s",
		"--user-id", "reviewer@example.test",
	})

	require.NoError(t, root.ExecuteContext(t.Context()))
	assert.Equal(t, Options{
		KAgentURL:            "https://api.example.test",
		KAgentGRPCURL:        "grpc.example.test:443",
		KAgentGRPCTLS:        true,
		KAgentGRPCCAFile:     "/tmp/ca.pem",
		KAgentGRPCServerName: "grpc.example.test",
		Namespace:            "agents",
		Verbose:              true,
		Timeout:              12 * time.Second,
		UserID:               "reviewer@example.test",
	}, got)
}
