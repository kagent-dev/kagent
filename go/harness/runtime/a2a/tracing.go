package a2a

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const runtimeScope = "github.com/kagent-dev/kagent/go/harness/runtime/a2a"

const (
	errorTypeTool       = "tool_error"
	errorTypeUnfinished = "unfinished"
	errorTypeModel      = "model_error"
)

// endedTools holds parent contexts for subagent spans arriving later in this segment.
type endedTools struct {
	// endedToolSpans holds the span contexts of written tools, by call ID.
	endedToolSpans map[string]trace.SpanContext
	// toolsAwaitingParent holds ended subagent tools, by call ID, until the span above them
	// is written. A nested subagent's spans can still go under them meanwhile.
	toolsAwaitingParent map[string]*pendingTool
}

func newEndedTools() *endedTools {
	return &endedTools{endedToolSpans: map[string]trace.SpanContext{}, toolsAwaitingParent: map[string]*pendingTool{}}
}

// nest writes a subagent span under an ended tool, or holds it on a tool awaiting its parent.
// It reports false when no ended tool has that ID.
func (e *endedTools) nest(toolID string, owner spanOwner, write func(under spanOwner)) bool {
	if toolID == "" {
		return false
	}
	if tool, ok := e.toolsAwaitingParent[toolID]; ok {
		tool.subagentSpans = append(tool.subagentSpans, write)
		return true
	}
	if parent, ok := e.endedToolSpans[toolID]; ok {
		owner.parent = trace.SpanFromContext(trace.ContextWithSpanContext(context.Background(), parent))
		write(owner)
		return true
	}
	return false
}

// spanOwner parents a segment's chat and tool spans.
type spanOwner struct {
	parent         trace.Span
	tracer         trace.Tracer
	conversationID string
	metrics        genAIMetrics
}

// pendingTool is a call whose execute_tool span is written once its outcome is known.
type pendingTool struct {
	id     string
	name   string
	server string
	start  time.Time
	owner  spanOwner
	// capturedArguments holds the arguments' span attributes. Only the ToolCall carries the
	// arguments, so they are captured at the start; empty when capture is off.
	capturedArguments []attribute.KeyValue
	parentToolID      string
	// subagentSpans write the model calls and tools of the subagent this tool runs. They wait
	// for the tool's span, because a span must exist before other spans can go under it.
	subagentSpans []func(under spanOwner)
}

func (s *executionSink) startToolSpan(event runtime.ToolCall) {
	if s.tools == nil {
		s.tools = map[string]pendingTool{}
	}
	parent := event.ParentToolID
	// A tool cannot go under itself. A reused ID can make it name itself as its parent,
	// and then its span would never be written.
	if parent == event.ID {
		parent = ""
	}
	// The plain MCP tool name groups a tool the same way in all runtimes.
	s.tools[event.ID] = pendingTool{
		id: event.ID, name: cmp.Or(event.MCPTool, event.Name), server: event.MCPServer, start: time.Now(), owner: s.owner,
		parentToolID:      parent,
		capturedArguments: capturedJSON(event.Arguments, s.captureLimit, conv.GenAIToolCallArgumentsKey, conv.KagentCaptureInputTruncatedKey),
	}
}

