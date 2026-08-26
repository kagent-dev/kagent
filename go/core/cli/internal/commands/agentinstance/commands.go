package agentinstance

import (
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/spf13/cobra"
)

// NewGetCommand constructs the AgentInstance get/list command.
func NewGetCommand(connectionOptions *connection.Options, outputFormat *string) *cobra.Command {
	cfg := &GetCfg{Connection: connectionOptions}
	cmd := &cobra.Command{
		Use:   "agent-instance [ID]",
		Short: "Get an AgentInstance or list your AgentInstances",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.OutputFormat = *outputFormat
			cfg.InstanceID = ""
			if len(args) == 1 {
				cfg.InstanceID = args[0]
			}
			return GetCmd(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}
	cmd.Flags().Int32Var(&cfg.PageSize, "page-size", 0, "Number of AgentInstances to return (default 50, maximum 100)")
	cmd.Flags().StringVar(&cfg.PageToken, "page-token", "", "Token returned by the previous page")
	return cmd
}

// NewInvokeCommand constructs the AgentInstance invoke command.
func NewInvokeCommand(connectionOptions *connection.Options, outputFormat *string) *cobra.Command {
	cfg := &InvokeCfg{Connection: connectionOptions}
	cmd := &cobra.Command{
		Use:     "invoke",
		Short:   "Invoke an AgentInstance",
		Long:    `Invoke an existing AgentInstance through the A2A API.`,
		Args:    cobra.NoArgs,
		Example: `kagent invoke --agent-instance 8bd650a8-9775-488f-8bc1-0d52bf7bdcab --task "Get all the pods"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.OutputFormat = *outputFormat
			return InvokeCmd(cmd.Context(), cfg, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cfg.AgentInstance, "agent-instance", "", "AgentInstance ID")
	cmd.Flags().StringVarP(&cfg.Task, "task", "t", "", "Task text")
	cmd.Flags().StringVarP(&cfg.File, "file", "f", "", "Read task text from a file or - for stdin")
	cmd.Flags().BoolVarP(&cfg.Stream, "stream", "S", false, "Stream the response")
	cmd.Flags().StringVar(&cfg.Token, "token", "", "Model API key passed through as an A2A Bearer token")
	_ = cmd.MarkFlagRequired("agent-instance")
	cmd.MarkFlagsOneRequired("task", "file")
	cmd.MarkFlagsMutuallyExclusive("task", "file")
	return cmd
}

// NewCreateCommand constructs the AgentInstance create command.
func NewCreateCommand(connectionOptions *connection.Options, outputFormat *string) *cobra.Command {
	cfg := &CreateCfg{Connection: connectionOptions}
	cmd := &cobra.Command{
		Use:   "agent-instance",
		Short: "Create an AgentInstance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.OutputFormat = *outputFormat
			return CreateCmd(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cfg.Harness, "harness", "", "Harness name")
	cmd.Flags().StringVar(&cfg.AgentTemplate, "agent-template", "", "AgentTemplate name")
	cmd.Flags().StringVar(&cfg.RequestID, "request-id", "", "Idempotency key (generated when omitted)")
	_ = cmd.MarkFlagRequired("harness")
	_ = cmd.MarkFlagRequired("agent-template")
	return cmd
}

// NewDeleteCommand constructs the AgentInstance delete command.
func NewDeleteCommand(connectionOptions *connection.Options, outputFormat *string) *cobra.Command {
	cfg := &DeleteCfg{Connection: connectionOptions}
	cmd := &cobra.Command{
		Use:   "agent-instance ID",
		Short: "Delete an AgentInstance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.OutputFormat = *outputFormat
			cfg.InstanceID = args[0]
			return DeleteCmd(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}
	return cmd
}
