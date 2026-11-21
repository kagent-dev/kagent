package commands

import (
	"errors"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

// NewHarnessCmd builds the harness list command.
func NewHarnessCmd() *cobra.Command {
	list := &cobra.Command{
		Use: "list", Short: "List Harnesses", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			session, format, err := openAPI(cmd)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, session.Close()) }()
			response, err := session.API.Harness.ListHarnesses(cmd.Context(), &apiv1.ListHarnessesRequest{Namespace: session.Namespace})
			if err != nil {
				return fmt.Errorf("list Harnesses: %w", err)
			}
			return writeHarnesses(cmd.OutOrStdout(), format, response)
		},
	}
	cmd := &cobra.Command{Use: "harness", Short: "List the Harnesses Agents run on"}
	cmd.AddCommand(list)
	return cmd
}

func writeHarnesses(w io.Writer, format clioutput.Format, response *apiv1.ListHarnessesResponse) error {
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(w, response)
	}
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"NAME", "RUNTIME"})
	for _, harness := range response.GetHarnesses() {
		tw.AppendRow(table.Row{harness.GetRef().GetName(), harness.GetRuntime()})
	}
	_, err := fmt.Fprintln(w, tw.Render())
	return err
}
