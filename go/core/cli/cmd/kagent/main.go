package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	cli "github.com/kagent-dev/kagent/go/core/cli/internal/cli/agent"
	agentinstancecli "github.com/kagent-dev/kagent/go/core/cli/internal/cli/agentinstance"
	agenttemplatecli "github.com/kagent-dev/kagent/go/core/cli/internal/cli/agenttemplate"
	"github.com/kagent-dev/kagent/go/core/cli/internal/cli/connection"
	"github.com/kagent-dev/kagent/go/core/cli/internal/cli/envdoc"
	"github.com/kagent-dev/kagent/go/core/cli/internal/cli/mcp"
	"github.com/kagent-dev/kagent/go/core/cli/internal/profiles"
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

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// listen for signals to cancel the context throughout the application
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-done

		fmt.Fprintf(os.Stderr, "kagent aborted.\n")
		fmt.Fprintf(os.Stderr, "Exiting.\n")

		cancel()
	}()
	rootCmd := newRootCommand(ctx, defaultRootOptions())
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)

		os.Exit(1)
	}
}

type rootOptions struct {
	Connection   connection.Options
	OutputFormat string
}

func defaultRootOptions() *rootOptions {
	return &rootOptions{Connection: connection.DefaultOptions(), OutputFormat: "table"}
}

