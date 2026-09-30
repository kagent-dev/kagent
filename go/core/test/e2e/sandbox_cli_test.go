package e2e_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/cli"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestSandboxCLI(t *testing.T) {
	t.Parallel()
	f := newSandboxFixture(t)
	// Wait for preparation using the suite's existing template fixture.
	prepared := f.create(t, 5*time.Minute)
	_, err := f.client.DeleteSandbox(f.ctx, &apiv1alpha1.DeleteSandboxRequest{SandboxId: prepared.Id})
	require.NoError(t, err)
	run := func(args ...string) (string, error) {
		cmd := cli.Root()
		var out, stderr bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&stderr)
		cmd.SetArgs(append([]string{"--api-url", "http://" + interactionTarget(t), "--user-id", "e2e", "--namespace", f.template.Namespace, "--timeout", "2m", "sandbox"}, args...))
		err := cmd.ExecuteContext(t.Context())
		if err != nil {
			t.Logf("CLI stderr: %s", stderr.String())
		}
		return out.String(), err
	}
	created, err := run("create", f.template.Name, "--request-id", uuid.NewString(), "--ttl", "5m", "-o", "json")
	require.NoError(t, err)
	var value apiv1alpha1.Sandbox
	require.NoError(t, protojson.Unmarshal([]byte(created), &value))
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, value.State)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, err := f.client.DeleteSandbox(ctx, &apiv1alpha1.DeleteSandboxRequest{SandboxId: value.Id})
		require.NoError(t, err)
	})
	data := bytes.Repeat([]byte{0, 255, 42, 10}, 300000)
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.bin"), filepath.Join(dir, "output.bin")
	require.NoError(t, os.WriteFile(input, data, 0600))
	_, err = run("upload", value.Id, input, "input.bin")
	require.NoError(t, err)
	result, err := run("exec", value.Id, "--", "sh", "-c", "cat input.bin > output.bin && printf 'copied\\n'")
	require.NoError(t, err)
	require.Equal(t, "copied\n", result)
	_, err = run("suspend", value.Id)
	require.NoError(t, err)
	_, err = run("resume", value.Id)
	require.NoError(t, err)
	_, err = run("download", value.Id, "output.bin", output)
	require.NoError(t, err)
	actual, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, data, actual)
	_, err = run("exec", value.Id, "--", "sh", "-c", "exit 7")
	var exitError interface{ ExitCode() int }
	require.ErrorAs(t, err, &exitError)
	require.Equal(t, 7, exitError.ExitCode())
	deleted, err := run("delete", value.Id, "-o", "json")
	require.NoError(t, err)
	var tombstone apiv1alpha1.Sandbox
	require.NoError(t, protojson.Unmarshal([]byte(deleted), &tombstone))
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED, tombstone.State)
}
