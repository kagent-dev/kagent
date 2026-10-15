package a2a

import (
	"context"
	"errors"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// Bucket boundaries the GenAI conventions advise, in seconds, tokens and calls.
var (
	durationBuckets = []float64{0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92}
	tokenBuckets    = []float64{1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864}
	callBuckets     = []float64{1, 2, 4, 8, 16, 32, 64, 128}
)

// genAIMetrics are the GenAI metrics of a harness turn, recorded with the spans kagent writes.
type genAIMetrics struct {
	// agent is empty when the runtime has no agent name, so the label is left out.
	agent                                           []attribute.KeyValue
	inferenceDuration, toolDuration                 metric.Float64Histogram
	inputPerCall, outputPerCall                     metric.Int64Histogram
	input, output, cacheRead, cacheWrite, reasoning metric.Int64Counter
	inferenceCalls, toolCalls                       metric.Int64Histogram
}

// newGenAIMetrics uses a no-op instrument for any it cannot create, and reports why.
func newGenAIMetrics(meter metric.Meter, agentName string) (genAIMetrics, error) {
	var errs []error
	duration := func(name, unit, description string) metric.Float64Histogram {
		instrument, err := meter.Float64Histogram(name,
			metric.WithUnit(unit), metric.WithDescription(description), metric.WithExplicitBucketBoundaries(durationBuckets...))
		if err != nil {
			errs = append(errs, err)
			return noop.Float64Histogram{}
		}
		return instrument
	}
	histogram := func(name, unit, description string, buckets []float64) metric.Int64Histogram {
		instrument, err := meter.Int64Histogram(name,
			metric.WithUnit(unit), metric.WithDescription(description), metric.WithExplicitBucketBoundaries(buckets...))
		if err != nil {
			errs = append(errs, err)
			return noop.Int64Histogram{}
		}
		return instrument
	}
	counter := func(name, unit, description string) metric.Int64Counter {
		instrument, err := meter.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(description))
		if err != nil {
			errs = append(errs, err)
			return noop.Int64Counter{}
		}
		return instrument
	}
	metrics := genAIMetrics{
		inferenceDuration: duration(
			conv.GenAIClientInferenceDuration,
			conv.GenAIClientInferenceDurationUnit,
			conv.GenAIClientInferenceDurationDescription,
		),
		toolDuration: duration(
			conv.GenAIExecuteToolDuration,
			conv.GenAIExecuteToolDurationUnit,
			conv.GenAIExecuteToolDurationDescription,
		),
		inputPerCall: histogram(
			conv.GenAIClientInferenceOperationInputTokens,
			conv.GenAIClientInferenceOperationInputTokensUnit,
			conv.GenAIClientInferenceOperationInputTokensDescription,
			tokenBuckets,
		),
		outputPerCall: histogram(
			conv.GenAIClientInferenceOperationOutputTokens,
			conv.GenAIClientInferenceOperationOutputTokensUnit,
			conv.GenAIClientInferenceOperationOutputTokensDescription,
			tokenBuckets,
		),
		input: counter(
			conv.GenAIClientInferenceUsageInputTokens,
			conv.GenAIClientInferenceUsageInputTokensUnit,
			conv.GenAIClientInferenceUsageInputTokensDescription,
		),
		output: counter(
			conv.GenAIClientInferenceUsageOutputTokens,
			conv.GenAIClientInferenceUsageOutputTokensUnit,
			conv.GenAIClientInferenceUsageOutputTokensDescription,
		),
		cacheRead: counter(
			conv.GenAIClientInferenceUsageCacheReadInputTokens,
			conv.GenAIClientInferenceUsageCacheReadInputTokensUnit,
			conv.GenAIClientInferenceUsageCacheReadInputTokensDescription,
		),
		cacheWrite: counter(
			conv.GenAIClientInferenceUsageCacheWriteInputTokens,
			conv.GenAIClientInferenceUsageCacheWriteInputTokensUnit,
			conv.GenAIClientInferenceUsageCacheWriteInputTokensDescription,
		),
		reasoning: counter(
			conv.GenAIClientInferenceUsageReasoningOutputTokens,
			conv.GenAIClientInferenceUsageReasoningOutputTokensUnit,
			conv.GenAIClientInferenceUsageReasoningOutputTokensDescription,
		),
		inferenceCalls: histogram(
			conv.GenAIInvokeAgentInferenceCalls,
			conv.GenAIInvokeAgentInferenceCallsUnit,
			conv.GenAIInvokeAgentInferenceCallsDescription,
			callBuckets,
		),
		toolCalls: histogram(
			conv.GenAIInvokeAgentToolCalls,
			conv.GenAIInvokeAgentToolCallsUnit,
			conv.GenAIInvokeAgentToolCallsDescription,
			callBuckets,
		),
	}
	if agentName != "" {
		metrics.agent = []attribute.KeyValue{attribute.String(tracing.AttributeAgentName, agentName)}
	}
	return metrics, errors.Join(errs...)
}

