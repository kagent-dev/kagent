package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/abiosoft/ishell/v2"
	"github.com/briandowns/spinner"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

// installChart installs or upgrades a Helm chart with the given parameters
func installChart(ctx context.Context, chartName string, namespace string, registry string, version string, setValues []string, s *spinner.Spinner) (string, error) {
	args := []string{
		"upgrade",
		"--install",
		chartName,
		registry + chartName,
		"--version",
		version,
		"--namespace",
		namespace,
		"--create-namespace",
		"--wait",
		"--history-max",
		"2",
		"--timeout",
		"5m",
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

	// get model provider from KAGENT_DEFAULT_MODEL_PROVIDER environment variable or use DefaultModelProvider
	modelProvider := GetModelProvider()

	//if model provider is openai, check if the api key is set
	apiKeyName := GetProviderAPIKey(modelProvider)
	apiKeyValue := os.Getenv(apiKeyName)

	if apiKeyName != "" && apiKeyValue == "" {
		c.Printf("%s is not set", apiKeyName)
		c.Printf("Please set the %s environment variable", apiKeyName)
		return
	}

	// Build Helm values
	helmProviderKey := GetModelProviderHelmValuesKey(modelProvider)
	values := []string{
		fmt.Sprintf("providers.default=%s", helmProviderKey),
		fmt.Sprintf("providers.%s.apiKey=%s", helmProviderKey, apiKeyValue),
	}

	//allow user to set the helm registry and version
	helmRegistry := GetEnvVarWithDefault(KAGENT_HELM_REPO, DefaultHelmOciRegistry)
	helmVersion := GetEnvVarWithDefault(KAGENT_HELM_VERSION, Version)

	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)

	// First install kagent-crds
	s.Suffix = " Installing kagent-crds from " + helmRegistry
	defer s.Stop()
	s.Start()
	if output, err := installChart(ctx, "kagent-crds", cfg.Namespace, helmRegistry, helmVersion, nil, s); err != nil {
		// Check for various CRD existence scenarios, this is to be compatible with
		// original kagent installation that had CRDs installed together with the kagent chart
		if strings.Contains(output, "exists and cannot be imported into the current release") {
			s.Stop()
			c.Println("Warning: CRDs exist but aren't managed by helm.")
			c.Println("Run `uninstall` or delete them manually to")
			c.Println("ensure they're fully managed on next install.")
			s.Start()
		} else {
			c.Println("\nError installing kagent-crds:", output)
			return
		}
	}

	// Update status
	s.Suffix = fmt.Sprintf(" Installing kagent [%s] Using %s:%s", modelProvider, helmRegistry, helmVersion)
	if output, err := installChart(ctx, "kagent", cfg.Namespace, helmRegistry, helmVersion, values, s); err != nil {
		c.Println("\nError installing kagent:", output)
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
	time.Sleep(3 * time.Second)

	// Check if port-forward is running
	if portForwardCmd.Process == nil {
		s.Stop()
		c.Println("Port-forward failed to start")
		return
	}
	c.Println("\nkagent installed successfully")
}

// deleteCRDs manually deletes Kubernetes CRDs for kagent
// This is a workaround for the fact that helm doesn't delete CRDs automatically
func deleteCRDs(ctx context.Context, c *ishell.Context) error {
	crds := []string{
		"agents.kagent.dev",
		"modelconfigs.kagent.dev",
		"teams.kagent.dev",
		"toolservers.kagent.dev",
	}

	var deleteErrors []string

	for _, crd := range crds {
		deleteCmd := exec.CommandContext(ctx, "kubectl", "delete", "crd", crd)
		if out, err := deleteCmd.CombinedOutput(); err != nil {
			if !strings.Contains(string(out), "not found") {
				errMsg := fmt.Sprintf("Error deleting CRD %s: %s", crd, string(out))
				c.Printf(errMsg)
				deleteErrors = append(deleteErrors, errMsg)
			}
		} else {
			c.Printf("Successfully deleted CRD %s\n", crd)
		}
	}

	if len(deleteErrors) > 0 {
		return fmt.Errorf("failed to delete some CRDs: %s", strings.Join(deleteErrors, "; "))
	}
	return nil
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
			c.Println("Warning: kagent-crds release not found, try to delete crds directly")
			// delete the CRDs directly, this is a workaround for the fact that helm doesn't delete CRDs
			if err := deleteCRDs(ctx, c); err != nil {
				c.Println("Error deleting CRDs:", err)
				return
			}
		} else {
			c.Println("Error uninstalling kagent-crds:", output)
			return
		}
	}

	s.Stop()
	c.Println("kagent uninstalled successfully")
}
