package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	typedapiv1alpha3 "github.com/kagent-dev/kagent/go/api/clientset/versioned/typed/api/v1alpha3"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	commonk8s "github.com/kagent-dev/kagent/go/core/cli/internal/common/k8s"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
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
	CreateAgentTemplate(context.Context, *apiv1alpha1.CreateAgentTemplateRequest) (*apiv1alpha1.CreateAgentTemplateResponse, error)
	UpdateAgentTemplate(context.Context, *apiv1alpha1.UpdateAgentTemplateRequest) (*apiv1alpha1.UpdateAgentTemplateResponse, error)
}

type agentTemplateManifestOperation func(context.Context, lifecycleClient, *apiv1alpha1.ResourceReference, *apiv1alpha1.StructuredObject, clioutput.Format, io.Writer) error

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
	return operation(ctx, session.Client.AgentTemplate, ref, resource, format, out)
}

func readAgentTemplateManifest(filename, namespace string) (*apiv1alpha1.ResourceReference, *apiv1alpha1.StructuredObject, error) {
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
	return &apiv1alpha1.ResourceReference{Namespace: namespace, Name: manifest.Name}, resource, nil
}

func applyAgentTemplate(
	ctx context.Context,
	client lifecycleClient,
	ref *apiv1alpha1.ResourceReference,
	resource *apiv1alpha1.StructuredObject,
	format clioutput.Format,
	out io.Writer,
) error {
	created, err := client.CreateAgentTemplate(ctx, &apiv1alpha1.CreateAgentTemplateRequest{Ref: ref, Resource: resource})
	if status.Code(err) != codes.AlreadyExists {
		if err != nil {
			return fmt.Errorf("apply AgentTemplate: %w", err)
		}
		return writeAgentTemplateResult(out, format, created, created.GetAgentTemplate())
	}
	updated, err := client.UpdateAgentTemplate(ctx, &apiv1alpha1.UpdateAgentTemplateRequest{Ref: ref, Resource: resource})
	if err != nil {
		return fmt.Errorf("apply AgentTemplate: %w", err)
	}
	return writeAgentTemplateResult(out, format, updated, updated.GetAgentTemplate())
}

func writeAgentTemplateResult(w io.Writer, format clioutput.Format, response proto.Message, result *apiv1alpha1.AgentTemplate) error {
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

// NewApplyAgentTemplateCmd constructs the AgentTemplate apply command.
func NewApplyAgentTemplateCmd() *cobra.Command {
	return newAgentTemplateManifestCmd("apply -f FILE", "Create or update an AgentTemplate", applyAgentTemplate)
}

func newAgentTemplateManifestCmd(use, short string, operation agentTemplateManifestOperation) *cobra.Command {
	cfg := &AgentTemplateManifestCfg{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			format, err := clioutput.FromCommand(cmd)
			if err != nil {
				return err
			}
			cfg.OutputFormat = format
			return runAgentTemplateManifest(cmd.Context(), options, cfg, cmd.OutOrStdout(), operation)
		},
	}
	cmd.Flags().StringVarP(&cfg.File, "file", "f", "", "Path to AgentTemplate manifest")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}
