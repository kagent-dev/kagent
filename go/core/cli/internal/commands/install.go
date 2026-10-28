package commands

import (
	"context"
	"encoding/json"
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
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"

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

	args = append(args, helmValueArgs(setValues, valuesFiles, inlineValues)...)
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

func podCertificateChartValues(values []string, namespace, serviceAccount string, includeBundledPostgresClient bool) []string {
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
			fmt.Sprintf("postgresClients[0]=%s/%s=%s", namespace, serviceAccount, kagentsetup.OwnerUser),
			fmt.Sprintf("postgresClients[1]=%s/%s=%s", namespace, serviceAccount, kagentsetup.ReadWriteUser),
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
	helmConfig.inlineValues += fmt.Sprintf("\nsubstrateWorkerPool:\n  create: true\n  workerImage: %s\n", strconv.Quote("ghcr.io/kagent-dev/substrate/ateworker-gvisor:v"+strings.TrimPrefix(substrateHelmConfig.version, "v")))

	if !skipDatabaseSetup {
		helmConfig.values = append(helmConfig.values, "database.postgres.clientCertificate.enabled=true")
	}
	serviceAccount, err := controllerServiceAccount(ctx, cfg.Namespace, helmConfig)
	if err != nil {
		return err
	}
	substrateHelmConfig.inlineValues = substrateInstallValues(cfg.Namespace, serviceAccount)

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
			return fmt.Errorf("install kagent-crds: Kagent CRDs from a previous installation exist outside Helm. Delete them, then retry: %s", strings.TrimSpace(output))
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
	if output, err := installChart(ctx, "substrate-podcert", podCertificateNamespace, podCertificateHelmConfig.registry, podCertificateHelmConfig.version, podCertificateChartValues(substrateHelmConfig.values, cfg.Namespace, serviceAccount, !skipDatabaseSetup), podCertificateHelmConfig.valuesFiles, ""); err != nil {
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

// uninstallChart removes a Helm release and reports whether it existed, so a
// rerun after an interrupted uninstall skips the releases already gone.
func uninstallChart(ctx context.Context, release, namespace string) (bool, error) {
	out, err := exec.CommandContext(ctx, "helm", "uninstall", release, "--namespace", namespace, "--wait").CombinedOutput()
	if err == nil {
		return true, nil
	}
	if strings.Contains(string(out), "not found") {
		return false, nil
	}
	return false, fmt.Errorf("uninstall %s: %s", release, strings.TrimSpace(string(out)))
}

// kagentReleaseNamespaces returns the namespaces holding a kagent Helm release.
func kagentReleaseNamespaces(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "helm", "list", "--all-namespaces", "--filter", "^kagent$", "--output", "json").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("list kagent Helm releases: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("list kagent Helm releases: %w", err)
	}
	var releases []struct {
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(out, &releases); err != nil {
		return nil, fmt.Errorf("decode kagent Helm releases: %w", err)
	}
	namespaces := make([]string, 0, len(releases))
	for _, release := range releases {
		namespaces = append(namespaces, release.Namespace)
	}
	return namespaces, nil
}

// deleteServed deletes objects and waits for their finalizers. A resource the
// cluster no longer serves has nothing to delete, as on a rerun after its CRD
// is gone.
func deleteServed(ctx context.Context, resource string, args ...string) error {
	out, err := kubectl(ctx, "", append(append([]string{"delete", resource}, args...), "--ignore-not-found", "--timeout=5m")...)
	if err != nil && !strings.Contains(string(out), "doesn't have a resource type") {
		return err
	}
	return nil
}

// substrateTrustBundles are the cluster-scoped ClusterTrustBundles Substrate's
// controllers publish. Kubernetes requires a bundle whose name has a signer
// prefix to carry that signer, so the names alone identify them.
var substrateTrustBundles = []string{
	"podidentity.podcert.ate.dev:identity:primary-bundle",
	"servicedns.podcert.ate.dev:identity:primary-bundle",
	"postgres.podcert.ate.dev:identity:primary-bundle",
	"egress-mitm.ate.dev:mitm:primary-bundle",
}

// runUninstall removes what install creates, in the reverse of install's order.
// Install supports one kagent per cluster and owns Substrate, the CRDs, and the
// bundled database, so uninstall removes them too. Each step skips what is
// already gone, so running uninstall again finishes an interrupted one.
func runUninstall(ctx context.Context, namespace, postgresImage string) error {
	if err := checkHelmAvailable(); err != nil {
		return err
	}
	if err := checkKubectlAvailable(); err != nil {
		return err
	}
	// Removing Substrate from under a kagent installed elsewhere strands its
	// WorkerPool, so a mismatched --namespace stops before anything is deleted.
	releaseNamespaces, err := kagentReleaseNamespaces(ctx)
	if err != nil {
		return err
	}
	if len(releaseNamespaces) > 0 && !slices.Contains(releaseNamespaces, namespace) {
		return fmt.Errorf("kagent is installed in namespace %s, not %s; rerun with --namespace %s", strings.Join(releaseNamespaces, ", "), namespace, releaseNamespaces[0])
	}

	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)
	defer s.Stop()
	s.Start()
	release := func(name, releaseNamespace string) error {
		s.Suffix = " Uninstalling " + name
		found, err := uninstallChart(ctx, name, releaseNamespace)
		if err == nil && !found {
			s.Stop()
			fmt.Fprintf(os.Stderr, "Warning: %s release not found, skipping uninstallation\n", name)
			s.Start()
		}
		return err
	}

	// The kagent controller finalizes SandboxTemplates, so they go while it runs.
	// Otherwise deleting their CRD waits forever.
	s.Suffix = " Deleting sandbox templates"
	if err := deleteServed(ctx, "sandboxtemplates.api.kagent.dev", "--all", "--all-namespaces"); err != nil {
		return fmt.Errorf("delete sandbox templates: %w", err)
	}
	// kagent goes before Substrate. Its release holds the Substrate WorkerPool,
	// and Helm can delete that only while substrate-crds still serves the kind.
	if err := release("kagent", namespace); err != nil {
		return err
	}
	// atecontroller releases a SandboxConfig once no WorkerPool uses it. With the
	// WorkerPool gone, the configs go while atecontroller still runs.
	s.Suffix = " Deleting Substrate sandbox configs"
	if err := deleteServed(ctx, "sandboxconfigs.ate.dev", "--all"); err != nil {
		return fmt.Errorf("delete Substrate sandbox configs: %w", err)
	}
	if err := release("substrate", substrateNamespace); err != nil {
		return err
	}
	bundled, err := bundledPostgresExists(ctx, namespace)
	if err != nil {
		return err
	}
	if bundled {
		s.Suffix = " Deleting bundled PostgreSQL"
		if err := deleteBundledPostgres(ctx, namespace, postgresImage); err != nil {
			return err
		}
	}
	if err := release(podCertificateRelease, podCertificateNamespace); err != nil {
		return err
	}
	// The CRDs go only through their releases. A list of CRD names falls behind
	// the chart, so CRDs applied outside Helm stay.
	if err := release("substrate-crds", substrateNamespace); err != nil {
		return err
	}
	if err := release("kagent-crds", namespace); err != nil {
		return err
	}

	// Install labels the kagent namespace so Substrate's API admits it. The
	// namespace stays, so only the label goes. A null merge patch is a no-op when
	// the label is absent, so the only "not found" is a namespace that is gone.
	s.Suffix = " Removing the Substrate API access label"
	if out, err := kubectl(ctx, "", "patch", "namespace", namespace, "--type=merge",
		"-p", `{"metadata":{"labels":{"ate.dev/api-access":null}}}`); err != nil && !strings.Contains(string(out), "not found") {
		return fmt.Errorf("remove the Substrate API access label from namespace %s: %w", namespace, err)
	}

	// The Substrate prerequisites live outside any release, in these namespaces.
	// They go after substrate-crds, whose release record ate-system holds. The
	// kagent namespace stays. Helm creates it without recording ownership, and
	// --namespace can name a namespace that other workloads share.
	namespaces := []string{podCertificateNamespace, substrateNamespace}
	s.Suffix = " Deleting Substrate namespaces"
	if _, err := kubectl(ctx, "", append(append([]string{"delete", "namespace"}, namespaces...), "--ignore-not-found")...); err != nil {
		return fmt.Errorf("delete Substrate namespaces: %w", err)
	}
	// The controllers republish their trust bundles, so the bundles go once the
	// namespace deletes above have waited the controllers out.
	s.Suffix = " Deleting Substrate trust bundles"
	if err := deleteServed(ctx, "clustertrustbundles.certificates.k8s.io", substrateTrustBundles...); err != nil {
		return fmt.Errorf("delete Substrate trust bundles: %w", err)
	}

	s.Stop()
	fmt.Fprintln(os.Stdout, "\nkagent uninstalled successfully")
	fmt.Fprintf(os.Stdout, "\nThe %s namespace stays, with anything else in it. To delete it and everything it holds:\n  kubectl delete namespace %s\n", namespace, namespace)
	return nil
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
		Long: `Uninstall kagent: the kagent and Substrate Helm releases, their CRDs, the
bundled PostgreSQL and its data, and the ate-system and
podcertificate-controller-system namespaces. Removing the CRDs deletes every
resource created from them, such as agents and model configs.

A database that kagent install did not deploy, such as one used with
--skip-database-setup, stays. The kagent namespace stays.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			return runUninstall(cmd.Context(), options.Namespace, cliconfig.FromContext(cmd.Context()).GetInstall().GetBundledPostgresImage())
		},
	}
}

// substrateInstallValues configures per-template snapshot storage and grants the
// controller access to Substrate. Explicit values files override defaults.
func substrateInstallValues(namespace, serviceAccount string) string {
	return fmt.Sprintf(`preview:
  - PerTemplateSnapshotLocation
ateApi:
  authorization:
    bootstrapOwners:
      - %s
credentialProvider:
  namespacePolicies:
    - atespace: %s
      allowedNamespaces: [%s]
`, strconv.Quote("spiffe://cluster.local/ns/"+namespace+"/sa/"+serviceAccount), strconv.Quote(namespace), strconv.Quote(namespace))
}

// controllerServiceAccount renders the same chart and overrides as the install so
// bootstrap grants follow the chart's naming rules and operator-managed accounts.
func controllerServiceAccount(ctx context.Context, namespace string, c helmConfig) (string, error) {
	args := []string{"template", "kagent", c.registry + "kagent", "--version", c.version,
		"--namespace", namespace, "--show-only", "templates/controller-deployment.yaml"}
	args = append(args, helmValueArgs(c.values, c.valuesFiles, c.inlineValues)...)
	cmd := exec.CommandContext(ctx, "helm", args...)
	if c.inlineValues != "" {
		cmd.Stdin = strings.NewReader(c.inlineValues)
	}
	manifest, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("render controller deployment: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("render controller deployment: %w", err)
	}
	var deployment appsv1.Deployment
	if err := yaml.Unmarshal(manifest, &deployment); err != nil {
		return "", fmt.Errorf("decode controller deployment: %w", err)
	}
	if deployment.Kind != "Deployment" || deployment.Spec.Template.Spec.ServiceAccountName == "" {
		return "", errors.New("rendered controller deployment has no service account")
	}
	return deployment.Spec.Template.Spec.ServiceAccountName, nil
}

// helmValueArgs keeps rendering and installation on the same values precedence.
func helmValueArgs(values, valuesFiles []string, inlineValues string) []string {
	var args []string
	for _, value := range values {
		if value != "" {
			args = append(args, "--set", value)
		}
	}
	if inlineValues != "" {
		args = append(args, "-f", "-")
	}
	for _, file := range valuesFiles {
		args = append(args, "-f", file)
	}
	return args
}
