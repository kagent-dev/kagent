package agenttemplate

import (
	"github.com/spf13/cobra"
)

// NewGetCommand constructs the AgentTemplate get/list command.
func NewGetCommand(namespace *string, outputFormat *string) *cobra.Command {
	cfg := &GetCfg{}
	cmd := &cobra.Command{
		Use:   "agent-template [NAME]",
		Short: "Get an AgentTemplate or list AgentTemplates",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.Namespace = *namespace
			cfg.OutputFormat = *outputFormat
			cfg.Name = ""
			if len(args) == 1 {
				cfg.Name = args[0]
			}
			return GetCmd(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}
	cmd.Flags().Int64Var(&cfg.PageSize, "page-size", 0, "Number of AgentTemplates per page (0 uses 100; maximum 100)")
	cmd.Flags().StringVar(&cfg.PageToken, "page-token", "", "Token returned by the previous page")
	return cmd
}
