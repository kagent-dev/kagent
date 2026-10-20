package kagent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"buf.build/go/protovalidate"
	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"istio.io/istio/pkg/kube/krt"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/internal/translator/adkconfig"
)

// Compiler translates resolved inputs into a kagent runtime revision.
type Compiler struct {
	settings *configv1.RuntimeDefaultsConfig
	config   *adkconfig.Builder
}

var _ translator.HarnessCompiler = (*Compiler)(nil)

func NewCompiler(ctx krt.HandlerContext, collections translator.Collections, settings *configv1.RuntimeDefaultsConfig) *Compiler {
	return &Compiler{settings: settings, config: adkconfig.NewBuilder(ctx, collections)}
}

func (c *Compiler) Compile(ctx context.Context, input *translator.HarnessInput) (*translator.CompileResult, error) {
	telemetryConfig, err := translator.TelemetryConfigFromSettings(c.settings.GetTelemetry())
	if err != nil {
		return nil, err
	}
	compiled, err := c.config.Build(ctx, input)
	if err != nil {
		return nil, err
	}
	template, harness := input.Root.Template, input.Harness
	configJSON, err := runtimeconfig.Marshal(&configv1.ADKConfig{
		Runtime: translator.RuntimeConfig(c.settings, input.AgentName, template.Namespace),
		Agent:   compiled.Config,
	})
	if err != nil {
		var invalid *protovalidate.ValidationError
		if errors.As(err, &invalid) {
			return nil, translator.NewValidationError("invalid agent config: %w", err)
		}
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
	environment, credentials, tokenPassthroughOrigins, err := translator.CompileCredentials(input, compiled.Models, environment)
	if err != nil {
		return nil, err
	}
	compiled.Egress = append(compiled.Egress, telemetryConfig.Destinations(environment)...)
	compiled.Egress = append(compiled.Egress, c.settings.GetControlPlaneUrl())
	slices.Sort(compiled.Egress)
	compiled.Egress = slices.Compact(compiled.Egress)
	return &translator.CompileResult{Revision: translator.Revision{
		Namespace: template.Namespace,
		Command:   slices.Clone(harness.Spec.Workload.Command), Args: slices.Clone(harness.Spec.Workload.Args),
		Environment: environment, ConfigJSON: configJSON, AgentCard: card,
		SkillImages:    compiled.SkillImages,
		WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		TokenPassthroughOrigins: tokenPassthroughOrigins, Credentials: credentials, Provenance: provenance, EgressDestinations: compiled.Egress,
	}}, nil
}

// agentCard is the managed card plus the capabilities only the kagent ADK
// runtimes implement.
func agentCard(agentName string, template *translator.TemplateConfiguration) *a2atype.AgentCard {
	card := translator.ManagedAgentCard(agentName, template)
	card.Capabilities.Extensions = append(card.Capabilities.Extensions, apia2a.UsageExtension())
	return card
}
