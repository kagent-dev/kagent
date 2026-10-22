package connection

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
)

// Flag names are unexported so RegisterFlags and OptionsFromCommand are the
// only things that can disagree about them, and they cannot.
const (
	flagAPIURL     = "api-url"
	flagGatewayURL = "gateway-url"
	flagCAFile     = "ca-file"
	flagServerName = "server-name"
	flagNamespace  = "namespace"
	flagVerbose    = "verbose"
	flagTimeout    = "timeout"
	flagUserID     = "user-id"
)

// RegisterFlags declares the CLI-wide connection flags, defaulted from DefaultOptions.
func RegisterFlags(flags *pflag.FlagSet) {
	defaults := DefaultOptions()
	flags.String(flagAPIURL, defaults.APIURL, "KAgent control-plane API URL")
	flags.String(flagGatewayURL, defaults.GatewayURL, "KAgent A2A and MCP gateway URL")
	flags.String(flagCAFile, defaults.CAFile, "CA certificate file for KAgent endpoints")
	flags.String(flagServerName, defaults.ServerName, "TLS server name for KAgent endpoints")
	flags.StringP(flagNamespace, "n", defaults.Namespace, "Namespace")
	flags.BoolP(flagVerbose, "v", defaults.Verbose, "Verbose output")
	flags.Duration(flagTimeout, defaults.Timeout, "Timeout")
	flags.String(flagUserID, defaults.UserID, "Caller identity used to select the server-side data partition")
}

type dialOptionsKey struct{}

// WithDialOptions makes OptionsFromCommand add options to every client built
// from a command run with ctx.
func WithDialOptions(ctx context.Context, options []grpc.DialOption) context.Context {
	return context.WithValue(ctx, dialOptionsKey{}, options)
}

// OptionsFromCommand resolves connection options from the flags a command was
// invoked with, which include the root's persistent flags.
func OptionsFromCommand(cmd *cobra.Command) (Options, error) {
	flags := cmd.Flags()
	var options Options
	var err error
	if options.APIURL, err = flags.GetString(flagAPIURL); err != nil {
		return Options{}, err
	}
	if options.GatewayURL, err = flags.GetString(flagGatewayURL); err != nil {
		return Options{}, err
	}
	if options.CAFile, err = flags.GetString(flagCAFile); err != nil {
		return Options{}, err
	}
	if options.ServerName, err = flags.GetString(flagServerName); err != nil {
		return Options{}, err
	}
	if options.Namespace, err = flags.GetString(flagNamespace); err != nil {
		return Options{}, err
	}
	if options.Verbose, err = flags.GetBool(flagVerbose); err != nil {
		return Options{}, err
	}
	if options.Timeout, err = flags.GetDuration(flagTimeout); err != nil {
		return Options{}, err
	}
	if options.UserID, err = flags.GetString(flagUserID); err != nil {
		return Options{}, err
	}
	if cmd.Context() != nil {
		options.DialOptions, _ = cmd.Context().Value(dialOptionsKey{}).([]grpc.DialOption)
	}
	return options, nil
}
