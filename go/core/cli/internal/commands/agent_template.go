package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	typedapiv1alpha3 "github.com/kagent-dev/kagent/go/api/clientset/versioned/typed/api/v1alpha3"
	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	commonk8s "github.com/kagent-dev/kagent/go/core/cli/internal/common/k8s"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const agentTemplateMaxPageSize = 100

// AgentTemplateGetCfg configures AgentTemplate get and list operations.
type AgentTemplateGetCfg struct {
	Namespace    string
	OutputFormat string
	Name         string
	PageSize     int64
	PageToken    string
}

// runGetAgentTemplate gets one AgentTemplate or lists AgentTemplates through Kubernetes.
func runGetAgentTemplate(ctx context.Context, cfg *AgentTemplateGetCfg, out io.Writer) error {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	if err := validateAgentTemplateGetCfg(cfg); err != nil {
		return err
	}

	clients, err := commonk8s.NewKagentClientset()
	if err != nil {
		return err
	}
	return getAgentTemplates(ctx, clients.ApiV1alpha3().AgentTemplates(cfg.Namespace), cfg, format, out)
}

func validateAgentTemplateGetCfg(cfg *AgentTemplateGetCfg) error {
	if cfg.PageSize < 0 || cfg.PageSize > agentTemplateMaxPageSize {
		return fmt.Errorf("page size must be between 1 and %d, or 0 for the default of %d", agentTemplateMaxPageSize, agentTemplateMaxPageSize)
	}
	if cfg.Name != "" && (cfg.PageSize != 0 || cfg.PageToken != "") {
		return errors.New("pagination flags cannot be used when getting one AgentTemplate")
	}
	return nil
}

func getAgentTemplates(
	ctx context.Context,
	client typedapiv1alpha3.AgentTemplateInterface,
	cfg *AgentTemplateGetCfg,
	format clioutput.Format,
	out io.Writer,
) error {
	if cfg.Name != "" {
		template, err := client.Get(ctx, cfg.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get AgentTemplate %q: %w", cfg.Name, err)
		}
		if format == clioutput.FormatJSON {
			return clioutput.WriteJSON(out, template)
		}
		return writeAgentTemplatesTable(out, []apiv1alpha3.AgentTemplate{*template}, false, "")
	}

	pageSize := cfg.PageSize
	if pageSize == 0 {
		pageSize = agentTemplateMaxPageSize
	}
	templates, err := client.List(ctx, metav1.ListOptions{Limit: pageSize, Continue: cfg.PageToken})
	if err != nil {
		return fmt.Errorf("list AgentTemplates: %w", err)
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteJSON(out, templates)
	}
	return writeAgentTemplatesTable(out, templates.Items, true, templates.Continue)
}

func writeAgentTemplatesTable(w io.Writer, templates []apiv1alpha3.AgentTemplate, list bool, nextPageToken string) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"NAME", "HARNESS", "READY", "CREATED"})
	for i := range templates {
		template := &templates[i]
		created := ""
		if !template.CreationTimestamp.IsZero() {
			created = template.CreationTimestamp.Time.UTC().Format(time.RFC3339)
		}
		if len(template.Status.Harnesses) == 0 {
			tw.AppendRow(table.Row{template.Name, "", "UNKNOWN", created})
			continue
		}
		for j := range template.Status.Harnesses {
			harness := &template.Status.Harnesses[j]
			ready := "UNKNOWN"
			if condition := meta.FindStatusCondition(harness.Conditions, apiv1alpha3.AgentTemplateConditionReady); condition != nil {
				ready = strings.ToUpper(string(condition.Status))
			}
			tw.AppendRow(table.Row{template.Name, harness.Harness, ready, created})
		}
	}

	output := tw.Render()
	if list {
		if nextPageToken != "" {
			output += "\nNext page token: " + nextPageToken
		}
	}
	if _, err := fmt.Fprintln(w, output); err != nil {
		return fmt.Errorf("write AgentTemplate output: %w", err)
	}
	return nil
}

// NewGetAgentTemplateCmd constructs the AgentTemplate get/list command.
func NewGetAgentTemplateCmd() *cobra.Command {
	cfg := &AgentTemplateGetCfg{}
	cmd := &cobra.Command{
		Use:   "agent-template [NAME]",
		Short: "Get an AgentTemplate or list AgentTemplates",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			format, err := clioutput.FromCommand(cmd)
			if err != nil {
				return err
			}
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			cfg.Namespace = options.Namespace
			cfg.OutputFormat = format
			cfg.Name = name
			return runGetAgentTemplate(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}
	cmd.Flags().Int64Var(&cfg.PageSize, "page-size", 0, "Number of AgentTemplates per page (0 uses 100; maximum 100)")
	cmd.Flags().StringVar(&cfg.PageToken, "page-token", "", "Token returned by the previous page")
	return cmd
}
