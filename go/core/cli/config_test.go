package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/cli"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func executeConfig(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := cli.Root()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return output.String(), err
}

func TestConfigFileAndFlagPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`connection:
  namespace: file-ns
  userId: file-user
  timeout: 12s
  caFile: ca.pem
  verbose: true
output:
  format: json
database:
  urlFile: secret-not-present
install:
  substrate:
    version: v1.2.3
`), 0600))
	output, err := executeConfig(t, "--config", path, "config", "view", "--namespace", "flag-ns", "--timeout", "0", "--verbose=false")
	require.NoError(t, err)
	cfg := &apiv1.CLIConfig{}
	require.NoError(t, protojson.Unmarshal([]byte(output), cfg))
	require.Equal(t, "flag-ns", cfg.Connection.GetNamespace())
	require.Equal(t, "file-user", cfg.Connection.GetUserId())
	require.Zero(t, cfg.Connection.Timeout.AsDuration())
	require.False(t, cfg.Connection.Verbose)
	require.Equal(t, filepath.Join(dir, "ca.pem"), cfg.Connection.CaFile)
	require.Equal(t, "json", cfg.Output.GetFormat())
	require.Equal(t, "v1.2.3", cfg.Install.PodCertificate.GetVersion())
	require.Equal(t, filepath.Join(dir, "secret-not-present"), cfg.Database.UrlFile)
	_, err = executeConfig(t, "--config", path, "config", "validate")
	require.NoError(t, err)
}

func TestConfigIndependentCommands(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")
	for _, args := range [][]string{{"--help"}, {"version"}, {"config", "schema"}} {
		_, err := executeConfig(t, append([]string{"--config", missing}, args...)...)
		require.NoError(t, err)
	}
	_, err := executeConfig(t, "--config", missing, "config", "validate")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDefaultConfigIsOptionalAndCommandTreesAreIndependent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	first, err := executeConfig(t, "config", "view", "--namespace", "first")
	require.NoError(t, err)
	cfg := &apiv1.CLIConfig{}
	require.NoError(t, protojson.Unmarshal([]byte(first), cfg))
	require.Equal(t, "first", cfg.Connection.GetNamespace())
	second, err := executeConfig(t, "config", "view")
	require.NoError(t, err)
	require.NoError(t, protojson.Unmarshal([]byte(second), cfg))
	require.Equal(t, "kagent", cfg.Connection.GetNamespace())
}

func TestDatabasePreRunLoadsRootConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("unexpected: true"), 0600))
	_, err := executeConfig(t, "--config", path, "db", "migrate", "status")
	require.ErrorContains(t, err, "unknown field")
	require.NoError(t, os.WriteFile(path, []byte("database: {urlFile: missing-url}"), 0600))
	_, err = executeConfig(t, "--config", path, "db", "migrate", "status")
	require.ErrorContains(t, err, "missing-url")
}
