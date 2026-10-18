package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

const (
	agentTemplateKind        = "AgentTemplate"
	agentTemplateMaxPageSize = 100
)

// AgentTemplateGetCfg configures AgentTemplate get and list operations.
type AgentTemplateGetCfg struct {
	Namespace    string
	OutputFormat string
	Name         string
	PageSize     int64
	PageToken    string
}

// AgentTemplateManifestCfg configures an AgentTemplate manifest operation.
type AgentTemplateManifestCfg struct {
	OutputFormat string
	File         string
}

type lifecycleClient interface {
	CreateAgentTemplate(context.Context, *apiv1.CreateAgentTemplateRequest) (*apiv1.CreateAgentTemplateResponse, error)
	UpdateAgentTemplate(context.Context, *apiv1.UpdateAgentTemplateRequest) (*apiv1.UpdateAgentTemplateResponse, error)
}

type agentTemplateManifestOperation func(context.Context, lifecycleClient, *apiv1.ResourceReference, *apiv1.StructuredObject, clioutput.Format, io.Writer) error

func runAgentTemplateManifest(
	ctx context.Context,
	options connection.Options,
	cfg *AgentTemplateManifestCfg,
	out io.Writer,
	operation agentTemplateManifestOperation,
) (err error) {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	ref, resource, err := readAgentTemplateManifest(cfg.File, options.Namespace)
	if err != nil {
		return err
	}
	session, err := connection.Open(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Close())
	}()
	return operation(ctx, session.API.AgentTemplate, ref, resource, format, out)
}

func readAgentTemplateManifest(filename, namespace string) (*apiv1.ResourceReference, *apiv1.StructuredObject, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("read AgentTemplate manifest %q: %w", filename, err)
	}
	manifest := &apiv1alpha3.AgentTemplate{}
	if err := yaml.UnmarshalStrict(data, manifest); err != nil {
		return nil, nil, fmt.Errorf("parse AgentTemplate manifest %q: %w", filename, err)
	}
	if manifest.APIVersion != apiv1alpha3.GroupVersion.String() || manifest.Kind != agentTemplateKind {
		return nil, nil, fmt.Errorf("AgentTemplate manifest %q must have apiVersion %q and kind %q", filename, apiv1alpha3.GroupVersion.String(), agentTemplateKind)
	}
	if manifest.Name == "" {
		return nil, nil, fmt.Errorf("AgentTemplate manifest %q must have metadata.name", filename)
	}
	if manifest.Namespace != "" && manifest.Namespace != namespace {
		return nil, nil, fmt.Errorf("AgentTemplate manifest namespace %q does not match --namespace %q", manifest.Namespace, namespace)
	}
	resource, err := structuredobject.FromGo(manifest, apiv1alpha3.GroupVersion.String(), agentTemplateKind, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("encode AgentTemplate manifest %q: %w", filename, err)
	}
	return &apiv1.ResourceReference{Namespace: namespace, Name: manifest.Name}, resource, nil
}

func applyAgentTemplate(
	ctx context.Context,
	client lifecycleClient,
	ref *apiv1.ResourceReference,
	resource *apiv1.StructuredObject,
	format clioutput.Format,
	out io.Writer,
) error {
	created, err := client.CreateAgentTemplate(ctx, &apiv1.CreateAgentTemplateRequest{Ref: ref, Resource: resource})
	if status.Code(err) != codes.AlreadyExists {
		if err != nil {
			return fmt.Errorf("apply AgentTemplate: %w", err)
		}
		return writeAgentTemplateResult(out, format, created, created.GetAgentTemplate())
	}
	updated, err := client.UpdateAgentTemplate(ctx, &apiv1.UpdateAgentTemplateRequest{Ref: ref, Resource: resource})
	if err != nil {
		return fmt.Errorf("apply AgentTemplate: %w", err)
	}
	return writeAgentTemplateResult(out, format, updated, updated.GetAgentTemplate())
}

func writeAgentTemplateResult(w io.Writer, format clioutput.Format, response proto.Message, result *apiv1.AgentTemplate) error {
	if result == nil {
		return errors.New("AgentTemplate operation returned no AgentTemplate")
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(w, response)
	}
	template := &apiv1alpha3.AgentTemplate{}
	if err := structuredobject.ToGo(result.GetResource(), agentTemplateKind, template, 0); err != nil {
		return fmt.Errorf("decode AgentTemplate result: %w", err)
	}
	return writeAgentTemplatesTable(w, []apiv1alpha3.AgentTemplate{*template}, false, "")
}

