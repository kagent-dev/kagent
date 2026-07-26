package a2a

import (
	"math"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// turnUsage accumulates token usage across the ADK events of one execution so
// the aggregated total can be emitted on terminal status updates. Partial
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

func (u *turnUsage) add(event *adksession.Event) {
	if event == nil || event.Partial || event.UsageMetadata == nil {
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
	if task == nil || task.Metadata == nil {
		return
	}
	prior, ok := task.Metadata[GetKAgentMetadataKey("usage_total")].(map[string]any)
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

// stamp attaches the aggregate to meta under kagent_usage_total. The value is
// serialized exactly like the per-event adk_usage_metadata (same genai type,
// same JSON mapping) plus modelVersion, so consumers can share one parser.
func (u *turnUsage) stamp(meta map[string]any) {
	if u.empty() {
		return
	}
	total, err := toA2AMetadataMap(&genai.GenerateContentResponseUsageMetadata{
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
	meta[GetKAgentMetadataKey("usage_total")] = total
}

func clampInt32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v)
}