func (s *executionSink) endToolSpan(event runtime.ToolResult) {
	pending, ok := s.tools[event.ID]
	delete(s.tools, event.ID)
	// A denied or declined call never ran, so it gets no span.
	if !ok || event.Status == runtime.ToolDeclined {
		// It should hold no subagent spans, but held model calls still carry usage.
		for _, write := range pending.subagentSpans {
			if !s.nestUnderTool(pending.parentToolID, write) {
				write(s.owner)
			}
		}
		return
	}
	errorType := ""
	switch {
	case event.Status == runtime.ToolUnfinished:
		errorType = errorTypeUnfinished
	case event.IsError:
		errorType = errorTypeTool
	}
	var result []attribute.KeyValue
	// Only a call that succeeded has a result to report.
	if errorType == "" {
		result = capturedJSON(event.Result, s.captureLimit, conv.GenAIToolCallResultKey, conv.KagentCaptureOutputTruncatedKey)
	}
	end, ended := time.Now(), s.ended
	keep := func(under spanOwner) {
		delete(ended.toolsAwaitingParent, event.ID)
		pending.owner = under
		ended.endedToolSpans[event.ID] = pending.record(end, errorType, result...).SpanContext()
	}
	// The tool waits for its parent's span in toolsAwaitingParent, where nested spans find it.
	ended.toolsAwaitingParent[event.ID] = &pending
	if !s.nestUnderTool(pending.parentToolID, keep) {
		keep(pending.owner)
	}
}

// nestUnderTool writes a subagent span under the tool call that started it, or holds it until
// that tool's span exists. It reports false for an unknown tool; the caller then uses the segment.
func (s *executionSink) nestUnderTool(toolID string, write func(under spanOwner)) bool {
	if toolID == "" {
		return false
	}
	if tool, ok := s.tools[toolID]; ok {
		tool.subagentSpans = append(tool.subagentSpans, write)
		s.tools[toolID] = tool
		return true
	}
	return s.ended.nest(toolID, s.owner, write)
}

// capturedJSON encodes value under content capture, cut to limit bytes like the invoke_agent
// content. It returns nothing when capture is off or value is nil or cannot be encoded.
func capturedJSON(value any, limit int, key, truncatedKey attribute.Key) []attribute.KeyValue {
	if limit <= 0 || value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil || string(encoded) == "null" {
		return nil
	}
	text, truncated := tracing.BoundedText(string(encoded), limit)
	return []attribute.KeyValue{key.String(text), truncatedKey.Bool(truncated)}
}

