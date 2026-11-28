package a2a

import (
	"encoding/json"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/protobuf/encoding/protojson"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// UsageExtensionURI identifies the versioned kagent token usage A2A extension.
// Version 1 is emitted whether or not the client activated it.
const UsageExtensionURI = "https://kagent.dev/extensions/usage/v1"

// UsageExtension returns the optional Agent Card capability implemented by the
// kagent ADK runtimes.
func UsageExtension() a2atype.AgentExtension {
	return a2atype.AgentExtension{
		URI: UsageExtensionURI, Description: "Task-lifetime token usage", Required: false,
	}
}

// TokenCounts is a provider-neutral token tally. CachedInputTokens is a subset
// of InputTokens, and OutputTokens excludes ReasoningTokens. TotalTokens is the
// provider-reported total when there is one, so it can exceed the sum of the
// other counts.
type TokenCounts struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	ReasoningTokens   int64 `json:"reasoningTokens"`
	CachedInputTokens int64 `json:"cachedInputTokens"`
	TotalTokens       int64 `json:"totalTokens"`
}

// Add returns the element-wise sum of two tallies.
func (c TokenCounts) Add(other TokenCounts) TokenCounts {
	return TokenCounts{
		InputTokens:       c.InputTokens + other.InputTokens,
		OutputTokens:      c.OutputTokens + other.OutputTokens,
		ReasoningTokens:   c.ReasoningTokens + other.ReasoningTokens,
		CachedInputTokens: c.CachedInputTokens + other.CachedInputTokens,
		TotalTokens:       c.TotalTokens + other.TotalTokens,
	}
}

// IsZero reports whether no tokens were counted.
func (c TokenCounts) IsZero() bool {
	return c == TokenCounts{}
}

// ModelUsage is the share of a task's usage consumed by one model.
type ModelUsage struct {
	Model string `json:"model"`
	TokenCounts
}

// Usage is the payload stored under UsageExtensionURI in the metadata of a
// task's terminal status update. It covers the whole task lifetime, so a
// consumer replaces any earlier value with the latest one. Models lists the
// calls that named their model; calls that did not are counted in the totals
// only.
type Usage struct {
	TokenCounts
	Models []ModelUsage `json:"models,omitempty"`
}

// AttachUsage writes the payload into metadata under UsageExtensionURI.
func AttachUsage(metadata map[string]any, usage Usage) error {
	encoded, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return err
	}
	metadata[UsageExtensionURI] = raw
	return nil
}

// UsageFromMetadata decodes the payload stored under UsageExtensionURI. It
// reports false when the key is missing or does not decode as a Usage.
func UsageFromMetadata(metadata map[string]any) (Usage, bool) {
	raw, ok := metadata[UsageExtensionURI].(map[string]any)
	if !ok {
		return Usage{}, false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Usage{}, false
	}
	var usage Usage
	if err := json.Unmarshal(encoded, &usage); err != nil {
		return Usage{}, false
	}
	return usage, true
}

// SetModelCallUsage publishes the final root call in the same numeric form as ADK event usage.
func SetModelCallUsage(event *a2atype.TaskStatusUpdateEvent, usage *apiv1.ModelCallUsage) {
	if usage.GetTotalTokenCount() <= 0 {
		return
	}
	raw := map[string]any{
		"totalTokenCount":         usage.TotalTokenCount,
		"promptTokenCount":        usage.PromptTokenCount,
		"candidatesTokenCount":    usage.CandidatesTokenCount,
		"thoughtsTokenCount":      usage.ThoughtsTokenCount,
		"cachedContentTokenCount": usage.CachedContentTokenCount,
	}
	if usage.ContextWindowTokens > 0 {
		raw["contextWindowTokens"] = usage.ContextWindowTokens
	}
	event.SetMeta(UsageMetadataKey, raw)
}

// ModelCallUsageFromTask reads the latest reported per-call usage after A2A persistence.
func ModelCallUsageFromTask(task *a2atype.Task) *apiv1.ModelCallUsage {
	if task == nil {
		return nil
	}
	raw, ok := task.Metadata[UsageMetadataKey].(map[string]any)
	if !ok {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var usage apiv1.ModelCallUsage
	decoder := protojson.UnmarshalOptions{DiscardUnknown: true}
	if decoder.Unmarshal(data, &usage) != nil || usage.TotalTokenCount <= 0 {
		return nil
	}
	if min(usage.ContextWindowTokens, usage.PromptTokenCount, usage.CandidatesTokenCount, usage.CachedContentTokenCount, usage.ThoughtsTokenCount) < 0 {
		return nil
	}
	return &usage
}
