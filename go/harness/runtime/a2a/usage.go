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
	total  reportedTokens
	models map[string]reportedTokens
}

// reportedTokens tallies a runtime's counters as it reports them: output holds
// the reasoning tokens, as Anthropic and OpenAI count them. The split into the
// extension's outputTokens and reasoningTokens happens once, when the tally is
// stamped. A runtime can name reasoning tokens after the output that holds them
// was counted: Claude Code's session totals report what a turn's calls left
// out per counter, so such a remainder can carry reasoning tokens and no output
// tokens. Subtracting per call would count those tokens twice.
type reportedTokens struct {
	input, output, reasoning, cached int64
}

func (r reportedTokens) add(other reportedTokens) reportedTokens {
	return reportedTokens{r.input + other.input, r.output + other.output, r.reasoning + other.reasoning, r.cached + other.cached}
}

// counts splits the reasoning tokens out of the output. The total follows the
// extension's formula for calls that report no provider total: input, output
// and reasoning, with the output excluding the reasoning.
func (r reportedTokens) counts() apia2a.TokenCounts {
	return apia2a.TokenCounts{
		InputTokens:       r.input,
		OutputTokens:      max(r.output-r.reasoning, 0),
		ReasoningTokens:   r.reasoning,
		CachedInputTokens: r.cached,
		TotalTokens:       r.input + max(r.output, r.reasoning),
	}
}

// reported reads a persisted tally back into its reported form, so a task that
// adds no call stamps the counts it loaded.
func reported(counts apia2a.TokenCounts) reportedTokens {
	return reportedTokens{input: counts.InputTokens, output: counts.OutputTokens + counts.ReasoningTokens, reasoning: counts.ReasoningTokens, cached: counts.CachedInputTokens}
}

func newTaskUsage(stored *a2atype.Task) *taskUsage {
	usage := &taskUsage{models: map[string]reportedTokens{}}
	if stored == nil {
		return usage
	}
	prior, ok := apia2a.UsageFromMetadata(stored.Metadata)
	if !ok {
		return usage
	}
	usage.total = reported(prior.TokenCounts)
	for _, model := range prior.Models {
		if model.Model != "" {
			usage.models[model.Model] = usage.models[model.Model].add(reported(model.TokenCounts))
		}
	}
	return usage
}

// add counts one model call as reported. Usage-only calls, such as the tokens
// Claude Code's session totals hold beyond its reported calls, name no
// response model, so they count in the totals only. A sink without an
// accumulator, one that serves no task, counts nothing.
func (u *taskUsage) add(call runtime.ModelCall) {
	if u == nil {
		return
	}
	tokens := reportedTokens{input: call.InputTokens, output: call.OutputTokens, reasoning: call.ReasoningOutputTokens, cached: call.CacheReadTokens}
	if tokens == (reportedTokens{}) {
		return
	}
	u.total = u.total.add(tokens)
	if call.ResponseModel != "" {
		u.models[call.ResponseModel] = u.models[call.ResponseModel].add(tokens)
	}
}

// snapshot copies the tally for the parked task, so the calls a cancel reaps
// into the copy and the stamp of the execution's input-required status never
// touch one tally at the same time.
func (u *taskUsage) snapshot() *taskUsage {
	if u == nil {
		return nil
	}
	return &taskUsage{total: u.total, models: maps.Clone(u.models)}
}

// stamp attaches the task's usage to its terminal status update. The key is
// left out while no call reported usage.
func (u *taskUsage) stamp(event *a2atype.TaskStatusUpdateEvent) *a2atype.TaskStatusUpdateEvent {
	if u.total == (reportedTokens{}) {
		return event
	}
	usage := apia2a.Usage{TokenCounts: u.total.counts()}
	for _, model := range slices.Sorted(maps.Keys(u.models)) {
		usage.Models = append(usage.Models, apia2a.ModelUsage{Model: model, TokenCounts: u.models[model].counts()})
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	// Encoding a struct of integers cannot fail.
	_ = apia2a.AttachUsage(event.Metadata, usage)
	return event
}
