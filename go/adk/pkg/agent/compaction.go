package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/adk/v2/session/compaction"
)

// summarizerTimeout bounds one summarization call. Compaction runs inside the
// run loop, the post-invocation pass from a defer, so a summarizer that never
// answers would hold the turn open with nothing to show for it. The ADK runner
// applies the same bound to the summarizer it installs when none is given.
const summarizerTimeout = 60 * time.Second

// toolNameInstruction is appended to a custom summarizer prompt so a summary
// keeps the registered, namespace-prefixed tool and agent names the model has
// to call by. The Python runtime appends the same text; ADK's default prompt
// already asks for exact tool names.
const toolNameInstruction = "\n\nIMPORTANT: When referencing any tools or agents that were used," +
	" you MUST preserve their exact registered names including any" +
	" namespace prefixes (for example: 'kagent__default__agent_name')." +
	" Never shorten, abbreviate, or rephrase tool or agent names."

// CompactionConfig translates the agent's context configuration into the ADK
// runner's compaction configuration. It returns nil when the agent configures
// no compaction, so the runner behaves exactly as without the feature.
//
// A dedicated summarizer model or a custom prompt template yields an LLM
// summarizer built here; otherwise the runner summarizes with the agent's own
// model and ADK's default prompt, which is what the Python runtime does too.
func CompactionConfig(ctx context.Context, agentConfig *adk.AgentConfig) (*compaction.Config, error) {
	log := logging.FromContext(ctx)
	if agentConfig == nil || agentConfig.ContextConfig == nil || agentConfig.ContextConfig.Compaction == nil {
		log.InfoContext(ctx, "context compaction not configured")
		return nil, nil
	}
	settings := agentConfig.ContextConfig.Compaction

	cfg, ignored, err := compactionStrategies(settings)
	if err != nil {
		return nil, err
	}
	for _, reason := range ignored {
		log.WarnContext(ctx, "ignoring part of the context compaction configuration", "reason", reason)
	}

	summarizer, err := compactionSummarizer(ctx, settings, agentConfig.Model)
	if err != nil {
		return nil, err
	}
	cfg.Summarizer = summarizer

	log.InfoContext(ctx, "context compaction enabled",
		"compaction_interval", cfg.CompactionInterval,
		"overlap_size", cfg.OverlapSize,
		"token_threshold", cfg.TokenThreshold,
		"event_retention_size", cfg.EventRetentionSize,
		"summarizer_model", summarizerModelDescription(settings, agentConfig.Model),
		"custom_prompt_template", settings.PromptTemplate != "")
	return &cfg, nil
}

// compactionStrategies maps the settings delivered in config.json onto the two
// ADK strategies: the sliding window (compaction_interval, overlap_size) and
// tail retention (token_threshold, event_retention_size). An unset setting
// leaves its strategy off.
//
// A setting whose partner is missing is left out and reported rather than
// rejected, mirroring the Python runtime, where adk-python runs tail retention
// only when both of its settings are present. What remains is validated the
// way the runner validates it, so a configuration the runner would refuse
// fails here at startup with the reason instead of on the first turn.
func compactionStrategies(settings *adk.AgentCompressionConfig) (compaction.Config, []string, error) {
	cfg := compaction.Config{
		CompactionInterval: intValue(settings.CompactionInterval),
		OverlapSize:        intValue(settings.OverlapSize),
		TokenThreshold:     intValue(settings.TokenThreshold),
		EventRetentionSize: intValue(settings.EventRetentionSize),
	}
	var ignored []string
	if cfg.OverlapSize > 0 && cfg.CompactionInterval == 0 {
		ignored = append(ignored, "overlap_size is set without compaction_interval, so there is no sliding window to overlap")
		cfg.OverlapSize = 0
	}
	switch {
	case cfg.TokenThreshold > 0 && cfg.EventRetentionSize == 0:
		ignored = append(ignored, "token_threshold needs an event_retention_size greater than zero, so tail-retention compaction stays off")
		cfg.TokenThreshold = 0
	case cfg.EventRetentionSize > 0 && cfg.TokenThreshold == 0:
		ignored = append(ignored, "event_retention_size needs a token_threshold greater than zero, so tail-retention compaction stays off")
		cfg.EventRetentionSize = 0
	}
	if err := cfg.Validate(); err != nil {
		return compaction.Config{}, nil, fmt.Errorf("invalid context compaction configuration: %w", err)
	}
	return cfg, ignored, nil
}

// compactionSummarizer builds the LLM summarizer for a dedicated summarizer
// model or a custom prompt template. It returns nil when neither is set, which
// leaves the runner to summarize with the agent's own model and ADK's default
// prompt.
func compactionSummarizer(ctx context.Context, settings *adk.AgentCompressionConfig, agentModel adk.Model) (compaction.Summarizer, error) {
	modelConfig := settings.SummarizerModel
	if modelConfig == nil {
		if settings.PromptTemplate == "" {
			return nil, nil
		}
		// A custom prompt without a dedicated model runs on the agent's own model.
		modelConfig = agentModel
	}
	if modelConfig == nil {
		return nil, fmt.Errorf("context compaction prompt template needs a model to run on")
	}
	llm, err := CreateLLM(ctx, modelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create context compaction summarizer model: %w", err)
	}
	summarizer, err := compaction.NewLLMSummarizer(compaction.LLMSummarizerConfig{
		Model:                 llm,
		PromptTemplate:        summarizerPromptTemplate(settings.PromptTemplate),
		GenerateContentConfig: generateContentConfig(modelConfig),
		Timeout:               summarizerTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("invalid context compaction summarizer: %w", err)
	}
	return summarizer, nil
}

// summarizerPromptTemplate returns the prompt the summarizer runs with: ADK's
// default when no template is configured (the empty string selects it), and
// otherwise the configured template with the tool-name instruction appended.
func summarizerPromptTemplate(custom string) string {
	if custom == "" {
		return ""
	}
	return custom + toolNameInstruction
}

// summarizerModelDescription names the model that writes the summaries, for
// the startup log.
func summarizerModelDescription(settings *adk.AgentCompressionConfig, agentModel adk.Model) string {
	if settings.SummarizerModel != nil {
		return settings.SummarizerModel.GetType()
	}
	if agentModel == nil {
		return "none"
	}
	return agentModel.GetType() + " (agent model)"
}

func intValue(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
