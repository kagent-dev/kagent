package cli

import (
	"context"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/kagent-dev/kagent/go/api/client"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands"
	dbcli "github.com/kagent-dev/kagent/go/core/cli/internal/commands/db"
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands/mcp"
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
	tokenProvider   client.TokenProvider
	extraMigrations func(context.Context) ([]migrations.Source, error)
}

// WithTokenProvider sends the provider's token on every call the CLI makes:
// the reachability check, the control-plane API, and the A2A gateway. It also
// registers --allow-insecure-token-transport, which permits a plaintext
// non-loopback endpoint. client.TokenProvider states when the provider is
// asked and how an empty token or an error is handled; a provider that also
// implements client.UnauthenticatedHinter supplies the next step when the
// server rejects a call.
func WithTokenProvider(provider client.TokenProvider) Option {
	return func(o *options) { o.tokenProvider = provider }
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
// same URL and TLS flags, the port-forward fallback, and the token provider.
// It is for an embedding binary that calls a service of its own on that API.
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
	if o.tokenProvider != nil {
		connection.RegisterTokenFlags(rootCmd.PersistentFlags())
		// Every command resolves its connection from its context, so the
		// provider rides in after configuration loads. Cobra runs only the
		// nearest PersistentPreRunE; a command that sets its own must call the
		// root's, as db does.
		loadConfig := rootCmd.PersistentPreRunE
		rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(cmd, args); err != nil {
				return err
			}
			cmd.SetContext(connection.WithTokenProvider(cmd.Context(), o.tokenProvider))
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
		mcp.NewMCPCmd(),
		sandboxcli.NewCmd(),
		commands.NewEnvCmd(),
		dbcli.NewDBCmd(o.extraMigrations),
	)
	return rootCmd
}
