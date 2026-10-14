package connection

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kagent-dev/kagent/go/api/client"
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

	flagAllowInsecureTokenTransport = "allow-insecure-token-transport"
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

// RegisterTokenFlags declares the flags that only mean something when the
// binary has a token provider. The kagent binary has none, so it does not register
// them, and carries no flag that does nothing.
func RegisterTokenFlags(flags *pflag.FlagSet) {
	flags.Bool(flagAllowInsecureTokenTransport, false,
		"Send the credential to a non-loopback endpoint without TLS, such as a Service address from inside the cluster")
}

type tokenProviderKey struct{}

// WithTokenProvider makes OptionsFromCommand attach provider to every client
// built from a command run with ctx.
func WithTokenProvider(ctx context.Context, provider client.TokenProvider) context.Context {
	return context.WithValue(ctx, tokenProviderKey{}, provider)
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
		options.TokenProvider, _ = cmd.Context().Value(tokenProviderKey{}).(client.TokenProvider)
	}
	if flags.Lookup(flagAllowInsecureTokenTransport) != nil {
		if options.AllowInsecureTokenTransport, err = flags.GetBool(flagAllowInsecureTokenTransport); err != nil {
			return Options{}, err
		}
	}
	return options, nil
}
