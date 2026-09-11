package a2a

import (
	"context"
	"math"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// usageTotalMetadataKey is the A2A metadata key carrying the aggregated
// token usage of a task. The value has the same shape as the per-event
// adk_usage_metadata entry, plus modelVersion. Every terminal status update of
// a task carries the running task-lifetime total, so consumers take the latest
// value rather than summing across executions.
var usageTotalMetadataKey = GetKAgentMetadataKey("usage_total")

const turnUsagePluginName = "kagent_turn_usage"

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
// its ExecutorContext from this context, so the runner plugin and the
// post-execution callback reach the accumulator of the execution they belong
// to.
func withTurnUsage(ctx context.Context, usage *turnUsage) context.Context {
	return context.WithValue(ctx, turnUsageContextKey{}, usage)
}

func turnUsageFrom(ctx context.Context) *turnUsage {
	usage, _ := ctx.Value(turnUsageContextKey{}).(*turnUsage)
	return usage
}

// newTurnUsagePlugin counts every ADK event the runner produces. Events are
// counted before A2A conversion, which drops the ones it cannot turn into an
// artifact (a paused tool call, for instance) even though they report token
// usage.
func newTurnUsagePlugin() (*plugin.Plugin, error) {
	return plugin.New(plugin.Config{
		Name: turnUsagePluginName,
		OnEventCallback: func(ictx adkagent.InvocationContext, event *adksession.Event) (*adksession.Event, error) {
			turnUsageFrom(ictx).add(event)
			return nil, nil
		},
	})
}

// add sums one LLM call into the accumulator. A call that reports no total
// contributes a derived one, so a task mixing providers that report a total
// with providers that do not keeps a total consistent with its parts.
func (u *turnUsage) add(event *adksession.Event) {
	if u == nil || event == nil || event.Partial || event.UsageMetadata == nil {
		return
	}
	prompt := int64(event.UsageMetadata.PromptTokenCount)
	completion := int64(event.UsageMetadata.CandidatesTokenCount)
	thoughts := int64(event.UsageMetadata.ThoughtsTokenCount)
	total := int64(event.UsageMetadata.TotalTokenCount)
	if total == 0 {
		total = prompt + completion + thoughts
	}
	u.promptTokens += prompt
	u.completionTokens += completion
	u.thoughtsTokens += thoughts
	u.cachedContentTokens += int64(event.UsageMetadata.CachedContentTokenCount)
	u.totalTokens += total
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