func newSpanOwner(ctx context.Context, reqCtx *a2asrv.ExecutorContext, metrics genAIMetrics) spanOwner {
	owner := spanOwner{
		parent: trace.SpanFromContext(ctx), tracer: tracerOf(ctx), metrics: metrics,
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

func (tool pendingTool) record(end time.Time, errorType string, result ...attribute.KeyValue) trace.Span {
	span := tool.owner.start("execute_tool "+tool.name, trace.SpanKindInternal, tool.start,
		attribute.String(tracing.AttributeOperationName, conv.GenAIOperationNameExecuteTool),
		conv.GenAIToolNameKey.String(tool.name),
		conv.GenAIToolCallIDKey.String(tool.id),
	)
	span.SetAttributes(tool.capturedArguments...)
	span.SetAttributes(result...)
	if tool.server != "" {
		span.SetAttributes(conv.KagentMcpServerNameKey.String(tool.server))
	}
	if errorType != "" {
		span.SetAttributes(attribute.String(tracing.AttributeErrorType, errorType))
		span.SetStatus(codes.Error, errorType)
	}
	under := tool.owner
	under.parent = span
	for _, write := range tool.subagentSpans {
		write(under)
	}
	span.End(trace.WithTimestamp(end))
	tool.owner.metrics.recordTool(trace.ContextWithSpan(context.Background(), span), tool, end, errorType)
	return span
}

// endOpenTools records calls that never got a result.
func (s *executionSink) endOpenTools() {
	end := time.Now()
	unfinished := func(tool *pendingTool) func(under spanOwner) {
		return func(under spanOwner) {
			tool.owner = under
			tool.record(end, errorTypeUnfinished)
		}
	}
	open := make(map[string]*pendingTool, len(s.tools))
	for id, tool := range s.tools {
		open[id] = &tool
	}
	// A subagent's open tool goes under its open parent tool, at any depth.
	for _, id := range slices.Sorted(maps.Keys(open)) {
		if parent, ok := open[open[id].parentToolID]; ok {
			parent.subagentSpans = append(parent.subagentSpans, unfinished(open[id]))
		}
	}
	for _, id := range slices.Sorted(maps.Keys(open)) {
		tool := open[id]
		if _, nested := open[tool.parentToolID]; nested {
			continue
		}
		// Its parent may have ended already, such as a Codex spawn call.
		if write := unfinished(tool); !s.ended.nest(tool.parentToolID, tool.owner, write) {
			write(tool.owner)
		}
	}
	clear(s.tools)
}

func tracerOf(spanCtx context.Context) trace.Tracer {
	if span := trace.SpanFromContext(spanCtx); span.SpanContext().IsValid() {
		return span.TracerProvider().Tracer(runtimeScope, trace.WithSchemaURL(tracing.SchemaURL))
	}
	return tracing.Tracer(runtimeScope)
}

// ModelCall records a GenAI chat span under the invoke_agent span, or under the
// execute_tool span of the tool call that started its subagent.
func (s *executionSink) ModelCall(event runtime.ModelCall) {
	event.Provider = cmp.Or(event.Provider, s.compiled.Provider)
	event.RequestModel = cmp.Or(event.RequestModel, s.compiled.Model)
	if event.UsageOnly {
		s.owner.metrics.addTokens(context.Background(), event, inferenceAttributes(event))
		return
	}
	if event.ParentToolID == "" {
		s.inferenceCalls++
	}
	if s.nestUnderTool(event.ParentToolID, func(under spanOwner) { under.chat(event) }) {
		return
	}
	s.owner.chat(event)
}

func (o spanOwner) chat(event runtime.ModelCall) {
	attributes := []attribute.KeyValue{attribute.String(tracing.AttributeOperationName, conv.GenAIOperationNameChat)}
	// A partial call with no counts never reported usage, which differs from reporting zero.
	hasUsage := !event.UsagePartial || event.InputTokens != 0 || event.OutputTokens != 0
	if event.UsagePartial {
		attributes = append(attributes, conv.KagentInferenceUsagePartialKey.Bool(true))
	}
	if event.Retries != 0 {
		attributes = append(attributes, conv.KagentInferenceRetryCountKey.Int(event.Retries))
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
		{conv.GenAIResponseIDKey, event.ResponseID},
		{conv.KagentInferenceRequestIDKey, event.RequestID},
	} {
		if field.value != "" {
			attributes = append(attributes, field.key.String(field.value))
		}
	}
	failed := event.StopReason == tracing.FinishReasonError && !event.Canceled
	if event.StopReason != "" {
		attributes = append(attributes, conv.GenAIResponseFinishReasonsKey.StringSlice([]string{event.StopReason}))
	}
	errorType := ""
	if failed {
		errorType = cmp.Or(event.ErrorType, errorTypeModel)
		attributes = append(attributes, attribute.String(tracing.AttributeErrorType, errorType))
	}
	if event.CacheReadTokens != 0 {
		attributes = append(attributes, conv.GenAIUsageCacheReadInputTokensKey.Int64(event.CacheReadTokens))
	}
	if event.CacheWriteTokens != 0 {
		attributes = append(attributes, conv.GenAIUsageCacheWriteInputTokensKey.Int64(event.CacheWriteTokens))
	}
	if event.ReasoningOutputTokens != 0 {
		attributes = append(attributes, conv.GenAIUsageReasoningOutputTokensKey.Int64(event.ReasoningOutputTokens))
	}
	span := o.start(strings.TrimSpace("chat "+event.RequestModel), trace.SpanKindClient, event.Start, attributes...)
	if failed {
		span.SetStatus(codes.Error, errorType)
	}
	span.End(trace.WithTimestamp(event.End))
	o.metrics.recordInference(trace.ContextWithSpan(context.Background(), span), event, errorType)
}

func (s *executionSink) ToolExecutionStarted(event runtime.ToolCall)    { s.startToolSpan(event) }
func (s *executionSink) ToolExecutionFinished(event runtime.ToolResult) { s.endToolSpan(event) }
