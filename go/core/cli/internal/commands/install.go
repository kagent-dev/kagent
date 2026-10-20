package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	cliconfig "github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/cli/internal/profiles"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	kagentsetup "github.com/kagent-dev/kagent/go/core/pkg/postgressetup"
)

type InstallCfg struct {
	Profile           string
	SkipDatabaseSetup bool
}

// installChart installs or upgrades a Helm chart with the given parameters
func installChart(ctx context.Context, chartName string, namespace string, registry string, version string, setValues []string, valuesFiles []string, inlineValues string) (string, error) {
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
		// Substrate creates its snapshot bucket in a Job; agents fail to snapshot until it finishes.
		"--wait-for-jobs",
		"--history-max",
		"2",
		"--timeout",
		"5m",
	}

	// Add set values if any
	for _, setValue := range setValues {
		if setValue != "" {
			args = append(args, "--set", setValue)
		}
	}

	if inlineValues != "" {
		args = append(args, "-f", "-")
	}
	for _, file := range valuesFiles {
		args = append(args, "-f", file)
	}
	cmd := exec.CommandContext(ctx, "helm", args...)

	// If a profile is provided, pass the embedded YAML to the stdin of the helm command.
	// Explicit values files override these installation defaults.
	if inlineValues != "" {
		cmd.Stdin = strings.NewReader(inlineValues)
	}

	if byt, err := cmd.CombinedOutput(); err != nil {
		return string(byt), err
	}
	return "", nil
}

func runInstall(ctx context.Context, options connection.Options, cfg *InstallCfg) error {
	if version.Version == "dev" {
		return errors.New("installation requires a released version of kagent")
	}

	if err := checkHelmAvailable(); err != nil {
		return err
	}
	if err := checkKubectlAvailable(); err != nil {
		return err
	}

	settings := cliconfig.FromContext(ctx).GetInstall()
	modelProvider := v1alpha3.ModelProvider(settings.GetDefaultModelProvider())

	apiKeyValue := ""
	if apiKey, ok := providerAPIKey(modelProvider); ok {
		apiKeyValue = apiKey.Get()
		if apiKeyValue == "" {
			return fmt.Errorf("%s is not set; set it, or choose another provider with install.defaultModelProvider in the CLI config", apiKey.Name())
		}
	}

	helmConfig := setupHelmConfig(settings.GetKagent(), modelProvider, apiKeyValue)
	substrateHelmConfig := chartConfig(settings.GetSubstrate())

	// setup profile if provided
	if cfg.Profile = strings.TrimSpace(cfg.Profile); cfg.Profile != "" {
		if !slices.Contains(profiles.Profiles, cfg.Profile) {
			fmt.Fprintf(os.Stderr, "Invalid --profile value (%s), defaulting to minimal\n", cfg.Profile)
			cfg.Profile = profiles.ProfileMinimal
		}

		helmConfig.inlineValues += "\n" + profiles.MinimalProfileYaml
	}

	return install(ctx, &options, helmConfig, substrateHelmConfig, chartConfig(settings.GetPodCertificate()), modelProvider, cfg.SkipDatabaseSetup, settings.GetBundledPostgresImage())
}

// helmConfig is the config for the kagent chart
type helmConfig struct {
	registry string
	version  string
	// values are values which are passed in via --set flags
	values      []string
	valuesFiles []string
	// inlineValues are values which are passed in via stdin (e.g. embedded profile YAML)
	inlineValues string
}

func podCertificateChartValues(values []string, namespace string, includeBundledPostgresClient bool) []string {
	result := make([]string, 0, len(values)+2)
	for _, value := range values {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "image.") || strings.HasPrefix(key, "global.") || strings.HasPrefix(key, "imagePullSecrets") {
			result = append(result, value)
		}
	}
	if includeBundledPostgresClient {
		// One entry per login: Helm's --set would split a comma-separated username list.
		result = append(result,
			fmt.Sprintf("postgresClients[0]=%s/kagent-controller=%s", namespace, kagentsetup.OwnerUser),
			fmt.Sprintf("postgresClients[1]=%s/kagent-controller=%s", namespace, kagentsetup.ReadWriteUser),
		)
	}
	return result
}

// setupHelmConfig sets up the helm config for the kagent chart
// This sets up the general configuration for a helm installation without the profile, which is calculated later based on the installation type (interactive or non-interactive)
func setupHelmConfig(chart *configv1.ChartConfig, modelProvider v1alpha3.ModelProvider, apiKeyValue string) helmConfig {
	// Build Helm values
	helmProviderKey := GetModelProviderHelmValuesKey(modelProvider)
	values := []string{
		fmt.Sprintf("providers.default=%s", helmProviderKey),
		fmt.Sprintf("providers.%s.apiKey=%s", helmProviderKey, apiKeyValue),
	}

	result := chartConfig(chart)
	result.values = values
	result.inlineValues = `controller:
  substrateClientCertificate:
    enabled: true
config:
  substrate:
    apiEndpoint: dns:///api.ate-system.svc:443
    routerUrl: http://atenet-router.ate-system.svc:80
`
	return result
}

