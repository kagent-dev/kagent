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
		"--api-url", "https://api.example.test",
		"--gateway-url", "https://gateway.example.test",
		"--ca-file", "/tmp/ca.pem",
		"--server-name", "api.example.test",
		"--namespace", "agents",
		"--verbose",
		"--timeout", "12s",
		"--user-id", "reviewer@example.test",
	})

	require.NoError(t, root.ExecuteContext(t.Context()))
	assert.Equal(t, Options{
		APIURL:     "https://api.example.test",
		GatewayURL: "https://gateway.example.test",
		CAFile:     "/tmp/ca.pem",
		ServerName: "api.example.test",
		Namespace:  "agents",
		Verbose:    true,
		Timeout:    12 * time.Second,
		UserID:     "reviewer@example.test",
	}, got)
}
