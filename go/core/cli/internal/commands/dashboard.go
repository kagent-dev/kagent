package commands

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
)

const dashboardURL = "http://localhost:8082"

// NewDashboardCmd constructs the kagent dashboard command.
func NewDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Open the kagent dashboard",
		Long:  `Open the kagent dashboard`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			return runDashboard(cmd, options.Namespace)
		},
	}
}

func openDashboard(ctx context.Context) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", dashboardURL).Run()
	case "windows":
		return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", dashboardURL).Run()
	default:
		return exec.CommandContext(ctx, "xdg-open", dashboardURL).Run()
	}
}

func runDashboard(cmd *cobra.Command, namespace string) error {
	ctx := cmd.Context()
	forwardCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	forward := exec.CommandContext(forwardCtx, "kubectl", "-n", namespace, "port-forward", "service/kagent-ui", "8082:8080")
	var stderr bytes.Buffer
	forward.Stderr = &stderr
	stdout, err := forward.StdoutPipe()
	if err != nil {
		return fmt.Errorf("read kubectl port-forward output: %w", err)
	}
	if err := forward.Start(); err != nil {
		return fmt.Errorf("start kubectl port-forward: %w", err)
	}

	ready, done := make(chan struct{}), make(chan struct{})
	var forwardErr error
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stdout)
		reportedReady := false
		for scanner.Scan() {
			if !reportedReady && strings.HasPrefix(scanner.Text(), "Forwarding from ") {
				close(ready)
				reportedReady = true
			}
		}
		if err := scanner.Err(); err != nil {
			cancel()
			forwardErr = fmt.Errorf("read kubectl port-forward output: %w", err)
		}
		if err := forward.Wait(); err != nil && forwardErr == nil {
			forwardErr = fmt.Errorf("kubectl port-forward: %w", err)
			if details := strings.TrimSpace(stderr.String()); details != "" {
				forwardErr = fmt.Errorf("%w: %s", forwardErr, details)
			}
		}
	}()
	defer func() {
		cancel()
		<-done
	}()

	select {
	case <-ready:
	case <-done:
		if ctx.Err() != nil {
			return nil
		}
		if forwardErr != nil {
			return forwardErr
		}
		return errors.New("kubectl port-forward exited before the dashboard was ready")
	case <-ctx.Done():
		return nil
	}

	if err := openDashboard(ctx); err != nil && ctx.Err() == nil {
		cmd.PrintErrf("Error opening kagent dashboard: %v\n", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "kagent dashboard is available at %s\n", dashboardURL)
	fmt.Fprintln(cmd.OutOrStdout(), "Press the Enter Key to stop the port-forward...")
	enter := make(chan struct{})
	in := cmd.InOrStdin()
	go func() {
		_, _ = bufio.NewReader(in).ReadString('\n')
		close(enter)
	}()
	select {
	case <-done:
		if ctx.Err() == nil {
			return forwardErr
		}
	case <-enter:
	case <-ctx.Done():
	}
	return nil
}
