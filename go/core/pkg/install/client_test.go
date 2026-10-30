package install

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestCommandRunner(t *testing.T) {
	runner := CommandRunner{}
	out, err := runner.Run(t.Context(), Command{Program: "sh", Args: []string{"-c", "cat; echo warning >&2"}, Stdin: "kind: Deployment\n"})
	require.NoError(t, err)
	require.Equal(t, "kind: Deployment\n", out, "successful render output must exclude stderr")
	_, err = runner.Run(t.Context(), Command{Program: "sh", Args: []string{"-c", "echo refused >&2; exit 1"}})
	require.ErrorContains(t, err, "refused")
}

func TestCommandRunnerPrivateValues(t *testing.T) {
	for _, name := range []string{"success", "failure", "encoded failure", "canceled", "start failure"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			temporary := filepath.Join(root, "temporary")
			require.NoError(t, os.Mkdir(temporary, 0o700))
			t.Setenv("TMPDIR", temporary)
			t.Setenv("TEST_CAPTURE", root)
			t.Setenv("TEST_FAIL", name)
			program := filepath.Join(root, "helm")
			require.NoError(t, os.WriteFile(program, []byte(`#!/bin/sh
printf '%s\n' "$@" > "$TEST_CAPTURE/args"
for last; do :; done
printf '%s' "$last" > "$TEST_CAPTURE/path"
cat "$last" > "$TEST_CAPTURE/values"
ls -ld "$last" "$(dirname "$last")" > "$TEST_CAPTURE/permissions"
case "$TEST_FAIL" in
  failure) cat "$last"; cat "$last" >&2; exit 7 ;;
  'encoded failure') base64 < "$last"; base64 < "$last" >&2; exit 7 ;;
esac
printf 'rendered output\n'
`), 0o700))
			ctx := t.Context()
			if name == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if name == "start failure" {
				program = filepath.Join(root, "missing")
			}
			const values = "token: secret-that-must-not-appear\n"
			command := Command{Program: program, Args: []string{"template", "test", "chart", "-f", "operator.yaml"}, SecretValues: values}
			out, err := (CommandRunner{}).Run(ctx, command)
			if name == "success" {
				require.NoError(t, err)
				require.Equal(t, "rendered output\n", out)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret-that-must-not-appear")
				require.NotContains(t, err.Error(), base64.StdEncoding.EncodeToString([]byte(values)))
				require.Contains(t, err.Error(), "output omitted")
				require.Empty(t, out)
				switch name {
				case "failure", "encoded failure":
					var exit *exec.ExitError
					require.ErrorAs(t, err, &exit)
					require.Equal(t, 7, exit.ExitCode())
				case "canceled":
					require.ErrorIs(t, err, context.Canceled)
				}
			}
			files, err := os.ReadDir(temporary)
			require.NoError(t, err)
			require.Empty(t, files, "credential files must be removed on every exit path")
			require.Equal(t, []string{"template", "test", "chart", "-f", "operator.yaml"}, command.Args)
			if name == "canceled" || name == "start failure" {
				return
			}
			data, err := os.ReadFile(filepath.Join(root, "values"))
			require.NoError(t, err)
			require.Equal(t, values, string(data))
			permissions, err := os.ReadFile(filepath.Join(root, "permissions"))
			require.NoError(t, err)
			require.Contains(t, string(permissions), "-rw-------")
			require.Contains(t, string(permissions), "drwx------")
			args, err := os.ReadFile(filepath.Join(root, "args"))
			require.NoError(t, err)
			require.NotContains(t, string(args), "secret-that-must-not-appear")
			path, err := os.ReadFile(filepath.Join(root, "path"))
			require.NoError(t, err)
			require.Equal(t, append(command.Args, "-f", string(path)), strings.Split(strings.TrimSpace(string(args)), "\n"))
		})
	}
}

func TestProviderAPIKeys(t *testing.T) {
	for _, tc := range []struct {
		provider v1alpha3.ModelProvider
		want     []string
	}{
		{v1alpha3.ModelProviderOpenAI, []string{"OPENAI_API_KEY"}},
		{v1alpha3.ModelProviderAnthropic, []string{"ANTHROPIC_API_KEY"}},
		{v1alpha3.ModelProviderAzureOpenAI, []string{"AZURE_OPENAI_API_KEY"}},
		{v1alpha3.ModelProviderOllama, []string{"OLLAMA_API_KEY"}},
		{v1alpha3.ModelProviderGemini, []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"}},
		{v1alpha3.ModelProviderBedrock, nil},
		{v1alpha3.ModelProvider("unknown"), nil},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			var names []string
			for _, key := range ProviderAPIKeys(tc.provider) {
				names = append(names, key.Name())
			}
			require.Equal(t, tc.want, names)
		})
	}
}
