package cli

import (
	"context"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands"
	dbcli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/db"
	sandboxcli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/sandbox"
	cliconfig "github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

// Option configures a binary that embeds the kagent command tree. Root with
// no options is the kagent CLI.
type Option func(*options)

type options struct {
	dialOptions     []grpc.DialOption
	extraMigrations func(context.Context) ([]migrations.Source, error)
}

// WithGRPCDialOptions adds gRPC dial options, such as interceptors, to every
// connection the CLI makes: the reachability check, the control-plane API,
// the A2A gateway, and Dial.
func WithGRPCDialOptions(dialOptions ...grpc.DialOption) Option {
	return func(o *options) { o.dialOptions = append(o.dialOptions, dialOptions...) }
}

// WithExtraMigrations adds migration tracks to the db commands, after the
// builtin tracks. The function runs when a db subcommand runs, with the
// resolved configuration in ctx.
func WithExtraMigrations(sources func(ctx context.Context) ([]migrations.Source, error)) Option {
	return func(o *options) { o.extraMigrations = sources }
}

// ConfigFromContext returns the CLI configuration the root command resolved
// from the configuration file and flags, or nil before it has run. A command
// added by an embedding binary reads it from cmd.Context() in RunE.
func ConfigFromContext(ctx context.Context) *configv1.CLIConfig {
	return cliconfig.FromContext(ctx)
}

// Dial connects to the control-plane API the way every CLI command does: the
// same URL and TLS flags, port-forward fallback, and dial options. Calls on
// it carry no x-user-id and no default deadline; the caller sets its own.
// Call it from RunE; the returned function closes the connection and stops
// any port-forward.
func Dial(cmd *cobra.Command) (grpc.ClientConnInterface, func() error, error) {
	options, err := connection.OptionsFromCommand(cmd)
	if err != nil {
		return nil, nil, err
	}
	session, err := connection.OpenAPI(cmd.Context(), options)
	if err != nil {
		return nil, nil, err
	}
	conn, err := session.API.Conn()
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	return conn, session.Close, nil
}

// Root creates a fresh kagent command tree.
func Root(opts ...Option) *cobra.Command {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	rootCmd := &cobra.Command{
		Use:           "kagent",
		Short:         "kagent is a CLI for kagent",
		Long:          "kagent is a CLI for kagent",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          runInteractive,
	}
	connection.RegisterFlags(rootCmd.PersistentFlags())
	rootCmd.PersistentFlags().StringP(clioutput.FlagName, "o", string(clioutput.FormatTable), "Output format")
	cliconfig.Register(rootCmd)
	if len(o.dialOptions) > 0 {
		// Commands resolve their connection from their context. Cobra runs only
		// the nearest PersistentPreRunE, so a command that sets its own and does
		// not call the root's, as db does, gets no dial options.
		loadConfig := rootCmd.PersistentPreRunE
		rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(cmd, args); err != nil {
				return err
			}
			cmd.SetContext(connection.WithDialOptions(cmd.Context(), o.dialOptions))
			return nil
		}
	}

	rootCmd.AddCommand(
		commands.NewAgentCmd(),
		commands.NewApplyAgentCmd(),
		commands.NewInstallCmd(),
		commands.NewUninstallCmd(),
		commands.NewBugReportCmd(),
		commands.NewVersionCmd(),
		commands.NewDashboardCmd(),
		sandboxcli.NewCmd(),
		commands.NewEnvCmd(),
		dbcli.NewDBCmd(o.extraMigrations),
	)
	return rootCmd
}
