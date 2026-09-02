package kagent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	"github.com/kagent-dev/kagent/go/core/v2/translator/adkconfig"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
)

const hitlExtensionURI = "https://kagent.dev/extensions/hitl/v1"

// Compiler translates resolved inputs into a kagent runtime revision.
type Compiler struct {
	config *adkconfig.Builder
}

var _ v2translator.HarnessCompiler = (*Compiler)(nil)

func NewCompiler(ctx krt.HandlerContext, collections v2translator.Collections) *Compiler {
	return &Compiler{config: adkconfig.NewBuilder(ctx, collections)}
}

func (c *Compiler) Compile(ctx context.Context, input *v2translator.HarnessInput) (*v2translator.Compilation, error) {
	if err := requireModels(input.Root); err != nil {
		return nil, err
	}
	compiled, err := c.config.Build(ctx, input.Root)
	if err != nil {
		return nil, err
	}
	template, harness := input.Root.Template, input.Harness
	if memory := harness.Spec.Kagent.Memory; memory != nil {
		name := memory.ModelConfigRef.Name
		model, err := c.config.BuildModel(ctx, harness.Namespace, name)
		if err != nil {
			return nil, fmt.Errorf("resolve memory ModelConfig %q: %w", name, err)
		}
		compiled.Config.Memory = &adk.MemoryConfig{TTLDays: memory.TTLDays, Embedding: adk.ModelToEmbeddingConfig(model.Model)}
		compiled.Models = append(compiled.Models, model.Config)
		compiled.Environment = append(compiled.Environment, model.Environment...)
		compiled.Egress = append(compiled.Egress, model.Egress...)
	}
	// The Python runtime needs an async SQLite driver; the Go runtime accepts
	// this URL and strips the driver before opening the same durable database.
	compiled.Config.SessionDBURL = "sqlite+aiosqlite:////data/sessions.db"
	configJSON, err := json.Marshal(compiled.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal agent config: %w", err)
	}
	cardJSON, err := json.Marshal(agentTemplateCard(template))
	if err != nil {
		return nil, fmt.Errorf("marshal agent card: %w", err)
	}

	environment := append(compiled.Environment, adkconfig.HarnessEnvironment(harness)...)
	environment = append(environment,
		corev1.EnvVar{Name: env.KagentName.Name(), Value: template.Name + "-" + harness.Name},
		corev1.EnvVar{Name: env.KagentNamespace.Name(), Value: template.Namespace},
		corev1.EnvVar{Name: env.KagentURL.Name(), Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace())},
		corev1.EnvVar{Name: env.KagentGRPCURL.Name(), Value: fmt.Sprintf("%s.%s:8084", utils.GetControllerName(), utils.GetResourceNamespace())},
		corev1.EnvVar{Name: "PORT", Value: "80"},
		corev1.EnvVar{Name: "KAGENT_A2A_GRPC_ADDRESS", Value: "[::]:80"},
		corev1.EnvVar{Name: "KAGENT_PRE_RESPONSE_TRACE_FLUSH", Value: "true"},
	)
	environment = adkconfig.DedupeEnv(environment)
	provenance, err := c.config.BuildProvenance(ctx, harness, compiled.Templates, compiled.Models, environment)
	if err != nil {
		return nil, fmt.Errorf("build revision provenance: %w", err)
	}
	environment, err = c.config.ResolveEnvironment(ctx, template.Namespace, environment)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime environment: %w", err)
	}
	slices.Sort(compiled.Egress)
	return &v2translator.Compilation{Revision: v2translator.Revision{
		Namespace: template.Namespace, AgentTemplateName: template.Name, HarnessName: harness.Name,
		Image: harness.Spec.Workload.Image, Environment: environment, ConfigJSON: configJSON, AgentCardJSON: cardJSON,
		WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		Provenance: provenance, EgressDestinations: slices.Compact(compiled.Egress),
	}}, nil
}

func requireModels(input *v2translator.AgentInput) error {
	if input.ResolvedModelConfig == nil || input.ResolvedModelConfig.Config == nil {
		return v2translator.NewValidationError("kagent ModelConfig is required")
	}
	for _, binding := range input.Shared {
		if err := requireModels(binding.Agent); err != nil {
			return err
		}
	}
	return nil
}

func agentTemplateCard(template *v1alpha3.AgentTemplate) *a2atype.AgentCard {
	return &a2atype.AgentCard{
		Name: strings.ReplaceAll(template.Name, "-", "_"), Description: template.Spec.Description, Version: "v1",
		SupportedInterfaces: []*a2atype.AgentInterface{{URL: "http://127.0.0.1:80", ProtocolBinding: a2atype.TransportProtocolGRPC, ProtocolVersion: a2atype.Version}},
		Capabilities: a2atype.AgentCapabilities{Streaming: true, Extensions: []a2atype.AgentExtension{{
			URI: hitlExtensionURI, Description: "Human in the loop for tool approval, ask user, and nested subagents",
		}}},
		Skills: []a2atype.AgentSkill{}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"},
	}
}
