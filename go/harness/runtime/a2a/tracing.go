package a2a

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const runtimeScope = "github.com/kagent-dev/kagent/go/harness/runtime/a2a"

const (
	errorTypeTool       = "tool_error"
	errorTypeUnfinished = "unfinished"
	errorTypeModel      = "model_error"
)

// openTools are the calls still running when a segment pauses. The call awaiting
// approval has not run, so it is held apart and gets no span unless approved.
type openTools struct {
	owner spanOwner
	tools map[string]pendingTool
	gated string
	held  *pendingTool
}

// spanOwner parents a segment's chat and tool spans. It keeps the span, not the
// request context, so a parked turn does not retain the request.
type spanOwner struct {
	parent         trace.Span
	tracer         trace.Tracer
	invocation     *tracing.Invocation
	conversationID string
	began          time.Time
}

// pendingTool is a call whose execute_tool span is written once its outcome is known.
type pendingTool struct {
	id    string
	name  string
	start time.Time
	owner spanOwner
}

func (s *executionSink) startToolSpan(event runtime.ToolCall) {
	if event.ID == s.denied {
		return
	}
	if s.tools == nil {
		s.tools = map[string]pendingTool{}
	}
	s.tools[event.ID] = pendingTool{id: event.ID, name: event.Name, start: time.Now(), owner: s.owner}
}

func (s *executionSink) endToolSpan(event runtime.ToolResult) {
	pending, ok := s.tools[event.ID]
	delete(s.tools, event.ID)
	// A denied or declined call never ran, so it gets no span.
	if !ok || event.Status == runtime.ToolDeclined {
		return
	}
	errorType := ""
	switch {
	case event.Status == runtime.ToolUnfinished:
		errorType = errorTypeUnfinished
	case event.IsError:
		errorType = errorTypeTool
	}
	pending.record(time.Now(), errorType)
}

func newSpanOwner(ctx context.Context, reqCtx *a2asrv.ExecutorContext) spanOwner {
	owner := spanOwner{
		parent: trace.SpanFromContext(ctx), tracer: tracerOf(ctx), invocation: tracing.InvocationFromContext(ctx),
		began: time.Now(),
	}
	if reqCtx != nil {
		owner.conversationID = reqCtx.ContextID
	}
	return owner
}

func (o spanOwner) start(name string, kind trace.SpanKind, start time.Time, attributes ...attribute.KeyValue) trace.Span {
	if o.conversationID != "" {
		attributes = append(attributes, conv.GenAIConversationIDKey.String(o.conversationID))
	}
	_, span := o.tracer.Start(
		trace.ContextWithSpan(context.Background(), o.parent),
		name,
		trace.WithSpanKind(kind),
		trace.WithTimestamp(start),
		trace.WithAttributes(attributes...),
	)
	return span
}

func (tool pendingTool) record(end time.Time, errorType string) {
	span := tool.owner.start("execute_tool "+tool.name, trace.SpanKindInternal, tool.start,
		attribute.String(tracing.AttributeOperationName, conv.GenAIOperationNameExecuteTool),
		conv.GenAIToolNameKey.String(tool.name),
		conv.GenAIToolCallIDKey.String(tool.id),
	)
	if errorType != "" {
		span.SetAttributes(attribute.String(tracing.AttributeErrorType, errorType))
		span.SetStatus(codes.Error, errorType)
	}
	span.End(trace.WithTimestamp(end))
}

// endOpenTools records calls that never got a result.
func (s *executionSink) endOpenTools() {
	(openTools{tools: s.tools}).recordUnfinished()
	clear(s.tools)
}

// pauseTools moves the open calls out of the sink for a pause.
func (s *executionSink) pauseTools(request runtime.InputRequest) openTools {
	open := openTools{owner: s.owner, tools: s.tools}
	if approval, ok := request.(*runtime.ApprovalRequest); ok {
		open.gated = approval.CallID
		if tool, ok := open.tools[open.gated]; ok {
			open.held = &tool
			delete(open.tools, open.gated)
		}
	}
	s.tools = nil
	return open
}

// resume returns an approved held call to the open calls. It starts now under owner,
// so its span excludes the approval wait.
func (o openTools) resume(owner spanOwner, approved bool) {
	if approved && o.held != nil {
		tool := *o.held
		tool.start, tool.owner = time.Now(), owner
		o.tools[o.gated] = tool
	}
}

func (o openTools) recordUnfinished() {
	for _, id := range slices.Sorted(maps.Keys(o.tools)) {
		o.tools[id].record(time.Now(), errorTypeUnfinished)
	}
}

func tracerOf(spanCtx context.Context) trace.Tracer {
	if span := trace.SpanFromContext(spanCtx); span.SpanContext().IsValid() {
		return span.TracerProvider().Tracer(runtimeScope, trace.WithSchemaURL(tracing.SchemaURL))
	}
	return tracing.Tracer(runtimeScope)
}

// ModelCall records a GenAI chat span under the invoke_agent span.
func (s *executionSink) ModelCall(event runtime.ModelCall) error {
	event.Provider = cmp.Or(event.Provider, s.compiled.Provider)
	event.RequestModel = cmp.Or(event.RequestModel, s.compiled.Model)
	attributes := []attribute.KeyValue{attribute.String(tracing.AttributeOperationName, conv.GenAIOperationNameChat)}
	// A partial call with no counts never reported usage, which differs from reporting zero.
	hasUsage := !event.UsagePartial || event.InputTokens != 0 || event.OutputTokens != 0
	if event.UsagePartial {
		attributes = append(attributes, conv.KagentModelCallUsagePartialKey.Bool(true))
	}
	if hasUsage {
		attributes = append(attributes,
			conv.GenAIUsageInputTokensKey.Int64(event.InputTokens),
			conv.GenAIUsageOutputTokensKey.Int64(event.OutputTokens),
		)
	}
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
	failed := event.StopReason == tracing.FinishReasonError && !event.Canceled
	if event.StopReason != "" {
		attributes = append(attributes, conv.GenAIResponseFinishReasonsKey.StringSlice([]string{event.StopReason}))
	}
	if failed {
		attributes = append(attributes, attribute.String(tracing.AttributeErrorType, errorTypeModel))
	}
	if event.CacheReadTokens != 0 {
		attributes = append(attributes, conv.GenAIUsageCacheReadInputTokensKey.Int64(event.CacheReadTokens))
	}
	if event.CacheWriteTokens != 0 {
		attributes = append(attributes, conv.GenAIUsageCacheWriteInputTokensKey.Int64(event.CacheWriteTokens))
	}
	owner := s.owner
	// Usage can arrive after an approval pause; keep the call under the segment it ran in.
	if s.paused != nil && !event.Start.IsZero() && event.Start.Before(owner.began) {
		owner = *s.paused
	}
	span := owner.start(strings.TrimSpace("chat "+event.RequestModel), trace.SpanKindClient, event.Start, attributes...)
	if failed {
		span.SetStatus(codes.Error, errorTypeModel)
	}
	span.End(trace.WithTimestamp(event.End))
	return nil
}
