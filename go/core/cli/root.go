package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/kagent-dev/kagent/go/core/cli/internal/commands"
	agentinstancecli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/agentinstance"
	agenttemplatecli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/agenttemplate"
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands/envdoc"
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands/mcp"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui"
	dbcli "github.com/kagent-dev/kagent/go/core/pkg/cli/db"
	dbmigrate "github.com/kagent-dev/kagent/go/core/pkg/cli/db/migrate"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Root creates a fresh kagent command tree.
func Root() *cobra.Command {
	connectionOptions := connection.DefaultOptions()
	cfg := &connectionOptions
	outputFormat := "table"
	rootCmd := &cobra.Command{
		Use:           "kagent",
		Short:         "kagent is a CLI for kagent",
		Long:          "kagent is a CLI for kagent",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInteractive(cmd, cfg)
		},
	}
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentURL, "kagent-url", cfg.KAgentURL, "KAgent REST URL")
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentGRPCURL, "kagent-grpc-url", cfg.KAgentGRPCURL, "KAgent gRPC target")
	rootCmd.PersistentFlags().BoolVar(&cfg.KAgentGRPCTLS, "kagent-grpc-tls", cfg.KAgentGRPCTLS, "Use TLS for KAgent gRPC")
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentGRPCCAFile, "kagent-grpc-ca-file", cfg.KAgentGRPCCAFile, "CA certificate file for KAgent gRPC")
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentGRPCServerName, "kagent-grpc-server-name", cfg.KAgentGRPCServerName, "TLS server name for KAgent gRPC")
	rootCmd.PersistentFlags().StringVarP(&cfg.Namespace, "namespace", "n", cfg.Namespace, "Namespace")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output-format", "o", outputFormat, "Output format")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", cfg.Verbose, "Verbose output")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "Timeout")
	rootCmd.PersistentFlags().StringVar(&cfg.UserID, "user-id", cfg.UserID, "Caller identity used to select the server-side data partition")
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get a kagent resource",
		Long:  `Get a kagent resource`,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("resource type is required")
		},
	}
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a kagent resource",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("resource type is required")
		},
	}
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a kagent resource",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("resource type is required")
		},
	}

	// Add subcommands to the respective parent commands
	getCmd.AddCommand(agentinstancecli.NewGetCommand(cfg, &outputFormat))
	getCmd.AddCommand(agenttemplatecli.NewGetCommand(&cfg.Namespace, &outputFormat))
	createCmd.AddCommand(agentinstancecli.NewCreateCommand(cfg, &outputFormat))
	deleteCmd.AddCommand(agentinstancecli.NewDeleteCommand(cfg, &outputFormat))
	rootCmd.AddCommand(commands.NewInstallCommand(cfg))
	rootCmd.AddCommand(commands.NewUninstallCommand(&cfg.Namespace))
	rootCmd.AddCommand(agentinstancecli.NewInvokeCommand(cfg, &outputFormat))
	rootCmd.AddCommand(commands.NewBugReportCommand(cfg))
	rootCmd.AddCommand(commands.NewVersionCommand(cfg))
	rootCmd.AddCommand(commands.NewDashboardCommand(&cfg.Namespace))
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(mcp.NewMCPCmd())
	rootCmd.AddCommand(envdoc.NewEnvCmd())
	rootCmd.AddCommand(dbcli.NewCommandFromFunc(migrationSources(cfg)))

	return rootCmd
}

// vectorEnabledKey names two lookups that deliberately share it: the CLI's
// own DATABASE_VECTOR_ENABLED env var (a local operator override), and the
// controller-configmap key the chart renders — the value the controller pod
// itself consumes via envFrom. Same name, two different places.
const vectorEnabledKey = "DATABASE_VECTOR_ENABLED"

// migrationSources resolves the built-in migration tracks when a db
// subcommand runs (never during command construction, so unrelated commands
// do no work and print no warnings). The vector track is gated, in order of
// precedence, on: the DATABASE_VECTOR_ENABLED env var in the CLI's own
// environment (explicit operator intent, works without a cluster), the
// controller's configmap on the live cluster (the same value the server
// reads), and finally the controller's default (enabled).
func migrationSources(cfg *connection.Options) dbmigrate.SourcesFunc {
	return func(ctx context.Context) ([]migrations.Source, error) {
		vectorEnabled := true
		if v := os.Getenv(vectorEnabledKey); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: invalid %s=%q; assuming true\n", vectorEnabledKey, v)
			} else {
				vectorEnabled = b
			}
		} else if b, ok := clusterVectorEnabled(ctx, cfg.Namespace); ok {
			vectorEnabled = b
		}
		return migrations.BuiltinSources(vectorEnabled), nil
	}
}

// clusterVectorEnabled reads the vectorEnabledKey entry from the controller
// configmap in the given namespace (the same "kagent-controller" default
// naming the rest of the CLI assumes) — the cluster-side counterpart of the
// env-var override in migrationSources. When the value is used it says so on
// stderr, naming the kubeconfig context it was read from — the lookup follows
// the *current* context, so this is the operator's cue that the cluster and
// their --db-url had better be the same install. Best-effort: reports
// ok=false when no cluster is reachable, the configmap is absent, or the
// value doesn't parse — callers fall back to the default.
func clusterVectorEnabled(ctx context.Context, namespace string) (enabled, ok bool) {
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return false, false
	}
	k8sClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var cm corev1.ConfigMap
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "kagent-controller"}, &cm); err != nil {
		return false, false
	}
	b, err := strconv.ParseBool(cm.Data[vectorEnabledKey])
	if err != nil {
		return false, false
	}
	// Trailing blank line separates the notice from the command's stdout
	// when both land on a terminal; piped stdout is unaffected.
	fmt.Fprintf(os.Stderr, "resolved vector track from cluster context %q: configmap %s/kagent-controller has %s=%t (set %s to override)\n\n",
		currentKubeContext(), namespace, vectorEnabledKey, b, vectorEnabledKey)
	return b, true
}

// currentKubeContext names the kubeconfig context the CLI's Kubernetes client
// dials, for operator-facing messages. Best-effort.
func currentKubeContext() string {
	raw, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil || raw.CurrentContext == "" {
		return "(current kubeconfig context)"
	}
	return raw.CurrentContext
}

// runInteractive launches the workspace; the TUI reads raw keys, so a redirected stream is an error.
func runInteractive(cmd *cobra.Command, cfg *connection.Options) (err error) {
	if !isTerminal(cmd.InOrStdin()) || !isTerminal(cmd.OutOrStdout()) {
		return errors.New("kagent requires a terminal; use `kagent get agent-instance` and `kagent invoke` for non-interactive use")
	}

	client := cfg.Client()
	defer func() {
		err = errors.Join(err, client.Close())
	}()

	portForward, connectErr := connection.Connect(cmd.Context(), cfg)
	if connectErr != nil {
		return fmt.Errorf("connect to kagent: %w", connectErr)
	}
	if portForward != nil {
		defer portForward.Stop()
	}

	workspace := tui.Options{Namespace: cfg.Namespace}
	if runErr := tui.RunWorkspace(cmd.Context(), workspace, client, cfg.Verbose); runErr != nil {
		return fmt.Errorf("run kagent workspace: %w", runErr)
	}
	return nil
}

// isTerminal reports whether a stream is backed by a TTY; a non-*os.File never is.
func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
