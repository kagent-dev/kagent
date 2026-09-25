package byo

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/internal/translator/adkconfig"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
)

// Compiler translates resolved inputs into a BYO A2A runtime revision.
type Compiler struct{ config *adkconfig.Builder }

var _ v2translator.HarnessCompiler = (*Compiler)(nil)

func NewCompiler(ctx krt.HandlerContext, collections v2translator.Collections) *Compiler {
	return &Compiler{config: adkconfig.NewBuilder(ctx, collections)}
}

func (c *Compiler) Compile(ctx context.Context, input *v2translator.HarnessInput) (*v2translator.CompileResult, error) {
	compiled, err := c.config.Build(ctx, input.Root)
	if err != nil {
		return nil, err
	}
	template, harness := input.Root.Template, input.Harness
	configJSON, err := json.Marshal(compiled.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal agent config: %w", err)
	}
	card, err := pbconv.ToProtoAgentCard(agentTemplateCard(template))
	if err != nil {
		return nil, fmt.Errorf("convert agent card: %w", err)
	}
	// An opaque image may export to its own backend, so its own values win.
	telemetryConfig, _ := v2translator.TelemetryConfigFromProcess()
	environment := append([]corev1.EnvVar(nil), compiled.Environment...)
	if telemetryConfig.Enabled() {
		environment = append(environment, v2translator.DefaultsEnvironment()...)
		environment = append(environment, telemetryConfig.TelemetryEnvironment(tracing.RuntimeTelemetry{
			AgentName: template.Name + "-" + harness.Name, AgentNamespace: template.Namespace,
		}, "")...)
		compiled.Egress = append(compiled.Egress, telemetryConfig.Destinations()...)
	}
	environment = append(environment, adkconfig.HarnessEnvironment(harness)...)
	environment = adkconfig.DedupeEnv(append(environment,
		corev1.EnvVar{Name: env.KagentAPIURL.Name(), Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace())},
	))
	provenance, err := c.config.BuildProvenance(ctx, harness, compiled.Templates, compiled.Models, environment)
	if err != nil {
		return nil, fmt.Errorf("build revision provenance: %w", err)
	}
	environment, credentials, err := v2translator.CompileCredentials(input, compiled.Models, environment)
	if err != nil {
		return nil, err
	}
	compiled.Egress = append(compiled.Egress, utils.GetControllerName()+"."+utils.GetResourceNamespace())
	slices.Sort(compiled.Egress)

	return &v2translator.CompileResult{Revision: v2translator.Revision{
		Namespace: template.Namespace, AgentTemplateName: template.Name, HarnessName: harness.Name,
		Image: harness.Spec.Workload.Image, Command: harness.Spec.Workload.Command, Args: harness.Spec.Workload.Args,
		Environment: environment, ConfigJSON: configJSON, AgentCard: card,
		WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		Credentials: credentials, Provenance: provenance, EgressDestinations: slices.Compact(compiled.Egress),
	}}, nil
}

func agentTemplateCard(template *v1alpha3.AgentTemplate) *a2atype.AgentCard {
	return &a2atype.AgentCard{
		Name: strings.ReplaceAll(template.Name, "-", "_"), Description: template.Spec.Description, Version: "v1",
		SupportedInterfaces: []*a2atype.AgentInterface{{URL: "http://127.0.0.1:80", ProtocolBinding: a2atype.TransportProtocolGRPC, ProtocolVersion: a2atype.Version}},
		Capabilities:        a2atype.AgentCapabilities{Streaming: true}, Skills: []a2atype.AgentSkill{},
		DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"},
	}
}
