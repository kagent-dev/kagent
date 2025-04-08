package cli

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/abiosoft/ishell/v2"
	"github.com/briandowns/spinner"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func InstallCmd(ctx context.Context, c *ishell.Context) {
	cfg := config.GetCfg(c)

	if Version == "dev" {
		c.Println("Installation requires released version of kagent")
		return
	}

	if os.Getenv("OPENAI_API_KEY") == "" {
		c.Println("OPENAI_API_KEY is not set")
		c.Println("Please set the OPENAI_API_KEY environment variable")
		return
	}

	args := []string{
		"upgrade",
		"--install",
		"kagent",
		"oci://ghcr.io/kagent-dev/kagent/helm/kagent",
		"--version",
		Version,
		"--namespace",
		cfg.Namespace,
		"--create-namespace",
		"--wait",
		"--set",
		"openai.apiKey=" + os.Getenv("OPENAI_API_KEY"),
	}
	cmd := exec.CommandContext(ctx, "helm", args...)
	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)
	s.Suffix = " Installing kagent"
	s.Start()

	if byt, err := cmd.CombinedOutput(); err != nil {
		s.Stop()
		c.Println("Error installing kagent: ", string(byt))
		return
	}

	// Create a new context for port-forward
	pfCtx := context.Background()

	portForwardCmd := exec.CommandContext(pfCtx, "kubectl", "-n", cfg.Namespace, "port-forward", "service/kagent", "8081:8081")
	if err := portForwardCmd.Start(); err != nil {
		s.Stop()
		c.Println("Error starting port-forward:", err)
		return
	}

	// Wait for port-forward to be ready
	time.Sleep(2 * time.Second)

	// Check if port-forward is running
	if portForwardCmd.Process == nil {
		s.Stop()
		c.Println("Port-forward failed to start")
		return
	}

	s.Stop()
	c.Println("kagent installed successfully")
}

func UninstallCmd(ctx context.Context, c *ishell.Context) {
	cfg := config.GetCfg(c)
	args := []string{
		"uninstall",
		"kagent",
		"--namespace",
		cfg.Namespace,
	}
	cmd := exec.CommandContext(ctx, "helm", args...)
	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)
	s.Suffix = " Uninstalling kagent"
	s.Start()

	if err := cmd.Run(); err != nil {
		s.Stop()
		c.Println("Error uninstalling kagent:", err)
		return
	}
	s.Stop()
	c.Println("kagent uninstalled successfully")
}
