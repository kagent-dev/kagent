package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	agentKind        = "Agent"
	agentMaxPageSize = 100
)

// AgentGetCfg configures Agent get and list operations.
type AgentGetCfg struct {
	Namespace    string
	OutputFormat string
	Name         string
	PageSize     int64
	PageToken    string
}

// AgentManifestCfg configures an Agent manifest operation.
type AgentManifestCfg struct {
	OutputFormat string
	File         string
}

type agentLifecycleClient interface {
	CreateAgent(context.Context, *apiv1alpha1.CreateAgentRequest) (*apiv1alpha1.CreateAgentResponse, error)
	UpdateAgent(context.Context, *apiv1alpha1.UpdateAgentRequest) (*apiv1alpha1.UpdateAgentResponse, error)
}

type agentManifestOperation func(context.Context, agentLifecycleClient, *apiv1alpha1.ResourceReference, *apiv1alpha1.StructuredObject, clioutput.Format, io.Writer) error

func runAgentManifest(
	ctx context.Context,
	options connection.Options,
	cfg *AgentManifestCfg,
	out io.Writer,
	operation agentManifestOperation,
) (err error) {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(cfg.File)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var kind struct {
		Kind string `json:"kind"`
	}
	if err := yaml.Unmarshal(data, &kind); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if kind.Kind == "AgentTemplate" {
		return runAgentTemplateManifest(ctx, options, &AgentTemplateManifestCfg{OutputFormat: cfg.OutputFormat, File: cfg.File}, out, applyAgentTemplate)
	}
	ref, resource, err := readAgentManifest(cfg.File, options.Namespace)
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
	return operation(ctx, session.API.Agent, ref, resource, format, out)
}

func readAgentManifest(filename, namespace string) (*apiv1alpha1.ResourceReference, *apiv1alpha1.StructuredObject, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("read Agent manifest %q: %w", filename, err)
	}
	manifest := &apiv1alpha3.Agent{}
	if err := yaml.UnmarshalStrict(data, manifest); err != nil {
		return nil, nil, fmt.Errorf("parse Agent manifest %q: %w", filename, err)
	}
	if manifest.APIVersion != apiv1alpha3.GroupVersion.String() || manifest.Kind != agentKind {
		return nil, nil, fmt.Errorf("agent manifest %q must have apiVersion %q and kind %q", filename, apiv1alpha3.GroupVersion.String(), agentKind)
	}
	if manifest.Name == "" {
		return nil, nil, fmt.Errorf("agent manifest %q must have metadata.name", filename)
	}
	if manifest.Namespace != "" && manifest.Namespace != namespace {
		return nil, nil, fmt.Errorf("agent manifest namespace %q does not match --namespace %q", manifest.Namespace, namespace)
	}
	resource, err := structuredobject.FromGo(manifest, apiv1alpha3.GroupVersion.String(), agentKind, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("encode Agent manifest %q: %w", filename, err)
	}
	return &apiv1alpha1.ResourceReference{Namespace: namespace, Name: manifest.Name}, resource, nil
}

func applyAgent(
	ctx context.Context,
	client agentLifecycleClient,
	ref *apiv1alpha1.ResourceReference,
	resource *apiv1alpha1.StructuredObject,
	format clioutput.Format,
	out io.Writer,
) error {
	created, err := client.CreateAgent(ctx, &apiv1alpha1.CreateAgentRequest{Ref: ref, Resource: resource})
	if status.Code(err) != codes.AlreadyExists {
		if err != nil {
			return fmt.Errorf("apply Agent: %w", err)
		}
		return writeAgentResult(out, format, created, created.GetAgent())
	}
	updated, err := client.UpdateAgent(ctx, &apiv1alpha1.UpdateAgentRequest{Ref: ref, Resource: resource})
	if err != nil {
		return fmt.Errorf("apply Agent: %w", err)
	}
	return writeAgentResult(out, format, updated, updated.GetAgent())
}

func writeAgentResult(w io.Writer, format clioutput.Format, response proto.Message, result *apiv1alpha1.Agent) error {
	if result == nil {
		return errors.New("agent operation returned no Agent")
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(w, response)
	}
	template := &apiv1alpha3.Agent{}
	if err := structuredobject.ToGo(result.GetResource(), agentKind, template, 0); err != nil {
		return fmt.Errorf("decode Agent result: %w", err)
	}
	return writeAgentsTable(w, []apiv1alpha3.Agent{*template}, false, "")
}