func newRootCommand(ctx context.Context, opts *rootOptions) *cobra.Command {
	cfg := &opts.Connection
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
	rootCmd.SetContext(ctx)

	rootCmd.PersistentFlags().StringVar(&cfg.KAgentURL, "kagent-url", cfg.KAgentURL, "KAgent REST URL")
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentGRPCURL, "grpc-url", cfg.KAgentGRPCURL, "KAgent gRPC target")
	rootCmd.PersistentFlags().BoolVar(&cfg.KAgentGRPCTLS, "grpc-tls", cfg.KAgentGRPCTLS, "Use TLS for KAgent gRPC")
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentGRPCCAFile, "grpc-ca-file", cfg.KAgentGRPCCAFile, "CA certificate file for KAgent gRPC")
	rootCmd.PersistentFlags().StringVar(&cfg.KAgentGRPCServerName, "grpc-server-name", cfg.KAgentGRPCServerName, "TLS server name for KAgent gRPC")
	rootCmd.PersistentFlags().StringVarP(&cfg.Namespace, "namespace", "n", cfg.Namespace, "Namespace")
	rootCmd.PersistentFlags().StringVarP(&opts.OutputFormat, "output-format", "o", opts.OutputFormat, "Output format")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", cfg.Verbose, "Verbose output")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "Timeout")
	rootCmd.PersistentFlags().StringVar(&cfg.UserID, "user-id", cfg.UserID, "Caller identity used to select the server-side data partition")
	installCfg := &cli.InstallCfg{
		Connection: cfg,
	}

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install kagent",
		Long:  `Install kagent`,
		Run: func(cmd *cobra.Command, args []string) {
			cli.InstallCmd(cmd.Context(), installCfg)
		},
	}
	installCmd.Flags().StringVar(&installCfg.Profile, "profile", "", "Installation profile (minimal|demo)")
	_ = installCmd.RegisterFlagCompletionFunc("profile", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return profiles.Profiles, cobra.ShellCompDirectiveNoFileComp
	})

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall kagent",
		Long:  `Uninstall kagent`,
		Run: func(cmd *cobra.Command, args []string) {
			cli.UninstallCmd(cmd.Context(), cfg.Namespace)
		},
	}

	invokeCfg := &agentinstancecli.InvokeCfg{
		Connection: cfg,
	}

	invokeCmd := &cobra.Command{
		Use:   "invoke",
		Short: "Invoke an AgentInstance",
		Long:  `Invoke an existing AgentInstance through the A2A API.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			invokeCfg.OutputFormat = opts.OutputFormat
			return agentinstancecli.InvokeCmd(cmd.Context(), invokeCfg, cmd.InOrStdin(), cmd.OutOrStdout())
		},
		Example: `kagent invoke --agent-instance 8bd650a8-9775-488f-8bc1-0d52bf7bdcab --task "Get all the pods"`,
	}

	invokeCmd.Flags().StringVar(&invokeCfg.AgentInstance, "agent-instance", "", "AgentInstance ID")
	invokeCmd.Flags().StringVarP(&invokeCfg.Task, "task", "t", "", "Task text")
	invokeCmd.Flags().StringVarP(&invokeCfg.File, "file", "f", "", "Read task text from a file or - for stdin")
	invokeCmd.Flags().BoolVarP(&invokeCfg.Stream, "stream", "S", false, "Stream the response")
	invokeCmd.Flags().StringVar(&invokeCfg.Token, "token", "", "Model API key passed through as an A2A Bearer token")
	_ = invokeCmd.MarkFlagRequired("agent-instance")
	invokeCmd.MarkFlagsOneRequired("task", "file")
	invokeCmd.MarkFlagsMutuallyExclusive("task", "file")

	bugReportCmd := &cobra.Command{
		Use:   "bug-report",
		Short: "Generate a bug report",
		Long:  `Generate a bug report`,
		Run: func(cmd *cobra.Command, args []string) {
			pf, err := connection.Connect(cmd.Context(), cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error connecting to server: %v\n", err)
				return
			}
			if pf != nil {
				defer pf.Stop()
			}
			cli.BugReportCmd(cfg.Namespace, cfg.Verbose)
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the kagent version",
		Long:  `Print the kagent version`,
		Run: func(cmd *cobra.Command, args []string) {
			// print out kagent CLI version regardless if a port-forward to kagent server succeeds
			// versions unable to obtain from the remote kagent will be reported as "unknown"
			clientSet := cfg.Client()
			defer clientSet.Close() //nolint:errcheck
			defer cli.VersionCmd(clientSet)

			if pf, _ := connection.Connect(cmd.Context(), cfg); pf != nil {
				defer pf.Stop()
			}
		},
	}

	dashboardCmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Open the kagent dashboard",
		Long:  `Open the kagent dashboard`,
		Run: func(cmd *cobra.Command, args []string) {
			cli.DashboardCmd(cmd.Context(), cfg.Namespace)
		},
	}

	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get a kagent resource",
		Long:  `Get a kagent resource`,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("resource type is required")
		},
	}
	agentInstanceGetCfg := &agentinstancecli.GetCfg{Connection: cfg}
	getAgentInstanceCmd := &cobra.Command{
		Use:   "agent-instance [ID]",
		Short: "Get an AgentInstance or list your AgentInstances",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentInstanceGetCfg.OutputFormat = opts.OutputFormat
			agentInstanceGetCfg.InstanceID = ""
			if len(args) == 1 {
				agentInstanceGetCfg.InstanceID = args[0]
			}
			return agentinstancecli.GetCmd(cmd.Context(), agentInstanceGetCfg, cmd.OutOrStdout())
		},
	}
	getAgentInstanceCmd.Flags().Int32Var(&agentInstanceGetCfg.PageSize, "page-size", 0, "Number of AgentInstances to return (default 50, maximum 100)")
	getAgentInstanceCmd.Flags().StringVar(&agentInstanceGetCfg.PageToken, "page-token", "", "Token returned by the previous page")

	agentTemplateGetCfg := &agenttemplatecli.GetCfg{}
	getAgentTemplateCmd := &cobra.Command{
		Use:   "agent-template [NAME]",
		Short: "Get an AgentTemplate or list AgentTemplates",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentTemplateGetCfg.Namespace = cfg.Namespace
			agentTemplateGetCfg.OutputFormat = opts.OutputFormat
			agentTemplateGetCfg.Name = ""
			if len(args) == 1 {
				agentTemplateGetCfg.Name = args[0]
			}
			return agenttemplatecli.GetCmd(cmd.Context(), agentTemplateGetCfg, cmd.OutOrStdout())
		},
	}
	getAgentTemplateCmd.Flags().Int64Var(&agentTemplateGetCfg.PageSize, "page-size", 0, "Number of AgentTemplates per page (0 uses 100; maximum 100)")
	getAgentTemplateCmd.Flags().StringVar(&agentTemplateGetCfg.PageToken, "page-token", "", "Token returned by the previous page")

	getCmd.AddCommand(getAgentInstanceCmd, getAgentTemplateCmd)

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a kagent resource",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("resource type is required")
		},
	}
	createAgentInstanceCfg := &agentinstancecli.CreateCfg{Connection: cfg}
	createAgentInstanceCmd := &cobra.Command{
		Use:   "agent-instance",
		Short: "Create an AgentInstance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			createAgentInstanceCfg.OutputFormat = opts.OutputFormat
			return agentinstancecli.CreateCmd(cmd.Context(), createAgentInstanceCfg, cmd.OutOrStdout())
		},
	}
	createAgentInstanceCmd.Flags().StringVar(&createAgentInstanceCfg.Harness, "harness", "", "Harness name")
	createAgentInstanceCmd.Flags().StringVar(&createAgentInstanceCfg.AgentTemplate, "agent-template", "", "AgentTemplate name")
	createAgentInstanceCmd.Flags().StringVar(&createAgentInstanceCfg.RequestID, "request-id", "", "Idempotency key (generated when omitted)")
	_ = createAgentInstanceCmd.MarkFlagRequired("harness")
	_ = createAgentInstanceCmd.MarkFlagRequired("agent-template")
	createCmd.AddCommand(createAgentInstanceCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a kagent resource",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("resource type is required")
		},
	}
	deleteAgentInstanceCfg := &agentinstancecli.DeleteCfg{Connection: cfg}
	deleteAgentInstanceCmd := &cobra.Command{
		Use:   "agent-instance ID",
		Short: "Delete an AgentInstance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deleteAgentInstanceCfg.OutputFormat = opts.OutputFormat
			deleteAgentInstanceCfg.InstanceID = args[0]
			return agentinstancecli.DeleteCmd(cmd.Context(), deleteAgentInstanceCfg, cmd.OutOrStdout())
		},
	}
	deleteCmd.AddCommand(deleteAgentInstanceCmd)

	rootCmd.AddCommand(installCmd, uninstallCmd, invokeCmd, bugReportCmd, versionCmd, dashboardCmd, getCmd, createCmd, deleteCmd, mcp.NewMCPCmd(), envdoc.NewEnvCmd(), dbcli.NewCommandFromFunc(migrationSources(opts)))

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
func migrationSources(opts *rootOptions) dbmigrate.SourcesFunc {
	return func(ctx context.Context) ([]migrations.Source, error) {
		vectorEnabled := true
		if v := os.Getenv(vectorEnabledKey); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: invalid %s=%q; assuming true\n", vectorEnabledKey, v)
			} else {
				vectorEnabled = b
			}
		} else if b, ok := clusterVectorEnabled(ctx, opts.Connection.Namespace); ok {
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
