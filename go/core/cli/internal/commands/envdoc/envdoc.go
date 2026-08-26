package envdoc

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/spf13/cobra"
)

var (
	format    string
	component string
)

// NewEnvCmd returns a cobra command that generates environment variable documentation.
func NewEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "env",
		Hidden: true,
		Short:  "List all kagent environment variables",
		Long:   "Generate documentation for all kagent environment variables in markdown or JSON format.",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "markdown", "md":
				fmt.Fprint(cmd.OutOrStdout(), env.ExportMarkdown(component))
			case "json":
				fmt.Fprint(cmd.OutOrStdout(), env.ExportJSON(component))
			default:
				return fmt.Errorf("unknown format %q: use markdown or json", format)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "markdown", "Output format: markdown, json")
	cmd.Flags().StringVar(&component, "component", "all", "Filter by component: controller, cli, agent-runtime, database, testing, all")

	return cmd
}
