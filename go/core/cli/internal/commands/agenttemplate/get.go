// Package agenttemplate implements AgentTemplate CLI commands.
package agenttemplate

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
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const maxPageSize = 100

// GetCfg configures AgentTemplate get and list operations.
type GetCfg struct {
	Namespace    string
	OutputFormat string
	Name         string
	PageSize     int64
	PageToken    string
}

// GetCmd gets one AgentTemplate or lists AgentTemplates through Kubernetes.
func GetCmd(ctx context.Context, cfg *GetCfg, out io.Writer) error {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	if err := validateGetCfg(cfg); err != nil {
		return err
	}

	clients, err := commonk8s.NewKagentClientset()
	if err != nil {
		return err
	}
	return get(ctx, clients.ApiV1alpha3().AgentTemplates(cfg.Namespace), cfg, format, out)
}

func validateGetCfg(cfg *GetCfg) error {
	if cfg.PageSize < 0 || cfg.PageSize > maxPageSize {
		return fmt.Errorf("page size must be between 1 and %d, or 0 for the default of %d", maxPageSize, maxPageSize)
	}
	if cfg.Name != "" && (cfg.PageSize != 0 || cfg.PageToken != "") {
		return errors.New("pagination flags cannot be used when getting one AgentTemplate")
	}
	return nil
}

func get(
	ctx context.Context,
	client typedapiv1alpha3.AgentTemplateInterface,
	cfg *GetCfg,
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
		return writeTemplatesTable(out, []apiv1alpha3.AgentTemplate{*template}, false, "")
	}

	pageSize := cfg.PageSize
	if pageSize == 0 {
		pageSize = maxPageSize
	}
	templates, err := client.List(ctx, metav1.ListOptions{Limit: pageSize, Continue: cfg.PageToken})
	if err != nil {
		return fmt.Errorf("list AgentTemplates: %w", err)
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteJSON(out, templates)
	}
	return writeTemplatesTable(out, templates.Items, true, templates.Continue)
}

func writeTemplatesTable(w io.Writer, templates []apiv1alpha3.AgentTemplate, list bool, nextPageToken string) error {
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
