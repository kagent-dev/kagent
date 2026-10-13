package kagent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"istio.io/istio/pkg/kube/krt"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/internal/translator/adkconfig"
)

// Compiler translates resolved inputs into a kagent runtime revision.
type Compiler struct {
	settings translator.CompilerSettings
	config   *adkconfig.Builder
}

var _ translator.HarnessCompiler = (*Compiler)(nil)

func NewCompiler(ctx krt.HandlerContext, collections translator.Collections, settings translator.CompilerSettings) *Compiler {
	return &Compiler{settings: settings, config: adkconfig.NewBuilder(ctx, collections)}
}

func (c *Compiler) Compile(ctx context.Context, input *translator.HarnessInput) (*translator.CompileResult, error) {
	telemetryConfig := c.settings.Telemetry
	compiled, err := c.config.Build(ctx, input)
	if err != nil {
		return nil, err
	}
	template, harness := input.Root.Template, input.Harness
	compiled.Config.Runtime = c.settings.RuntimeConfig(input.AgentName, template.Namespace)
	configJSON, err := json.Marshal(compiled.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal agent config: %w", err)
	}
	card, err := pbconv.ToProtoAgentCard(agentCard(input.AgentName, template))
	if err != nil {
		return nil, fmt.Errorf("convert agent card: %w", err)
	}

	harnessEnvironment := adkconfig.HarnessEnvironment(harness)
	environment := append(compiled.Environment, harnessEnvironment...)
	environment = adkconfig.DedupeEnv(environment)
	provenance, err := c.config.BuildProvenance(ctx, harness, compiled.Templates, compiled.Models, environment)
	if err != nil {
		return nil, fmt.Errorf("build revision provenance: %w", err)
	}
	environment, credentials, err := translator.CompileCredentials(input, compiled.Models, environment)
	if err != nil {
		return nil, err
	}
	compiled.Egress = append(compiled.Egress, telemetryConfig.Destinations(environment)...)
	compiled.Egress = append(compiled.Egress, c.settings.APIURL)
	slices.Sort(compiled.Egress)
	compiled.Egress = slices.Compact(compiled.Egress)
	return &translator.CompileResult{Revision: translator.Revision{
		Namespace: template.Namespace,
		Image:     harness.Spec.Workload.Image, Command: slices.Clone(harness.Spec.Workload.Command), Args: slices.Clone(harness.Spec.Workload.Args),
		Environment: environment, ConfigJSON: configJSON, AgentCard: card,
		WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		Credentials: credentials, Provenance: provenance, EgressDestinations: compiled.Egress,
	}}, nil
}

// agentCard is the managed card plus the capabilities only the kagent ADK
// runtimes implement.
func agentCard(agentName string, template *translator.TemplateConfiguration) *a2atype.AgentCard {
	card := translator.ManagedAgentCard(agentName, template)
	card.Capabilities.Extensions = append(card.Capabilities.Extensions, apia2a.UsageExtension())
	return card
}
