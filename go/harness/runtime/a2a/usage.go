package a2a

import (
	"maps"
	"slices"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// taskUsage sums the model calls a runtime reports into the task-lifetime
// payload of the usage extension. It starts from the total persisted on the
// stored task, so a resumed task keeps counting where its last execution ended.
type taskUsage struct {
	total  apia2a.TokenCounts
	models map[string]apia2a.TokenCounts
}

func newTaskUsage(stored *a2atype.Task) *taskUsage {
	usage := &taskUsage{models: map[string]apia2a.TokenCounts{}}
	if stored == nil {
		return usage
	}
	prior, ok := apia2a.UsageFromMetadata(stored.Metadata)
	if !ok {
		return usage
	}
	usage.total = prior.TokenCounts
	for _, model := range prior.Models {
		if model.Model != "" {
			usage.models[model.Model] = usage.models[model.Model].Add(model.TokenCounts)
		}
	}
	return usage
}

// add counts one model call. A runtime's OutputTokens includes its reasoning
// tokens, as Anthropic and OpenAI report them, while the extension's
// outputTokens leaves them out. Usage-only calls, such as the tokens Claude
// Code's session totals hold beyond its reported calls, name no response
// model, so they count in the totals only. A sink without an accumulator, one
// that serves no task, counts nothing.
func (u *taskUsage) add(call runtime.ModelCall) {
	if u == nil {
		return
	}
	counts := apia2a.TokenCounts{
		InputTokens:       call.InputTokens,
		OutputTokens:      max(call.OutputTokens-call.ReasoningOutputTokens, 0),
		ReasoningTokens:   call.ReasoningOutputTokens,
		CachedInputTokens: call.CacheReadTokens,
	}
	counts.TotalTokens = counts.InputTokens + counts.OutputTokens + counts.ReasoningTokens
	if counts.IsZero() {
		return
	}
	u.total = u.total.Add(counts)
	if call.ResponseModel != "" {
		u.models[call.ResponseModel] = u.models[call.ResponseModel].Add(counts)
	}
}

// stamp attaches the task's usage to its terminal status update. The key is
// left out while no call reported usage.
func (u *taskUsage) stamp(event *a2atype.TaskStatusUpdateEvent) *a2atype.TaskStatusUpdateEvent {
	if u.total.IsZero() {
		return event
	}
	usage := apia2a.Usage{TokenCounts: u.total}
	for _, model := range slices.Sorted(maps.Keys(u.models)) {
		usage.Models = append(usage.Models, apia2a.ModelUsage{Model: model, TokenCounts: u.models[model]})
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	// Encoding a struct of integers cannot fail.
	_ = apia2a.AttachUsage(event.Metadata, usage)
	return event
}
