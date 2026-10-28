// Package install plans and applies kagent installations without command-line or
// terminal dependencies. Embedding applications supply chart and database settings.
package install

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"k8s.io/client-go/kubernetes"
)

// Command is a subprocess invocation, including any input normally piped to it.
type Command struct {
	Program string
	Args    []string
	Stdin   string
}

// Runner executes commands. Successful output is stdout; errors may include stderr.
type Runner interface {
	Run(context.Context, Command) (string, error)
}

// CommandRunner executes Helm and kubectl on the caller's PATH.
type CommandRunner struct{}

var _ Runner = CommandRunner{}

func (CommandRunner) Run(ctx context.Context, command Command) (string, error) {
	cmd := exec.CommandContext(ctx, command.Program, command.Args...)
	cmd.Stdin = strings.NewReader(command.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
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
