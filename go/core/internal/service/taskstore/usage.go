package taskstore

import (
	"context"
	"log/slog"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
)

// UsageRecorder charges the tokens of each published boundary to the sender
// of the execution that reached it.
type UsageRecorder struct {
	input, output, reasoning, cacheRead metric.Int64Counter
}

// NewUsageRecorder creates the kagent.turn.usage.* counters on meter.
func NewUsageRecorder(meter metric.Meter) (*UsageRecorder, error) {
	recorder := &UsageRecorder{}
	for _, counter := range []struct {
		target                  *metric.Int64Counter
		name, unit, description string
	}{
		{&recorder.input, conv.KagentTurnUsageInputTokens, conv.KagentTurnUsageInputTokensUnit, conv.KagentTurnUsageInputTokensDescription},
		{&recorder.output, conv.KagentTurnUsageOutputTokens, conv.KagentTurnUsageOutputTokensUnit, conv.KagentTurnUsageOutputTokensDescription},
		{&recorder.reasoning, conv.KagentTurnUsageReasoningOutputTokens, conv.KagentTurnUsageReasoningOutputTokensUnit, conv.KagentTurnUsageReasoningOutputTokensDescription},
		{&recorder.cacheRead, conv.KagentTurnUsageCacheReadInputTokens, conv.KagentTurnUsageCacheReadInputTokensUnit, conv.KagentTurnUsageCacheReadInputTokensDescription},
	} {
		instrument, err := meter.Int64Counter(counter.name, metric.WithUnit(counter.unit), metric.WithDescription(counter.description))
		if err != nil {
			return nil, err
		}
		*counter.target = instrument
	}
	return recorder, nil
}

// record counts what the boundary added to the task's usage. The usage/v1
// payload covers the task's lifetime, so the total the task carried before the
// boundary is subtracted. It logs one event per boundary naming the sender. A
// boundary whose sender was not recorded is counted without a sender and logged
// as a warning, so the totals stay complete.
func (r *UsageRecorder) record(ctx context.Context, session *apiv1.Session, settled *database.SettledTask) {
	if r == nil || settled == nil {
		return
	}
	after, reported := apia2a.UsageFromMetadata(settled.Task.Metadata)
	before, _ := apia2a.UsageFromMetadata(settled.Previous.Metadata)
	usage := subtractUsage(after, before)
	agent := session.GetAgent().GetNamespace() + "/" + session.GetAgent().GetName()
	sender := settled.Sender
	if sender == nil {
		sender = &database.TurnSender{}
	}
	level := slog.LevelInfo
	if sender.Kind == "" {
		level = slog.LevelWarn
	}
	logging.FromContext(ctx).Log(ctx, level, "turn usage",
		"agent", agent, "session_id", session.GetId(), "task_id", settled.Task.ID, "state", settled.Task.Status.State,
		"sender", sender.Kind, "principal_id", sender.PrincipalID, "caller_session_id", sender.CallerSessionID,
		"usage_reported", reported,
		"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens,
		"reasoning_tokens", usage.ReasoningTokens, "cached_input_tokens", usage.CachedInputTokens,
		"total_tokens", usage.TotalTokens)
	base := []attribute.KeyValue{conv.GenAIAgentIDKey.String(agent)}
	if sender.Kind != "" {
		base = append(base, conv.KagentTurnSenderKey.String(sender.Kind))
	}
	remainder := usage.TokenCounts
	for _, model := range usage.Models {
		r.add(ctx, model.TokenCounts, append(slices.Clone(base), conv.GenAIResponseModelKey.String(model.Model)))
		remainder = subtractCounts(remainder, model.TokenCounts)
	}
	r.add(ctx, remainder, base)
}

func (r *UsageRecorder) add(ctx context.Context, counts apia2a.TokenCounts, attributes []attribute.KeyValue) {
	if counts.IsZero() {
		return
	}
	set := metric.WithAttributeSet(attribute.NewSet(attributes...))
	r.input.Add(ctx, counts.InputTokens, set)
	r.output.Add(ctx, counts.OutputTokens, set)
	r.reasoning.Add(ctx, counts.ReasoningTokens, set)
	r.cacheRead.Add(ctx, counts.CachedInputTokens, set)
}

// subtractUsage returns what after added to before, per count and per model.
// A count that went down (a runtime that lost its persisted total) is zero.
func subtractUsage(after, before apia2a.Usage) apia2a.Usage {
	prior := map[string]apia2a.TokenCounts{}
	for _, model := range before.Models {
		prior[model.Model] = model.TokenCounts
	}
	result := apia2a.Usage{TokenCounts: subtractCounts(after.TokenCounts, before.TokenCounts)}
	for _, model := range after.Models {
		counts := subtractCounts(model.TokenCounts, prior[model.Model])
		if !counts.IsZero() {
			result.Models = append(result.Models, apia2a.ModelUsage{Model: model.Model, TokenCounts: counts})
		}
	}
	return result
}

func subtractCounts(after, before apia2a.TokenCounts) apia2a.TokenCounts {
	return apia2a.TokenCounts{
		InputTokens:       max(after.InputTokens-before.InputTokens, 0),
		OutputTokens:      max(after.OutputTokens-before.OutputTokens, 0),
		ReasoningTokens:   max(after.ReasoningTokens-before.ReasoningTokens, 0),
		CachedInputTokens: max(after.CachedInputTokens-before.CachedInputTokens, 0),
		TotalTokens:       max(after.TotalTokens-before.TotalTokens, 0),
	}
}