type agentTemplateReadClient interface {
	ListAgentTemplates(context.Context, *apiv1.ListAgentTemplatesRequest) (*apiv1.ListAgentTemplatesResponse, error)
	GetAgentTemplate(context.Context, *apiv1.GetAgentTemplateRequest) (*apiv1.GetAgentTemplateResponse, error)
}

// runGetAgentTemplate gets one AgentTemplate or lists AgentTemplates through the kagent API.
func runGetAgentTemplate(ctx context.Context, options connection.Options, cfg *AgentTemplateGetCfg, out io.Writer) (err error) {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	if err := validateAgentTemplateGetCfg(cfg); err != nil {
		return err
	}

	session, err := connection.OpenAPI(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Close())
	}()
	return getAgentTemplates(ctx, session.API.AgentTemplate, cfg, format, out)
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
	client agentTemplateReadClient,
	cfg *AgentTemplateGetCfg,
	format clioutput.Format,
	out io.Writer,
) error {
	if cfg.Name != "" {
		response, err := client.GetAgentTemplate(ctx, &apiv1.GetAgentTemplateRequest{Ref: &apiv1.ResourceReference{Namespace: cfg.Namespace, Name: cfg.Name}})
		if err != nil {
			return fmt.Errorf("get AgentTemplate %q: %w", cfg.Name, err)
		}
		item, err := decodeAgentTemplate(response.GetAgentTemplate())
		if err != nil {
			return err
		}
		if format == clioutput.FormatJSON {
			return clioutput.WriteJSON(out, item)
		}
		return writeAgentTemplatesTable(out, []apiv1alpha3.AgentTemplate{*item}, false, "")
	}

	pageSize := cfg.PageSize
	if pageSize == 0 {
		pageSize = agentTemplateMaxPageSize
	}
	response, err := client.ListAgentTemplates(ctx, &apiv1.ListAgentTemplatesRequest{
		Namespace: cfg.Namespace,
		Page:      &apiv1.PageRequest{Limit: int32(pageSize), PageToken: cfg.PageToken},
	})
	if err != nil {
		return fmt.Errorf("list AgentTemplates: %w", err)
	}
	// The Kubernetes list shape keeps JSON output and its continue token as they were.
	list := &apiv1alpha3.AgentTemplateList{ListMeta: metav1.ListMeta{Continue: response.GetPage().GetNextPageToken()}}
	for _, result := range response.GetAgentTemplates() {
		item, err := decodeAgentTemplate(result)
		if err != nil {
			return err
		}
		list.Items = append(list.Items, *item)
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteJSON(out, list)
	}
	return writeAgentTemplatesTable(out, list.Items, true, list.Continue)
}

func decodeAgentTemplate(result *apiv1.AgentTemplate) (*apiv1alpha3.AgentTemplate, error) {
	if result == nil {
		return nil, errors.New("agent template operation returned no AgentTemplate")
	}
	item := &apiv1alpha3.AgentTemplate{}
	if err := structuredobject.ToGo(result.GetResource(), agentTemplateKind, item, 0); err != nil {
		return nil, fmt.Errorf("decode AgentTemplate: %w", err)
	}
	return item, nil
}

func writeAgentTemplatesTable(w io.Writer, templates []apiv1alpha3.AgentTemplate, list bool, nextPageToken string) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"NAME", "CREATED"})
	for i := range templates {
		template := &templates[i]
		created := ""
		if !template.CreationTimestamp.IsZero() {
			created = template.CreationTimestamp.Time.UTC().Format(time.RFC3339)
		}
		tw.AppendRow(table.Row{template.Name, created})
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

func newAgentTemplateReadCmd(list bool) *cobra.Command {
	cfg := &AgentTemplateGetCfg{}
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get an AgentTemplate",
		Args:  cobra.ExactArgs(1),
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
				if name == "" {
					return errors.New("agent template name must not be empty")
				}
			}
			cfg.Namespace = options.Namespace
			cfg.OutputFormat = format
			cfg.Name = name
			return runGetAgentTemplate(cmd.Context(), options, cfg, cmd.OutOrStdout())
		},
	}
	if list {
		cmd.Use = "list"
		cmd.Short = "List AgentTemplates"
		cmd.Args = cobra.NoArgs
		cmd.Flags().Int64Var(&cfg.PageSize, "page-size", 0, "Number of AgentTemplates per page (0 uses 100; maximum 100)")
		cmd.Flags().StringVar(&cfg.PageToken, "page-token", "", "Token returned by the previous page")
	}
	return cmd
}
