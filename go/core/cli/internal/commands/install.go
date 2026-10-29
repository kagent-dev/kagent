package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	cliconfig "github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/cli/internal/profiles"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	installer "github.com/kagent-dev/kagent/go/core/pkg/install"
)

const kagentChartName = "kagent"

type InstallCfg struct {
	Profile           string
	SkipDatabaseSetup bool
}

func newInstallClient() (*installer.Client, error) {
	for _, tool := range []string{"helm", "kubectl"} {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, fmt.Errorf("%s not found in PATH; install it first", tool)
		}
	}
	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load Kubernetes configuration: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	return installer.NewClient(installer.CommandRunner{}, client), nil
}

func chartConfig(chart *configv1.ChartConfig, name string) installer.Chart {
	return installer.Chart{Name: name, Repository: chart.GetRepository(), Version: chart.GetVersion(), ValuesFiles: slices.Clone(chart.GetValuesFiles())}
}

func runInstall(cmd *cobra.Command, options connection.Options, cfg *InstallCfg) (resultErr error) {
	if version.Version == "dev" {
		return errors.New("installation requires a released version of kagent")
	}
	settings := cliconfig.FromContext(cmd.Context()).GetInstall()
	modelProvider := v1alpha3.ModelProvider(settings.GetDefaultModelProvider())
	apiKeyValue := ""
	if apiKey, ok := providerAPIKey(modelProvider); ok {
		apiKeyValue = apiKey.Get()
		if apiKeyValue == "" {
			return fmt.Errorf("%s is not set; set it, or choose another provider with install.defaultModelProvider in the CLI config", apiKey.Name())
		}
	}
	kagent := chartConfig(settings.GetKagent(), kagentChartName)
	if profile := strings.TrimSpace(cfg.Profile); profile != "" {
		if !slices.Contains(profiles.Profiles, profile) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Invalid --profile value (%s), defaulting to minimal\n", profile)
		}
		directory, err := os.MkdirTemp("", "kagent-install-profile-")
		if err != nil {
			return fmt.Errorf("failed to create installation profile directory: %w", err)
		}
		defer func() {
			if err := os.RemoveAll(directory); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("failed to clean installation profile directory: %w", err))
			}
		}()
		path := filepath.Join(directory, "values.yaml")
		if err := os.WriteFile(path, []byte(profiles.MinimalProfileYaml), 0o600); err != nil {
			return fmt.Errorf("failed to write installation profile values: %w", err)
		}
		kagent.ValuesFiles = append([]string{path}, kagent.ValuesFiles...)
	}
	client, err := newInstallClient()
	if err != nil {
		return err
	}
	plan, err := client.Plan(cmd.Context(), installer.Config{
		Namespace:      options.Namespace,
		Kagent:         kagent,
		KagentCRDs:     chartConfig(settings.GetKagent(), kagentChartName+"-crds"),
		Substrate:      chartConfig(settings.GetSubstrate(), "substrate"),
		SubstrateCRDs:  chartConfig(settings.GetSubstrate(), "substrate-crds"),
		PodCertificate: chartConfig(settings.GetPodCertificate(), "substrate-podcert"),
		ModelProvider:  string(modelProvider), APIKey: apiKeyValue,
		WorkerImage:       "ghcr.io/kagent-dev/substrate/ateworker-gvisor:v" + strings.TrimPrefix(settings.GetSubstrate().GetVersion(), "v"),
		SkipDatabaseSetup: cfg.SkipDatabaseSetup,
		Database:          installer.Postgres{Image: settings.GetBundledPostgresImage()},
	})
	if err != nil {
		return err
	}
	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)
	s.Writer = cmd.OutOrStdout()
	s.Start()
	defer s.Stop()
	if err := client.Apply(cmd.Context(), plan, func(message string) { s.Suffix = " " + message }); err != nil {
		return err
	}
	s.Stop()
	fmt.Fprintln(cmd.OutOrStdout(), "kagent installed successfully")
	pf, err := connection.NewPortForward(cmd.Context(), &options, options.APIURL)
	if err != nil {
		return fmt.Errorf("start port-forward: %w", err)
	}
	pf.Stop()
	return nil
}

func runUninstall(cmd *cobra.Command, namespace, postgresImage string) error {
	client, err := newInstallClient()
	if err != nil {
		return err
	}
	s := spinner.New(spinner.CharSets[35], 100*time.Millisecond)
	s.Writer = cmd.OutOrStdout()
	s.Start()
	defer s.Stop()
	result, err := client.Uninstall(cmd.Context(), namespace, postgresImage, func(message string) { s.Suffix = " " + message })
	s.Stop()
	for _, release := range result.MissingReleases {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s release not found, skipping uninstallation\n", release)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nkagent uninstalled successfully")
	fmt.Fprintf(cmd.OutOrStdout(), "\nThe %s namespace stays, with anything else in it. To delete it and everything it holds:\n  kubectl delete namespace %s\n", namespace, namespace)
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
			return runInstall(cmd, options, cfg)
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
			return runUninstall(cmd, options.Namespace, cliconfig.FromContext(cmd.Context()).GetInstall().GetBundledPostgresImage())
		},
	}
}

// providerAPIKey selects the first explicitly set credential, or the fallback
// name used in the missing-credential error. An explicitly empty key stays empty.
func providerAPIKey(provider v1alpha3.ModelProvider) (env.StringVar, bool) {
	keys := installer.ProviderAPIKeys(provider)
	for _, key := range keys {
		if _, set := key.Lookup(); set {
			return key, true
		}
	}
	if len(keys) == 0 {
		return env.StringVar{}, false
	}
	return keys[len(keys)-1], true
}
