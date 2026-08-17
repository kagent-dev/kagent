package a2a

import (
	"context"
	"fmt"
	"iter"
	"math"
	"slices"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// usageTotalMetadataKey is the A2A metadata key carrying the aggregated
// token usage of a task. The value has the same shape as the per-event
// adk_usage_metadata entry, plus modelVersion.
var usageTotalMetadataKey = GetKAgentMetadataKey("usage_total")

// turnUsage accumulates token usage across the ADK events of one execution so
// the aggregated total can be emitted on the terminal status update. Partial
// (streaming chunk) events are skipped: each LLM call reports its usage on the
// final non-partial event, so summing partials would double-count.
type turnUsage struct {
	promptTokens        int64
	completionTokens    int64
	thoughtsTokens      int64
	cachedContentTokens int64
	totalTokens         int64
	modelVersion        string
}

type turnUsageContextKey struct{}

// withTurnUsage binds an accumulator to ctx. The upstream ADK executor derives
// its ExecutorContext from this context, so the per-event and post-execution
// callbacks reach the accumulator of the execution they belong to.
func withTurnUsage(ctx context.Context, usage *turnUsage) context.Context {
	return context.WithValue(ctx, turnUsageContextKey{}, usage)
}

func turnUsageFrom(ctx context.Context) *turnUsage {
	usage, _ := ctx.Value(turnUsageContextKey{}).(*turnUsage)
	return usage
}

// usageObservingRunnerProvider mirrors the ADK executor's default runner
// provider and wraps the runner so every ADK event is seen. Events are counted
// before A2A conversion, which drops the ones it cannot turn into an artifact
// (a paused tool call, for instance) even though they report token usage.
func usageObservingRunnerProvider(baseConfig runner.Config) adka2a.RunnerProvider {
	return func(_ context.Context, _ *a2asrv.ExecutorContext, executorPlugin *plugin.Plugin) (adka2a.RunnerConfig, adka2a.Runner, error) {
		if baseConfig.Agent == nil {
			return adka2a.RunnerConfig{}, nil, fmt.Errorf("runner.Config.Agent is not provided")
		}
		if baseConfig.SessionService == nil {
			return adka2a.RunnerConfig{}, nil, fmt.Errorf("runner.Config.SessionService is not provided")
		}

		cfg := baseConfig
		cfg.PluginConfig.Plugins = append(slices.Clone(cfg.PluginConfig.Plugins), executorPlugin)
		adkRunner, err := runner.New(cfg)
		if err != nil {
			return adka2a.RunnerConfig{}, nil, err
		}
		return adka2a.RunnerConfig{
				AppName:        cfg.AppName,
				Agent:          cfg.Agent,
				SessionService: cfg.SessionService,
			},
			&usageObservingRunner{runner: adkRunner}, nil
	}
}

type usageObservingRunner struct {
	runner *runner.Runner
}

func (r *usageObservingRunner) Run(
	ctx context.Context,
	userID, sessionID string,
	message *genai.Content,
	config adkagent.RunConfig,
) iter.Seq2[*adksession.Event, error] {
	events := r.runner.Run(ctx, userID, sessionID, message, config)
	usage := turnUsageFrom(ctx)
	if usage == nil {
		return events
	}
	return func(yield func(*adksession.Event, error) bool) {
		for event, err := range events {
			if err == nil {
				usage.add(event)
			}
			if !yield(event, err) {
				return
			}
		}
	}
}

func (u *turnUsage) add(event *adksession.Event) {
	if u == nil || event == nil || event.Partial || event.UsageMetadata == nil {
		return
	}
	u.promptTokens += int64(event.UsageMetadata.PromptTokenCount)
	u.completionTokens += int64(event.UsageMetadata.CandidatesTokenCount)
	u.thoughtsTokens += int64(event.UsageMetadata.ThoughtsTokenCount)
	u.cachedContentTokens += int64(event.UsageMetadata.CachedContentTokenCount)
	u.totalTokens += int64(event.UsageMetadata.TotalTokenCount)
	if event.ModelVersion != "" {
		u.modelVersion = event.ModelVersion
	}
}

// seedFromTask primes the accumulator with the total already persisted on a
// resumed task, so tasks spanning multiple executions (HITL input-required
// cycles, follow-up messages) report a task-lifetime total instead of the last
// segment only.
func (u *turnUsage) seedFromTask(task *a2atype.Task) {
	if u == nil || task == nil || task.Metadata == nil {
		return
	}
	prior, ok := task.Metadata[usageTotalMetadataKey].(map[string]any)
	if !ok {
		return
	}
	u.promptTokens += metadataTokenCount(prior["promptTokenCount"])
	u.completionTokens += metadataTokenCount(prior["candidatesTokenCount"])
	u.thoughtsTokens += metadataTokenCount(prior["thoughtsTokenCount"])
	u.cachedContentTokens += metadataTokenCount(prior["cachedContentTokenCount"])
	u.totalTokens += metadataTokenCount(prior["totalTokenCount"])
	if modelVersion, ok := prior["modelVersion"].(string); ok && modelVersion != "" {
		u.modelVersion = modelVersion
	}
}

// metadataTokenCount reads a numeric token count from stored task metadata.
// Counts are float64 after a JSON round-trip but keep an integer type with
// in-memory task stores.
func metadataTokenCount(value any) int64 {
	switch n := value.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	default:
		return 0
	}
}

func (u *turnUsage) empty() bool {
	return u.promptTokens == 0 && u.completionTokens == 0 && u.totalTokens == 0
}

// stampEvent attaches the aggregate to a terminal status update under
// kagent_usage_total. The value is serialized exactly like the per-event
// adk_usage_metadata (same genai type, same JSON mapping) plus modelVersion, so
// consumers can share one parser.
func (u *turnUsage) stampEvent(event *a2atype.TaskStatusUpdateEvent) {
	if u == nil || event == nil || u.empty() {
		return
	}
	total, err := toJSONMap(&genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:        clampInt32(u.promptTokens),
		CandidatesTokenCount:    clampInt32(u.completionTokens),
		ThoughtsTokenCount:      clampInt32(u.thoughtsTokens),
		CachedContentTokenCount: clampInt32(u.cachedContentTokens),
		TotalTokenCount:         clampInt32(u.totalTokens),
	})
	if err != nil || total == nil {
		return
	}
	if u.modelVersion != "" {
		total["modelVersion"] = u.modelVersion
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	event.Metadata[usageTotalMetadataKey] = total
}

func clampInt32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v)
}
