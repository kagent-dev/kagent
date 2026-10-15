package adkconfig

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	adkoutputschema "github.com/kagent-dev/kagent/go/adk/pkg/outputschema"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

// modelResult is the runtime configuration contributed by one ModelConfig.
type modelResult struct {
	Resolved    *translator.ResolvedModelConfig
	Model       *configv1.ADKModelConfig
	Environment []corev1.EnvVar
}

// buildModel translates a standalone ModelConfig without building an agent.
func buildModel(resolved *translator.ResolvedModelConfig) (*modelResult, error) {
	if resolved == nil {
		return nil, fmt.Errorf("auxiliary model must be resolved before rendering")
	}
	runtime, err := resolveModel(resolved)
	if err != nil {
		return nil, err
	}
	if runtime.HasUnsupportedVolumes {
		return nil, translator.NewValidationError("ModelConfig requires volume mounts unsupported by Substrate ActorTemplate")
	}
	return &modelResult{
		Resolved: resolved, Model: runtime.Model, Environment: runtime.Environment,
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
func applyCompaction(result *Result, harness *translator.HarnessConfiguration, template *translator.TemplateConfiguration, resolved *translator.ResolvedModelConfig) error {
	spec := harness.Spec.Kagent.Compaction
	if spec == nil {
		return nil
	}
	compaction := &configv1.ADKCompressionConfig{
		CompactionInterval: runtimeInt(spec.CompactionInterval),
		OverlapSize:        runtimeInt(spec.OverlapSize),
		TokenThreshold:     runtimeInt(spec.TokenThreshold),
		EventRetentionSize: runtimeInt(spec.EventRetentionSize),
	}
	if summarizer := spec.Summarizer; summarizer != nil {
		compaction.PromptTemplate = summarizer.PromptTemplate
		if ref := summarizer.ModelConfigRef; ref != nil && !isAgentModel(template, ref.Name) {
			model, err := buildModel(resolved)
			if err != nil {
				return fmt.Errorf("resolve summarizer ModelConfig %q: %w", ref.Name, err)
			}
			compaction.SummarizerModel = model.Model
			result.Models = append(result.Models, model.Resolved)
			result.Environment = append(result.Environment, model.Environment...)
		}
	}
	result.Config.ContextConfig = &configv1.ADKContextConfig{Compaction: compaction}
	return nil
}

func isAgentModel(template *translator.TemplateConfiguration, name string) bool {
	return template.Spec.ModelConfig != nil && template.Spec.ModelConfig.Name == name
}

// applyOutputSchema verifies the portable schema by performing the same
// genai.Schema conversion used by the Go runtime, then records the canonical
// schema for both Go and Python ADK runtimes. This keeps compatibility
// failures at Harness compilation instead of actor startup.
func applyOutputSchema(config *configv1.ADKAgentConfig, output *translator.ResolvedOutputSchema) error {
	if output == nil {
		return nil
	}
	if _, err := adkoutputschema.ToGenAISchema(output.Schema); err != nil {
		return translator.NewValidationError("output schema is incompatible with Go ADK: %v", err)
	}
	config.Output = &configv1.ADKOutputConfig{
		JsonSchema: append(json.RawMessage(nil), output.Schema...),
		Sha256:     output.SHA256,
	}
	return nil
}

func requireModels(input *translator.AgentInput) error {
	if input.ResolvedModelConfig == nil || input.ResolvedModelConfig.Config == nil {
		return translator.NewValidationError("kagent ModelConfig is required")
	}
	for _, binding := range input.Shared {
		if err := requireModels(binding.Agent); err != nil {
			return err
		}
	}
	return nil
}

func applyMemory(result *Result, harness *translator.HarnessConfiguration, resolved *translator.ResolvedModelConfig) error {
	memory := harness.Spec.Kagent.Memory
	if memory == nil {
		return nil
	}
	name := memory.ModelConfigRef.Name
	model, err := buildModel(resolved)
	if err != nil {
		return fmt.Errorf("resolve memory ModelConfig %q: %w", name, err)
	}
	result.Config.Memory = &configv1.ADKMemoryConfig{TtlDays: int64(memory.TTLDays), Embedding: runtimeconfig.ModelToEmbeddingConfig(model.Model)}
	result.Models = append(result.Models, model.Resolved)
	result.Environment = append(result.Environment, model.Environment...)
	return nil
}
