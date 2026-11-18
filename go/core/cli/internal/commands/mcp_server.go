package commands

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

// NewMCPServerCmd builds the mcp-server list command.
func NewMCPServerCmd() *cobra.Command {
	list := &cobra.Command{
		Use: "list", Short: "List MCP servers and the tools they expose", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			session, format, err := openAPI(cmd)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, session.Close()) }()
			response, err := session.API.Tool.ListToolServers(cmd.Context(), &apiv1.ListToolServersRequest{})
			if err != nil {
				return fmt.Errorf("list MCP servers: %w", err)
			}
			return writeMCPServers(cmd.OutOrStdout(), format, session.Namespace, response)
		},
	}
	cmd := &cobra.Command{Use: "mcp-server", Short: "List the MCP servers that AgentTemplates can bind"}
	cmd.AddCommand(list)
	return cmd
}

// writeMCPServers filters to namespace here, because ListToolServers takes no
// namespace and returns every server the caller can read, as namespace/name.
func writeMCPServers(w io.Writer, format clioutput.Format, namespace string, response *apiv1.ListToolServersResponse) error {
	servers := slices.DeleteFunc(response.GetToolServers(), func(server *apiv1.ToolServer) bool {
		return !strings.HasPrefix(server.GetRef(), namespace+"/")
	})
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(w, &apiv1.ListToolServersResponse{ToolServers: servers})
	}
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"SERVER", "KIND", "TOOLS"})
	for _, server := range servers {
		names := make([]string, 0, len(server.GetDiscoveredTools()))
		for _, tool := range server.GetDiscoveredTools() {
			names = append(names, tool.GetName())
		}
		tw.AppendRow(table.Row{strings.TrimPrefix(server.GetRef(), namespace+"/"), server.GetGroupKind(), strings.Join(names, "\n")})
	}
	_, err := fmt.Fprintln(w, tw.Render())
	return err
}
