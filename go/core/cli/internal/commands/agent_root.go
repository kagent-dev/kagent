package commands

import (
	"github.com/kagent-dev/kagent/go/core/cli/internal/commands/session"
	"github.com/spf13/cobra"
)

// NewAgentCmd groups agent discovery, invocation, and conversations.
func NewAgentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Discover agents and work with their conversations"}
	templates := &cobra.Command{Use: "template", Short: "Discover reusable agent templates"}
	templates.AddCommand(newAgentTemplateReadCmd(true), newAgentTemplateReadCmd(false))
	cmd.AddCommand(newAgentReadCmd(true), newAgentReadCmd(false), session.NewInvokeCmd(), session.NewCmd(), templates)
	return cmd
}