// runGetAgent gets one Agent or lists Agents through Kubernetes.
func runGetAgent(ctx context.Context, cfg *AgentGetCfg, out io.Writer) error {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	if err := validateAgentGetCfg(cfg); err != nil {
		return err
	}

	clients, err := commonk8s.NewKagentClientset()
	if err != nil {
		return err
	}
	return getAgents(ctx, clients.ApiV1alpha3().Agents(cfg.Namespace), cfg, format, out)
}

func validateAgentGetCfg(cfg *AgentGetCfg) error {
	if cfg.PageSize < 0 || cfg.PageSize > agentMaxPageSize {
		return fmt.Errorf("page size must be between 1 and %d, or 0 for the default of %d", agentMaxPageSize, agentMaxPageSize)
	}
	if cfg.Name != "" && (cfg.PageSize != 0 || cfg.PageToken != "") {
		return errors.New("pagination flags cannot be used when getting one Agent")
	}
	return nil
}

func getAgents(
	ctx context.Context,
	client typedapiv1alpha3.AgentInterface,
	cfg *AgentGetCfg,
	format clioutput.Format,
	out io.Writer,
) error {
	if cfg.Name != "" {
		template, err := client.Get(ctx, cfg.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get Agent %q: %w", cfg.Name, err)
		}
		if format == clioutput.FormatJSON {
			return clioutput.WriteJSON(out, template)
		}
		return writeAgentsTable(out, []apiv1alpha3.Agent{*template}, false, "")
	}

	pageSize := cfg.PageSize
	if pageSize == 0 {
		pageSize = agentMaxPageSize
	}
	templates, err := client.List(ctx, metav1.ListOptions{Limit: pageSize, Continue: cfg.PageToken})
	if err != nil {
		return fmt.Errorf("list Agents: %w", err)
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteJSON(out, templates)
	}
	return writeAgentsTable(out, templates.Items, true, templates.Continue)
}

func writeAgentsTable(w io.Writer, templates []apiv1alpha3.Agent, list bool, nextPageToken string) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"NAME", "READY", "CREATED"})
	for i := range templates {
		template := &templates[i]
		created := ""
		if !template.CreationTimestamp.IsZero() {
			created = template.CreationTimestamp.Time.UTC().Format(time.RFC3339)
		}
		ready := "UNKNOWN"
		if condition := meta.FindStatusCondition(template.Status.Conditions, apiv1alpha3.AgentConditionReady); condition != nil {
			ready = string(condition.Status)
		}
		tw.AppendRow(table.Row{template.Name, ready, created})
	}

	output := tw.Render()
	if list {
		if nextPageToken != "" {
			output += "\nNext page token: " + nextPageToken
		}
	}
	if _, err := fmt.Fprintln(w, output); err != nil {
		return fmt.Errorf("write Agent output: %w", err)
	}
	return nil
}

// NewGetAgentCmd constructs the Agent get/list command.
func NewGetAgentCmd() *cobra.Command {
	cfg := &AgentGetCfg{}
	cmd := &cobra.Command{
		Use:   "agent [NAME]",
		Short: "Get an Agent or list Agents",
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
			return runGetAgent(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}
	cmd.Flags().Int64Var(&cfg.PageSize, "page-size", 0, "Number of Agents per page (0 uses 100; maximum 100)")
	cmd.Flags().StringVar(&cfg.PageToken, "page-token", "", "Token returned by the previous page")
	return cmd
}

// NewApplyAgentCmd constructs the Agent apply command.
func NewApplyAgentCmd() *cobra.Command {
	return newAgentManifestCmd("apply -f FILE", "Create or update an Agent or AgentTemplate", applyAgent)
}

func newAgentManifestCmd(use, short string, operation agentManifestOperation) *cobra.Command {
	cfg := &AgentManifestCfg{}
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
			return runAgentManifest(cmd.Context(), options, cfg, cmd.OutOrStdout(), operation)
		},
	}
	cmd.Flags().StringVarP(&cfg.File, "file", "f", "", "Path to Agent or AgentTemplate manifest")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}