func chartConfig(chart *configv1.ChartConfig) helmConfig {
	return helmConfig{registry: strings.TrimRight(chart.GetRepository(), "/") + "/", version: chart.GetVersion(), valuesFiles: slices.Clone(chart.GetValuesFiles())}
}

// install installs the Substrate and Kagent releases.
func install(ctx context.Context, cfg *connection.Options, helmConfig, substrateHelmConfig, podCertificateHelmConfig helmConfig, modelProvider v1alpha3.ModelProvider, skipDatabaseSetup bool, postgresImage string) error {
	substrateHelmConfig.inlineValues = substrateInstallValues(cfg.Namespace)
	helmConfig.inlineValues += fmt.Sprintf("\nsubstrateWorkerPool:\n  create: true\n  workerImage: %s\n", strconv.Quote("ghcr.io/kagent-dev/substrate/ateworker-gvisor:v"+strings.TrimPrefix(substrateHelmConfig.version, "v")))

	// spinner for installation progress
	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)

	// First install kagent-crds
	s.Suffix = " Installing kagent-crds from " + helmConfig.registry
	defer s.Stop()
	s.Start()
	if output, err := installChart(ctx, "kagent-crds", cfg.Namespace, helmConfig.registry, helmConfig.version, nil, helmConfig.valuesFiles, ""); err != nil {
		// Helm creates the namespaces later steps deploy into, so CRDs it does not
		// own, such as those left by an old install, stop the install.
		if strings.Contains(output, "exists and cannot be imported into the current release") {
			return fmt.Errorf("install kagent-crds: Kagent CRDs from a previous installation exist outside Helm. Run `kagent uninstall` or delete them, then retry: %s", strings.TrimSpace(output))
		}
		return fmt.Errorf("install kagent-crds: %s", strings.TrimSpace(output))
	}

	s.Suffix = " Installing substrate-crds from " + substrateHelmConfig.registry
	if output, err := installChart(ctx, "substrate-crds", substrateNamespace, substrateHelmConfig.registry, substrateHelmConfig.version, nil, substrateHelmConfig.valuesFiles, ""); err != nil {
		return fmt.Errorf("install substrate-crds: %s", strings.TrimSpace(output))
	}

	s.Suffix = " Preparing Substrate prerequisites"
	if err := prepareSubstrate(ctx, cfg.Namespace); err != nil {
		return fmt.Errorf("prepare Substrate prerequisites: %w", err)
	}

	s.Suffix = " Installing substrate-podcert from " + podCertificateHelmConfig.registry
	if output, err := installChart(ctx, "substrate-podcert", podCertificateNamespace, podCertificateHelmConfig.registry, podCertificateHelmConfig.version, podCertificateChartValues(substrateHelmConfig.values, cfg.Namespace, !skipDatabaseSetup), podCertificateHelmConfig.valuesFiles, ""); err != nil {
		return fmt.Errorf("install substrate-podcert: %s", strings.TrimSpace(output))
	}

	if !skipDatabaseSetup {
		s.Suffix = " Preparing bundled PostgreSQL"
		if err := prepareBundledPostgres(ctx, cfg.Namespace, postgresImage); err != nil {
			return fmt.Errorf("prepare bundled PostgreSQL: %w", err)
		}
		substrateHelmConfig.values = append(substrateHelmConfig.values, "postgres.clientCertificates.enabled=true")
	}

	s.Suffix = " Installing substrate from " + substrateHelmConfig.registry
	if output, err := installChart(ctx, "substrate", substrateNamespace, substrateHelmConfig.registry, substrateHelmConfig.version, substrateHelmConfig.values, substrateHelmConfig.valuesFiles, substrateHelmConfig.inlineValues); err != nil {
		return fmt.Errorf("install substrate: %s", strings.TrimSpace(output))
	}

	// Update status
	// Removing api key(s) from printed values
	redactedValues := []string{}
	for _, value := range helmConfig.values {
		if strings.Contains(value, "apiKey=") {
			// Split the value by "=" and replace the second part with "********"
			// This follows the format we're following to define the api key values in the helm chart (providers.{provider}.apiKey=...)
			parts := strings.Split(value, "=")
			redactedValues = append(redactedValues, parts[0]+"=********")
		} else {
			redactedValues = append(redactedValues, value)
		}
	}

	if !skipDatabaseSetup {
		helmConfig.values = append(helmConfig.values, "database.postgres.clientCertificate.enabled=true")
	}
	s.Suffix = fmt.Sprintf(" Installing kagent [%s] Using %s:%s %v", modelProvider, helmConfig.registry, helmConfig.version, redactedValues)
	if output, err := installChart(ctx, "kagent", cfg.Namespace, helmConfig.registry, helmConfig.version, helmConfig.values, helmConfig.valuesFiles, helmConfig.inlineValues); err != nil {
		return fmt.Errorf("install kagent: %s", strings.TrimSpace(output))
	}

	// Stop the spinner completely before printing the success message
	s.Stop()
	fmt.Fprintln(os.Stdout, "kagent installed successfully")

	// The port-forward only proves the API is reachable; later commands open their own.
	pf, err := connection.NewPortForward(ctx, cfg, cfg.APIURL)
	if err != nil {
		return fmt.Errorf("start port-forward: %w", err)
	}
	pf.Stop()
	return nil
}