// recordInference adds a model call to the metrics. errorType is empty unless the call failed.
// ctx carries the chat span, so a metric can link to it.
func (m genAIMetrics) recordInference(ctx context.Context, event runtime.ModelCall, errorType string) {
	attributes := inferenceAttributes(event)
	// A Codex call from a thread not known yet has no duration, so it adds only its tokens.
	if event.End.After(event.Start) {
		m.inferenceDuration.Record(ctx, event.End.Sub(event.Start).Seconds(), withError(attributes, errorType))
	}
	// A partial call's input count is exact, but its output count is only a lower bound.
	perCall := metric.WithAttributes(attributes...)
	if !event.UsagePartial || event.InputTokens > 0 {
		m.inputPerCall.Record(ctx, event.InputTokens, perCall)
	}
	if !event.UsagePartial {
		m.outputPerCall.Record(ctx, event.OutputTokens, perCall)
	}
	m.addTokens(ctx, event, attributes)
}

func inferenceAttributes(event runtime.ModelCall) []attribute.KeyValue {
	attributes := []attribute.KeyValue{attribute.String(tracing.AttributeOperationName, conv.GenAIOperationNameChat)}
	for _, field := range []struct {
		key   attribute.Key
		value string
	}{
		{conv.GenAIProviderNameKey, event.Provider},
		{conv.GenAIRequestModelKey, event.RequestModel},
		{conv.GenAIResponseModelKey, event.ResponseModel},
	} {
		if field.value != "" {
			attributes = append(attributes, field.key.String(field.value))
		}
	}
	return attributes
}

func (m genAIMetrics) addTokens(ctx context.Context, event runtime.ModelCall, attributes []attribute.KeyValue) {
	modality := conv.GenAITokenModalityKey.String(conv.GenAITokenModalityUnknown)
	usage := metric.WithAttributes(append(slices.Clip(attributes), modality)...)
	for _, u := range []struct {
		counter metric.Int64Counter
		count   int64
	}{
		{m.input, event.InputTokens},
		{m.output, event.OutputTokens},
		{m.cacheRead, event.CacheReadTokens},
		{m.cacheWrite, event.CacheWriteTokens},
		{m.reasoning, event.ReasoningOutputTokens},
	} {
		if u.count > 0 {
			u.counter.Add(ctx, u.count, usage)
		}
	}
}

// recordTool adds a tool call to the metrics. ctx carries the execute_tool span.
func (m genAIMetrics) recordTool(ctx context.Context, tool pendingTool, end time.Time, errorType string) {
	attributes := []attribute.KeyValue{conv.GenAIToolNameKey.String(tool.name)}
	// A subagent's tool is not the agent's own, and the subagent has no name to record.
	if tool.parentToolID == "" {
		attributes = append(attributes, m.agent...)
	}
	m.toolDuration.Record(ctx, end.Sub(tool.start).Seconds(), withError(attributes, errorType))
}

// recordInvocation adds the agent's own model and tool calls of one segment.
// ctx carries the invoke_agent span.
func (m genAIMetrics) recordInvocation(ctx context.Context, inferenceCalls, toolCalls int64) {
	agent := metric.WithAttributes(m.agent...)
	m.inferenceCalls.Record(ctx, inferenceCalls, agent)
	m.toolCalls.Record(ctx, toolCalls, agent)
}

func withError(attributes []attribute.KeyValue, errorType string) metric.MeasurementOption {
	if errorType == "" {
		return metric.WithAttributes(attributes...)
	}
	return metric.WithAttributes(append(slices.Clip(attributes), attribute.String(tracing.AttributeErrorType, errorType))...)
}
