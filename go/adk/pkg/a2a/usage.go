package a2a

import (
	"context"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	adksession "google.golang.org/adk/v2/session"
)

// UsageExtensionURI is the versioned A2A extension carrying task token usage.
const UsageExtensionURI = apia2a.UsageExtensionURI

const turnUsagePluginName = "kagent_turn_usage"

var usageAgentExtension = apia2a.UsageExtension()

// UsageActivationInterceptor activates the usage extension when the client
// requested it, so the transports echo it. Emission does not depend on it.
func UsageActivationInterceptor() a2asrv.CallInterceptor {
	return &usageActivationInterceptor{}
}

type usageActivationInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

func (*usageActivationInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	if callCtx != nil && callCtx.Extensions().Requested(&usageAgentExtension) {
		callCtx.Extensions().Activate(&usageAgentExtension)
	}
	return ctx, nil, nil
}

// turnUsage accumulates token usage across the ADK events of a task so the
// total can be emitted on the terminal status update. Partial (streaming chunk)
// events are skipped: each LLM call reports its usage on the final non-partial
// event, so summing partials would double-count.
type turnUsage struct {
	total  apia2a.TokenCounts
	models map[string]apia2a.TokenCounts
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

// callTokenCounts maps one LLM call to provider-neutral counts. A call that
// reports no total contributes a derived one, so a task mixing providers that
// report a total with providers that do not keeps a total consistent with its
// parts.
func callTokenCounts(event *adksession.Event) apia2a.TokenCounts {
	usage := event.UsageMetadata
	counts := apia2a.TokenCounts{
		InputTokens:       int64(usage.PromptTokenCount),
		OutputTokens:      int64(usage.CandidatesTokenCount),
		ReasoningTokens:   int64(usage.ThoughtsTokenCount),
		CachedInputTokens: int64(usage.CachedContentTokenCount),
		TotalTokens:       int64(usage.TotalTokenCount),
	}
	if counts.TotalTokens == 0 {
		counts.TotalTokens = counts.InputTokens + counts.OutputTokens + counts.ReasoningTokens
	}
	return counts
}

func (u *turnUsage) add(event *adksession.Event) {
	if u == nil || event == nil || event.Partial || event.UsageMetadata == nil {
		return
	}
	counts := callTokenCounts(event)
	u.total = u.total.Add(counts)
	if event.ModelVersion != "" {
		u.addModel(event.ModelVersion, counts)
	}
}

func (u *turnUsage) addModel(model string, counts apia2a.TokenCounts) {
	if u.models == nil {
		u.models = map[string]apia2a.TokenCounts{}
	}
	u.models[model] = u.models[model].Add(counts)
}

// seedFromTask primes the accumulator with the usage already persisted on a
// resumed task, so tasks spanning multiple executions (HITL input-required
// cycles, follow-up messages) report a task-lifetime total instead of the last
// segment only.
func (u *turnUsage) seedFromTask(task *a2atype.Task) {
	if u == nil || task == nil {
		return
	}
	prior, ok := apia2a.UsageFromMetadata(task.Metadata)
	if !ok {
		return
	}
	u.total = u.total.Add(prior.TokenCounts)
	for _, model := range prior.Models {
		if model.Model != "" {
			u.addModel(model.Model, model.TokenCounts)
		}
	}
}

func (u *turnUsage) empty() bool {
	return u.total.IsZero()
}

// snapshot returns the accumulated usage with models in a stable order.
func (u *turnUsage) snapshot() apia2a.Usage {
	usage := apia2a.Usage{TokenCounts: u.total}
	for model, counts := range u.models {
		usage.Models = append(usage.Models, apia2a.ModelUsage{Model: model, TokenCounts: counts})
	}
	slices.SortFunc(usage.Models, func(a, b apia2a.ModelUsage) int {
		return strings.Compare(a.Model, b.Model)
	})
	return usage
}

// stampEvent attaches the task usage to a terminal status update.
func (u *turnUsage) stampEvent(event *a2atype.TaskStatusUpdateEvent) {
	if u == nil || event == nil || u.empty() {
		return
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	// Encoding a struct of integers cannot fail.
	_ = apia2a.AttachUsage(event.Metadata, u.snapshot())
}
