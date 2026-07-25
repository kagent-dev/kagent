package a2a

import (
	adksession "google.golang.org/adk/v2/session"
)

// turnUsage accumulates token usage across the ADK events of one turn so the
// aggregated total can be emitted on the terminal status update. Partial
// (streaming chunk) events are skipped: each LLM call reports its usage on the
// final non-partial event, so summing partials would double-count.
type turnUsage struct {
	promptTokens     int64
	completionTokens int64
	totalTokens      int64
	model            string
}

func (u *turnUsage) add(event *adksession.Event) {
	if event == nil || event.Partial || event.UsageMetadata == nil {
		return
	}
	u.promptTokens += int64(event.UsageMetadata.PromptTokenCount)
	u.completionTokens += int64(event.UsageMetadata.CandidatesTokenCount)
	u.totalTokens += int64(event.UsageMetadata.TotalTokenCount)
	if event.ModelVersion != "" {
		u.model = event.ModelVersion
	}
}

func (u *turnUsage) empty() bool {
	return u.promptTokens == 0 && u.completionTokens == 0 && u.totalTokens == 0
}

// toMetadata returns the aggregate in the documented kagent_usage_total shape.
func (u *turnUsage) toMetadata() map[string]any {
	m := map[string]any{
		"prompt_tokens":     u.promptTokens,
		"completion_tokens": u.completionTokens,
		"total_tokens":      u.totalTokens,
	}
	if u.model != "" {
		m["model"] = u.model
	}
	return m
}
