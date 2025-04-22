package cli

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/abiosoft/ishell/v2"
	"github.com/briandowns/spinner"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

// installChart installs or upgrades a Helm chart with the given parameters
func installChart(ctx context.Context, chartName string, namespace string, version string, setValues []string, s *spinner.Spinner) (string, error) {
	args := []string{
		"upgrade",
		"--install",
		chartName,
		"oci://ghcr.io/kagent-dev/kagent/helm/" + chartName,
		"--version",
		version,
		"--namespace",
		namespace,
		"--create-namespace",
		"--wait",
	}

	// Add set values if any
	for _, setValue := range setValues {
		args = append(args, "--set", setValue)
	}

	cmd := exec.CommandContext(ctx, "helm", args...)
	if byt, err := cmd.CombinedOutput(); err != nil {
		return string(byt), err
	}
	return "", nil
}

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

	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)

	// First install kagent-crds
	s.Suffix = " Installing kagent-crds"
	s.Start()
	if output, err := installChart(ctx, "kagent-crds", cfg.Namespace, Version, nil, s); err != nil {
		// Check for various CRD existence scenarios, this is to be compatible with
		// original kagent installation that had CRDs installed together with the kagent chart
		if strings.Contains(output, "exists and cannot be imported into the current release") {
			s.Stop()
			c.Println("Warning: CRDs already exist but not managed by helm, you might need to delete them manually to make them fully managed by helm.")
			s.Start()
		} else {
			c.Println("Error installing kagent-crds:", output)
			return
		}
	}

	// Then install kagent
	s.Suffix = " Installing kagent"
	if output, err := installChart(ctx, "kagent", cfg.Namespace, Version, []string{"openai.apiKey=" + os.Getenv("OPENAI_API_KEY")}, s); err != nil {
		c.Println("Error installing kagent:", output)
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
	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)

	// First uninstall kagent
	s.Suffix = " Uninstalling kagent"
	s.Start()

	args := []string{
		"uninstall",
		"kagent",
		"--namespace",
		cfg.Namespace,
	}
	cmd := exec.CommandContext(ctx, "helm", args...)

	if out, err := cmd.CombinedOutput(); err != nil {
		s.Stop()
		// Check if this is because kagent doesn't exist
		output := string(out)
		if strings.Contains(output, "not found") {
			c.Println("Warning: kagent release not found, skipping uninstallation")
		} else {
			c.Println("Error uninstalling kagent:", output)
			return
		}
	}

	// Then uninstall kagent-crds
	s.Suffix = " Uninstalling kagent-crds"

	args = []string{
		"uninstall",
		"kagent-crds",
		"--namespace",
		cfg.Namespace,
	}
	cmd = exec.CommandContext(ctx, "helm", args...)

	if out, err := cmd.CombinedOutput(); err != nil {
		s.Stop()
		// Check if this is because kagent-crds doesn't exist
		output := string(out)
		if strings.Contains(output, "not found") {
			c.Println("Warning: kagent-crds release not found, skipping uninstallation")
		} else {
			c.Println("Error uninstalling kagent-crds:", output)
			return
		}
	}

	s.Stop()
	c.Println("kagent uninstalled successfully")
}
