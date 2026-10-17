package byo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"buf.build/go/protovalidate"
	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/internal/translator/adkconfig"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// Compiler translates resolved inputs into a BYO A2A runtime revision.
type Compiler struct {
	config   *adkconfig.Builder
	settings *configv1.RuntimeDefaultsConfig
}

var _ translator.HarnessCompiler = (*Compiler)(nil)

func NewCompiler(ctx krt.HandlerContext, collections translator.Collections, settings *configv1.RuntimeDefaultsConfig) *Compiler {
	return &Compiler{settings: settings, config: adkconfig.NewBuilder(ctx, collections)}
}

func (c *Compiler) Compile(ctx context.Context, input *translator.HarnessInput) (*translator.CompileResult, error) {
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
	card, err := pbconv.ToProtoAgentCard(agentTemplateCard(input.AgentName, template))
	if err != nil {
		return nil, fmt.Errorf("convert agent card: %w", err)
	}
	// An opaque image may export to its own backend, so its own values win.
	telemetryConfig, err := translator.TelemetryConfigFromSettings(c.settings.GetTelemetry())
	if err != nil {
		return nil, err
	}
	environment := append([]corev1.EnvVar(nil), compiled.Environment...)
	environment = append(environment, adkconfig.HarnessEnvironment(harness)...)
	environment = adkconfig.DedupeEnv(environment)
	if telemetryConfig.Enabled() {
		environment = telemetryConfig.TelemetryEnvironment(tracing.RuntimeTelemetry{
			AgentName: input.AgentName, AgentNamespace: template.Namespace,
		}, environment)
	}
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

	return &translator.CompileResult{Revision: translator.Revision{
		Namespace: template.Namespace,
		Command:   harness.Spec.Workload.Command, Args: harness.Spec.Workload.Args,
		Environment: environment, ConfigJSON: configJSON, AgentCard: card,
		SkillImages:    compiled.SkillImages,
		WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		TokenPassthroughOrigins: tokenPassthroughOrigins, Credentials: credentials, Provenance: provenance, EgressDestinations: slices.Compact(compiled.Egress),
	}}, nil
}

func agentTemplateCard(agentName string, template *translator.TemplateConfiguration) *a2atype.AgentCard {
	return &a2atype.AgentCard{
		Name: strings.ReplaceAll(agentName, "-", "_"), Description: template.Spec.Description, Version: "v1",
		SupportedInterfaces: []*a2atype.AgentInterface{{URL: "http://127.0.0.1:80", ProtocolBinding: a2atype.TransportProtocolGRPC, ProtocolVersion: a2atype.Version}},
		Capabilities:        a2atype.AgentCapabilities{Streaming: true}, Skills: []a2atype.AgentSkill{},
		DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"},
	}
}
