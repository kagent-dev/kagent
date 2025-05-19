//go:build darwin

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func DashboardCmd(ctx context.Context, cfg *config.Config) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "kubectl", "-n", cfg.Namespace, "port-forward", "service/kagent", "8082:80")

	defer func() {
		cancel()
		if err := cmd.Wait(); err != nil { // These 2 errors are expected
			if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exec: not started") {
				fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error port-forwarding kagent: %v\n", err)
		return
	}

	// Wait for the port-forward to start
	time.Sleep(1 * time.Second)

	// Open the dashboard in the browser
	openCmd := exec.CommandContext(ctx, "open", "http://localhost:8082")
	if err := openCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error opening kagent dashboard: %v\n", err)
	}

	fmt.Fprintln(os.Stdout, "kagent dashboard is available at http://localhost:8082")

	fmt.Println("Press the Enter Key to stop the port-forward...")
	fmt.Scanln() // wait for Enter Key
}
