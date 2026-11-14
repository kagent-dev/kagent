package commands

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

// NewModelCmd builds the model list command.
func NewModelCmd() *cobra.Command {
	list := &cobra.Command{
		Use: "list", Short: "List ModelConfigs", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			session, format, err := openAPI(cmd)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, session.Close()) }()
			response, err := session.API.Model.ListModelConfigs(cmd.Context(), &apiv1.ListModelConfigsRequest{})
			if err != nil {
				return fmt.Errorf("list ModelConfigs: %w", err)
			}
			return writeModelConfigs(cmd.OutOrStdout(), format, session.Namespace, response)
		},
	}
	cmd := &cobra.Command{Use: "model", Short: "List the ModelConfigs AgentTemplates use"}
	cmd.AddCommand(list)
	return cmd
}

// writeModelConfigs filters to namespace here, because ListModelConfigs takes
// no namespace and returns every one the caller can read.
func writeModelConfigs(w io.Writer, format clioutput.Format, namespace string, response *apiv1.ListModelConfigsResponse) error {
	configs := slices.DeleteFunc(response.GetModelConfigs(), func(config *apiv1.ModelConfig) bool {
		return config.GetRef().GetNamespace() != namespace
	})
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(w, &apiv1.ListModelConfigsResponse{ModelConfigs: configs})
	}
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"NAME", "PROVIDER", "MODEL"})
	for _, config := range configs {
		decoded := &apiv1alpha3.ModelConfig{}
		if err := structuredobject.ToGo(config.GetResource(), apiv1alpha3.ModelConfigGroupKind.Kind, decoded, 0); err != nil {
			return fmt.Errorf("decode ModelConfig %q: %w", config.GetRef().GetName(), err)
		}
		tw.AppendRow(table.Row{config.GetRef().GetName(), decoded.Spec.Provider, decoded.Spec.Model})
	}
	_, err := fmt.Fprintln(w, tw.Render())
	return err
}
