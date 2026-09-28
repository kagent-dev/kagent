package adkconfig

import (
	"encoding/json"
	"fmt"

	adkoutputschema "github.com/kagent-dev/kagent/go/adk/pkg/outputschema"
	"github.com/kagent-dev/kagent/go/api/adk"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// modelResult is the runtime configuration contributed by one ModelConfig.
type modelResult struct {
	Resolved    *v2translator.ResolvedModelConfig
	Model       adk.Model
	Environment []corev1.EnvVar
	Egress      []string
}

// buildModel translates a standalone ModelConfig without building an agent.
func (c *Builder) buildModel(namespace, name string) (*modelResult, error) {
	resolved := krt.FetchOne(c.ctx, c.collections.ResolvedModelConfigs, krt.FilterObjectName(types.NamespacedName{Namespace: namespace, Name: name}))
	if resolved == nil {
		return nil, fmt.Errorf("model config %q not found", name)
	}
	if failures := resolved.SemanticFailures; len(failures) > 0 {
		return nil, v2translator.NewValidationError("ModelConfig %q: %s", name, failures[0].Message)
	}
	if failures := resolved.ReferenceFailures; len(failures) > 0 {
		return nil, fmt.Errorf("ModelConfig %q: %s", name, failures[0].Message)
	}
	runtime, err := resolveModel(resolved)
	if err != nil {
		return nil, err
	}
	if runtime.HasUnsupportedVolumes {
		return nil, v2translator.NewValidationError("ModelConfig requires volume mounts unsupported by Substrate ActorTemplate")
	}
	return &modelResult{
		Resolved: resolved, Model: runtime.Model, Environment: runtime.Environment,
		Egress: agentConfigDestinations(&adk.AgentConfig{}, resolved.Config, runtime.Model),
	}, nil
}

// applyCompaction translates the Harness's kagent compaction policy into the
// ADK context configuration of a compiled root agent. Compaction is a property
// of the runner that drives the root agent, so it is runtime policy on the
// Harness rather than portable behavior on the AgentTemplate.
//
// A summarizer ModelConfig other than the agent's own is resolved like the
// agent model: its runtime configuration lands in config.json, its credentials
// and egress join the revision, and it joins the provenance so a change to it
// compiles a new revision. The agent's own model is left out because the
// runtime already summarizes with it by default.
func (c *Builder) applyCompaction(result *Result, harness *v2translator.HarnessConfiguration, template *v2translator.TemplateConfiguration) error {
	spec := harness.Spec.Kagent.Compaction
	if spec == nil {
		return nil
	}
	compaction := &adk.AgentCompressionConfig{
		CompactionInterval: spec.CompactionInterval,
		OverlapSize:        spec.OverlapSize,
		TokenThreshold:     spec.TokenThreshold,
		EventRetentionSize: spec.EventRetentionSize,
	}
	if summarizer := spec.Summarizer; summarizer != nil {
		compaction.PromptTemplate = summarizer.PromptTemplate
		if ref := summarizer.ModelConfigRef; ref != nil && !isAgentModel(template, ref.Name) {
			model, err := c.buildModel(harness.Namespace, ref.Name)
			if err != nil {
				return fmt.Errorf("resolve summarizer ModelConfig %q: %w", ref.Name, err)
			}
			compaction.SummarizerModel = model.Model
			result.Models = append(result.Models, model.Resolved)
			result.Environment = append(result.Environment, model.Environment...)
			result.Egress = append(result.Egress, model.Egress...)
		}
	}
	result.Config.ContextConfig = &adk.AgentContextConfig{Compaction: compaction}
	return nil
}

func isAgentModel(template *v2translator.TemplateConfiguration, name string) bool {
	return template.Spec.ModelConfig != nil && template.Spec.ModelConfig.Name == name
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

func (c *Builder) applyMemory(result *Result, harness *v2translator.HarnessConfiguration) error {
	memory := harness.Spec.Kagent.Memory
	if memory == nil {
		return nil
	}
	name := memory.ModelConfigRef.Name
	model, err := c.buildModel(harness.Namespace, name)
	if err != nil {
		return fmt.Errorf("resolve memory ModelConfig %q: %w", name, err)
	}
	result.Config.Memory = &adk.MemoryConfig{TTLDays: memory.TTLDays, Embedding: adk.ModelToEmbeddingConfig(model.Model)}
	result.Models = append(result.Models, model.Resolved)
	result.Environment = append(result.Environment, model.Environment...)
	result.Egress = append(result.Egress, model.Egress...)
	return nil
}
