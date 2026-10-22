package a2a

import (
	"maps"
	"slices"
	"strings"

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

// add counts one model call as reported, in the totals and in the entry of the
// model it names. A sink without an accumulator, one that serves no task,
// counts nothing.
func (u *taskUsage) add(call runtime.ModelCall) {
	if u == nil {
		return
	}
	tokens := reportedTokens{input: call.InputTokens, output: call.OutputTokens, reasoning: call.ReasoningOutputTokens, cached: call.CacheReadTokens}
	if tokens == (reportedTokens{}) {
		return
	}
	u.total = u.total.add(tokens)
	model := call.ResponseModel
	if model == "" && call.UsageOnly && call.RequestModel != "" {
		model = u.familyEntry(call.RequestModel)
	}
	if model != "" {
		u.models[model] = u.models[model].add(tokens)
	}
}

// familyEntry names the entry a usage-only call counts in. Such a call, the
// tokens Claude Code's session totals hold beyond the calls it reported, names
// the model it was requested as: the totals key a model by its undated name,
// such as claude-sonnet-4-5, while a response names the dated model, such as
// claude-sonnet-4-5-20250929. The remainder belongs to that family as a whole.
// Its one entry takes the call, or a new entry under the undated name when the
// family has none. Several entries are folded into one under the undated name
// first, so the reasoning the call names is split out of the output that holds
// it instead of being counted beside it.
func (u *taskUsage) familyEntry(name string) string {
	var members []string
	for model := range u.models {
		if model == name || datedAs(model, name) {
			members = append(members, model)
		}
	}
	switch len(members) {
	case 0:
		return name
	case 1:
		return members[0]
	}
	var family reportedTokens
	for _, model := range members {
		family = family.add(u.models[model])
		delete(u.models, model)
	}
	u.models[name] = family
	return name
}

// datedAs reports whether model is name followed by a date, the way Anthropic
// names a model version: claude-sonnet-4-5-20250929 is claude-sonnet-4-5
// dated, and it is not claude-sonnet-4 dated.
func datedAs(model, name string) bool {
	date, ok := strings.CutPrefix(model, name+"-")
	if !ok || len(date) != 8 {
		return false
	}
	for _, digit := range []byte(date) {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
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
