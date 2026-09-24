package kagent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	adkoutputschema "github.com/kagent-dev/kagent/go/adk/pkg/outputschema"
	"github.com/kagent-dev/kagent/go/api/adk"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/internal/translator/adkconfig"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
)

// Compiler translates resolved inputs into a kagent runtime revision.
type Compiler struct {
	config *adkconfig.Builder
}

var _ v2translator.HarnessCompiler = (*Compiler)(nil)

func NewCompiler(ctx krt.HandlerContext, collections v2translator.Collections) *Compiler {
	return &Compiler{config: adkconfig.NewBuilder(ctx, collections)}
}

func (c *Compiler) Compile(ctx context.Context, input *v2translator.HarnessInput) (*v2translator.CompileResult, error) {
	if err := requireModels(input.Root); err != nil {
		return nil, err
	}
	telemetryConfig, _ := v2translator.TelemetryConfigFromProcess()
	traceConfig, logConfig := telemetryConfig.Traces, telemetryConfig.Logs
	compiled, err := c.config.Build(ctx, input.Root)
	if err != nil {
		return nil, err
	}
	if err := applyOutputSchema(compiled.Config, input.OutputSchema); err != nil {
		return nil, err
	}
	template, harness := input.Root.Template, input.Harness
	if err := c.config.ApplyCompaction(compiled, harness, template); err != nil {
		return nil, err
	}
	if memory := harness.Spec.Kagent.Memory; memory != nil {
		name := memory.ModelConfigRef.Name
		model, err := c.config.BuildModel(harness.Namespace, name)
		if err != nil {
			return nil, fmt.Errorf("resolve memory ModelConfig %q: %w", name, err)
		}
		compiled.Config.Memory = &adk.MemoryConfig{TTLDays: memory.TTLDays, Embedding: adk.ModelToEmbeddingConfig(model.Model)}
		compiled.Models = append(compiled.Models, model.Resolved)
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
	card, err := pbconv.ToProtoAgentCard(v2translator.ManagedAgentCard(template))
	if err != nil {
		return nil, fmt.Errorf("convert agent card: %w", err)
	}

	environment := append(compiled.Environment, adkconfig.HarnessEnvironment(harness)...)
	environment = append(environment,
		corev1.EnvVar{Name: env.KagentName.Name(), Value: template.Name + "-" + harness.Name},
		corev1.EnvVar{Name: env.KagentNamespace.Name(), Value: template.Namespace},
		corev1.EnvVar{Name: env.KagentAPIURL.Name(), Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace())},
		corev1.EnvVar{Name: env.KagentGatewayURL.Name(), Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace())},
		corev1.EnvVar{Name: "PORT", Value: "80"},
		corev1.EnvVar{Name: "KAGENT_A2A_GRPC_ADDRESS", Value: "[::]:80"},
		corev1.EnvVar{Name: "KAGENT_PRE_RESPONSE_TRACE_FLUSH", Value: "true"},
	)
	environment = append(environment, telemetryConfig.TraceEnvironment()...)
	environment = append(environment, telemetryConfig.LogEnvironment()...)
	environment = append(environment, telemetryConfig.CaptureEnvironment())
	environment = adkconfig.DedupeEnv(environment)
	provenance, err := c.config.BuildProvenance(ctx, harness, compiled.Templates, compiled.Models, environment)
	if err != nil {
		return nil, fmt.Errorf("build revision provenance: %w", err)
	}
	environment, credentials, err := v2translator.CompileCredentials(input, compiled.Models, environment)
	if err != nil {
		return nil, err
	}
	if traceConfig.Enabled {
		compiled.Egress = append(compiled.Egress, traceConfig.Hostname)
	}
	if logConfig.Enabled {
		compiled.Egress = append(compiled.Egress, logConfig.Hostname)
	}
	slices.Sort(compiled.Egress)
	compiled.Egress = slices.Compact(compiled.Egress)
	return &v2translator.CompileResult{Revision: v2translator.Revision{
		Namespace: template.Namespace, AgentTemplateName: template.Name, HarnessName: harness.Name,
		Image: harness.Spec.Workload.Image, Command: slices.Clone(harness.Spec.Workload.Command), Args: slices.Clone(harness.Spec.Workload.Args),
		Environment: environment, ConfigJSON: configJSON, AgentCard: card,
		WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		Credentials: credentials, Provenance: provenance, EgressDestinations: compiled.Egress,
	}}, nil
}

// applyOutputSchema verifies the portable schema by performing the same
// genai.Schema conversion used by the Go runtime, then records the canonical
// schema for both Go and Python ADK runtimes. This keeps compatibility
// failures at Harness compilation instead of actor startup.
func applyOutputSchema(config *adk.AgentConfig, output *v2translator.ResolvedOutputSchema) error {
	if output == nil {
		return nil
	}
	if _, err := adkoutputschema.ToGenAISchema(output.Schema); err != nil {
		return v2translator.NewValidationError("output schema is incompatible with Go ADK: %v", err)
	}
	config.Output = &adk.OutputConfig{
		JSONSchema: append(json.RawMessage(nil), output.Schema...),
		SHA256:     output.SHA256,
	}
	return nil
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