// deleteCRDs manually deletes Kubernetes CRDs for kagent
// This is a workaround for the fact that helm doesn't delete CRDs automatically
func deleteCRDs(ctx context.Context) error {
	resources := []string{
		"agents",
		"agenttemplates",
		"harnesses",
		"modelconfigs",
		"modelproviderconfigs",
		"remotemcpservers",
	}

	var deleteErrors []string

	for _, resource := range resources {
		crd := v1alpha3.GroupVersion.WithResource(resource).GroupResource().String()
		deleteCmd := exec.CommandContext(ctx, "kubectl", "delete", "crd", crd)
		if out, err := deleteCmd.CombinedOutput(); err != nil {
			if !strings.Contains(string(out), "not found") {
				errMsg := fmt.Sprintf("Error deleting CRD %s: %s", crd, string(out))
				fmt.Fprintln(os.Stderr, errMsg)
				deleteErrors = append(deleteErrors, errMsg)
			}
		} else {
			fmt.Fprintf(os.Stdout, "Successfully deleted CRD %s\n", crd)
		}
	}

	if len(deleteErrors) > 0 {
		return fmt.Errorf("failed to delete some CRDs: %s", strings.Join(deleteErrors, "; "))
	}
	return nil
}

func runUninstall(ctx context.Context, namespace string) {
	// Check if helm is available
	if err := checkHelmAvailable(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)

	// First uninstall kagent
	s.Suffix = " Uninstalling kagent"
	s.Start()

	args := []string{
		"uninstall",
		"kagent",
		"--namespace",
		namespace,
	}
	cmd := exec.CommandContext(ctx, "helm", args...)

	if out, err := cmd.CombinedOutput(); err != nil {
		s.Stop()
		// Check if this is because kagent doesn't exist
		output := string(out)
		if strings.Contains(output, "not found") {
			fmt.Fprintln(os.Stderr, "Warning: kagent release not found, skipping uninstallation")
		} else {
			fmt.Fprintln(os.Stderr, "Error uninstalling kagent:", output)
			return
		}
	}

	// Then uninstall kagent-crds
	s.Suffix = " Uninstalling kagent-crds"

	args = []string{
		"uninstall",
		"kagent-crds",
		"--namespace",
		namespace,
	}
	cmd = exec.CommandContext(ctx, "helm", args...)

	if out, err := cmd.CombinedOutput(); err != nil {
		s.Stop()
		// Check if this is because kagent-crds doesn't exist
		output := string(out)
		if strings.Contains(output, "not found") {
			fmt.Fprintln(os.Stderr, "Warning: kagent-crds release not found, try to delete crds directly")
			// delete the CRDs directly, this is a workaround for the fact that helm doesn't delete CRDs
			if err := deleteCRDs(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "Error deleting CRDs:", err)
				return
			}
		} else {
			fmt.Fprintln(os.Stderr, "Error uninstalling kagent-crds:", output)
			return
		}
	}

	s.Stop()
	fmt.Fprintln(os.Stdout, "\nkagent uninstalled successfully")
}

func checkHelmAvailable() error {
	_, err := exec.LookPath("helm")
	if err != nil {
		return fmt.Errorf("helm not found in PATH. Please install helm first: https://helm.sh/docs/intro/install/")
	}
	return nil
}

func checkKubectlAvailable() error {
	_, err := exec.LookPath("kubectl")
	if err != nil {
		return fmt.Errorf("kubectl not found in PATH. Please install kubectl first: https://kubernetes.io/docs/tasks/tools/")
	}
	return nil
}

// NewInstallCmd constructs the kagent install command.
func NewInstallCmd() *cobra.Command {
	cfg := &InstallCfg{}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install kagent",
		Long:  `Install kagent`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			return runInstall(cmd.Context(), options, cfg)
		},
	}
	cmd.Flags().StringVar(&cfg.Profile, "profile", "", "Installation profile (minimal)")
	cmd.Flags().BoolVar(&cfg.SkipDatabaseSetup, "skip-database-setup", false, "Skip bundled PostgreSQL deployment and initialization")
	_ = cmd.RegisterFlagCompletionFunc("profile", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return profiles.Profiles, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// NewUninstallCmd constructs the kagent uninstall command.
func NewUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall kagent",
		Long:  `Uninstall kagent`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			runUninstall(cmd.Context(), options.Namespace)
			return nil
		},
	}
}

// substrateInstallValues grants the default controller's pod-certificate identity
// access to the Substrate control plane. Explicit values files override defaults.
func substrateInstallValues(namespace string) string {
	return fmt.Sprintf(`ateApi:
  authorization:
    bootstrapOwners:
      - %s
credentialProvider:
  namespacePolicies:
    - atespace: %s
      allowedNamespaces: [%s]
`, strconv.Quote("spiffe://cluster.local/ns/"+namespace+"/sa/kagent-controller"), strconv.Quote(namespace), strconv.Quote(namespace))
}
