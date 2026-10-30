// Package install plans and applies kagent installations without command-line or
// terminal dependencies. Embedding applications supply chart and database settings.
package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"k8s.io/client-go/kubernetes"
)

// Command is a subprocess invocation, including any input normally piped to it.
type Command struct {
	Program string
	Args    []string
	Stdin   string
	// SecretValues contains Helm override YAML, supplied through a private file
	// after other values files. It must never appear in process arguments or logs.
	SecretValues string
}

// Runner executes commands, including SecretValues when present. Implementations
// must omit failed-command output when SecretValues could appear in diagnostics.
type Runner interface {
	Run(context.Context, Command) (string, error)
}

// CommandRunner executes Helm and kubectl on the caller's PATH.
type CommandRunner struct{}

var _ Runner = CommandRunner{}

func (CommandRunner) Run(ctx context.Context, command Command) (output string, resultErr error) {
	args := slices.Clone(command.Args)
	if command.SecretValues != "" {
		directory, err := os.MkdirTemp("", "kagent-helm-credentials-")
		if err != nil {
			return "", fmt.Errorf("failed to create Helm credential directory: %w", err)
		}
		defer func() {
			if err := os.RemoveAll(directory); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("failed to remove Helm credential directory: %w", err))
			}
		}()
		path := filepath.Join(directory, "values.yaml")
		if err := os.WriteFile(path, []byte(command.SecretValues), 0o600); err != nil {
			return "", fmt.Errorf("failed to write Helm credential values: %w", err)
		}
		args = append(args, "-f", path)
	}
	cmd := exec.CommandContext(ctx, command.Program, args...)
	cmd.Stdin = strings.NewReader(command.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if command.SecretValues != "" {
			return "", fmt.Errorf("failed to run %s (output omitted because it may contain credentials): %w", command.Program, err)
		}
		return stdout.String(), fmt.Errorf("failed to run %s: %w: %s", command.Program, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// Client owns the I/O used by installation and setup. The caller loads Kubernetes
// configuration and supplies progress reporting; this package does neither.
type Client struct {
	runner     Runner
	kubernetes kubernetes.Interface
}

func NewClient(runner Runner, client kubernetes.Interface) *Client {
	return &Client{runner: runner, kubernetes: client}
}

func (c *Client) kubectl(ctx context.Context, stdin string, args ...string) (string, error) {
	out, err := c.runner.Run(ctx, Command{Program: "kubectl", Args: args, Stdin: stdin})
	if err != nil {
		return out, fmt.Errorf("failed to run kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return out, nil
}
