package connection

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Flag names are unexported so RegisterFlags and OptionsFromCommand are the
// only things that can disagree about them, and they cannot.
const (
	flagKAgentURL            = "kagent-url"
	flagKAgentGRPCURL        = "grpc-url"
	flagKAgentGRPCTLS        = "grpc-tls"
	flagKAgentGRPCCAFile     = "grpc-ca-file"
	flagKAgentGRPCServerName = "grpc-server-name"
	flagNamespace            = "namespace"
	flagVerbose              = "verbose"
	flagTimeout              = "timeout"
	flagUserID               = "user-id"
)

// RegisterFlags declares the CLI-wide connection flags, defaulted from DefaultOptions.
func RegisterFlags(flags *pflag.FlagSet) {
	defaults := DefaultOptions()
	flags.String(flagKAgentURL, defaults.KAgentURL, "KAgent REST URL")
	flags.String(flagKAgentGRPCURL, defaults.KAgentGRPCURL, "KAgent gRPC target")
	flags.Bool(flagKAgentGRPCTLS, defaults.KAgentGRPCTLS, "Use TLS for KAgent gRPC")
	flags.String(flagKAgentGRPCCAFile, defaults.KAgentGRPCCAFile, "CA certificate file for KAgent gRPC")
	flags.String(flagKAgentGRPCServerName, defaults.KAgentGRPCServerName, "TLS server name for KAgent gRPC")
	flags.StringP(flagNamespace, "n", defaults.Namespace, "Namespace")
	flags.BoolP(flagVerbose, "v", defaults.Verbose, "Verbose output")
	flags.Duration(flagTimeout, defaults.Timeout, "Timeout")
	flags.String(flagUserID, defaults.UserID, "Caller identity used to select the server-side data partition")
}

// OptionsFromCommand resolves connection options from the flags a command was
// invoked with, which include the root's persistent flags.
func OptionsFromCommand(cmd *cobra.Command) (Options, error) {
	flags := cmd.Flags()
	var options Options
	var err error
	if options.KAgentURL, err = flags.GetString(flagKAgentURL); err != nil {
		return Options{}, err
	}
	if options.KAgentGRPCURL, err = flags.GetString(flagKAgentGRPCURL); err != nil {
		return Options{}, err
	}
	if options.KAgentGRPCTLS, err = flags.GetBool(flagKAgentGRPCTLS); err != nil {
		return Options{}, err
	}
	if options.KAgentGRPCCAFile, err = flags.GetString(flagKAgentGRPCCAFile); err != nil {
		return Options{}, err
	}
	if options.KAgentGRPCServerName, err = flags.GetString(flagKAgentGRPCServerName); err != nil {
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
	return options, nil
}
