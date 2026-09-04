package byo

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/internal/translator/adkconfig"
	"istio.io/istio/pkg/kube/krt"
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
	cardJSON, err := json.Marshal(agentTemplateCard(template))
	if err != nil {
		return nil, fmt.Errorf("marshal agent card: %w", err)
	}
	environment := adkconfig.DedupeEnv(append(compiled.Environment, adkconfig.HarnessEnvironment(harness)...))
	provenance, err := c.config.BuildProvenance(ctx, harness, compiled.Templates, compiled.Models, environment)
	if err != nil {
		return nil, fmt.Errorf("build revision provenance: %w", err)
	}
	environment, err = c.config.ResolveEnvironment(ctx, template.Namespace, environment)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime environment: %w", err)
	}
	slices.Sort(compiled.Egress)

	return &v2translator.CompileResult{Revision: v2translator.Revision{
		Namespace: template.Namespace, AgentTemplateName: template.Name, HarnessName: harness.Name,
		Image: harness.Spec.Workload.Image, Command: harness.Spec.Workload.Command, Args: harness.Spec.Workload.Args,
		Environment: environment, ConfigJSON: configJSON, AgentCardJSON: cardJSON,
		WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		Provenance: provenance, EgressDestinations: slices.Compact(compiled.Egress),
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
