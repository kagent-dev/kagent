//go:build darwin

package cli

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/abiosoft/ishell/v2"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func DashboardCmd(ctx context.Context, c *ishell.Context) {
	cfg := config.GetCfg(c)
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "kubectl", "-n", cfg.Namespace, "port-forward", "service/kagent", "8082:80")

	defer func() {
		cancel()
		if err := cmd.Wait(); err != nil { // These 2 errors are expected
			if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exec: not started") {
				c.Printf("Error waiting for port-forward to exit: %v\n", err)
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		c.Println("Error port-forwarding kagent:", err)
		return
	}

	// Wait for the port-forward to start
	time.Sleep(1 * time.Second)

	// Open the dashboard in the browser
	openCmd := exec.CommandContext(ctx, "open", "http://localhost:8082")
	if err := openCmd.Run(); err != nil {
		c.Printf("Error opening kagent dashboard: %v\n", err)
	}

	c.Println("kagent dashboard is available at http://localhost:8082")

	// This waits for user input to stop the port-forward
	c.ShowPrompt(false)
	c.Println("Press Enter to stop the port-forward...")
	if _, err := c.ReadLineErr(); err != nil {
		c.Println("Error reading input:", err)
	}
	c.ShowPrompt(true)
}
