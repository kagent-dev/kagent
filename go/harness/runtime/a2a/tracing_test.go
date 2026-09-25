package a2a

import (
	"context"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// traceRecorder starts one invocation per request, the way the A2A transport
// interceptor does, and collects what each segment exported.
type traceRecorder struct {
	exporter *tracetest.InMemoryExporter
	tracer   trace.Tracer
}

func newTraceRecorder(t *testing.T) *traceRecorder {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return &traceRecorder{exporter: exporter, tracer: provider.Tracer("test")}
}

func (r *traceRecorder) segment(ctx context.Context) context.Context {
	ctx, _ = tracing.StartInvocation(ctx, r.tracer, "invoke_agent", nil)
	return ctx
}

func (r *traceRecorder) spans(t *testing.T, want int) tracetest.SpanStubs {
	t.Helper()
	spans := r.exporter.GetSpans()
	if len(spans) != want {
		t.Fatalf("exported %d invocation spans, want %d", len(spans), want)
	}
	return spans
}

func attributeOf(span tracetest.SpanStub, key string) (attribute.Value, bool) {
	for _, attr := range span.Attributes {
		if string(attr.Key) == key {
			return attr.Value, true
		}
	}
	return attribute.Value{}, false
}

func stringAttribute(t *testing.T, span tracetest.SpanStub, key string) string {
	t.Helper()
	value, ok := attributeOf(span, key)
	if !ok {
		t.Fatalf("span is missing attribute %s", key)
	}
	return value.AsString()
}

func captureTelemetry(limit int) tracing.RuntimeTelemetry {
	return tracing.RuntimeTelemetry{
		Runtime: tracing.RuntimeClaude, AgentName: "reporter-claude",
		AgentNamespace: "kagent", CaptureContent: true, MaxCaptureBytes: limit,
	}
}

func TestInvocationRecordsResolvedRequestIdentity(t *testing.T) {
	recorder := newTraceRecorder(t)
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	_, errs := collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-identity", "hello")))
	if len(errs) != 0 {
		t.Fatalf("execution errors = %v", errs)
	}

	span := recorder.spans(t, 1)[0]
	for key, want := range map[string]string{
		tracing.AttributeConversationID: testContextID,
		tracing.AttributeTaskID:         "task-identity",
		tracing.AttributeSegment:        tracing.SegmentInitial,
		tracing.AttributeTaskState:      string(a2atype.TaskStateCompleted),
	} {
		if got := stringAttribute(t, span, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if _, ok := attributeOf(span, tracing.AttributeInputMessages); ok {
		t.Error("capture is disabled by default, so no input attribute should be recorded")
	}
	if _, ok := attributeOf(span, tracing.AttributeOutputMessages); ok {
		t.Error("capture is disabled by default, so no output attribute should be recorded")
	}
}

func TestInvocationRecordsRuntimeFailure(t *testing.T) {
	recorder := newTraceRecorder(t)
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{Failure: &runtime.Failure{Message: "provider rejected the api key sk-secret"}}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-failure", "hello")))

	span := recorder.spans(t, 1)[0]
	if span.Status.Code != codes.Error {
		t.Errorf("status = %v, want an error status", span.Status.Code)
	}
	if got := stringAttribute(t, span, tracing.AttributeErrorType); got != "runtime_failure" {
		t.Errorf("%s = %q, want a safe category", tracing.AttributeErrorType, got)
	}
	if strings.Contains(span.Status.Description, "sk-secret") {
		t.Errorf("status description leaked the provider message: %q", span.Status.Description)
	}
	if got := stringAttribute(t, span, tracing.AttributeTaskState); got != string(a2atype.TaskStateFailed) {
		t.Errorf("%s = %q, want %q", tracing.AttributeTaskState, got, a2atype.TaskStateFailed)
	}
}

func TestInvocationRecordsCancellation(t *testing.T) {
	recorder := newTraceRecorder(t)
	started := make(chan struct{})
	executor, err := New(fakeRunner{run: func(ctx context.Context, _ runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
		close(started)
		<-ctx.Done()
		return runtime.Outcome{}, ctx.Err()
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		collect(executor.Execute(recorder.segment(context.Background()), requestContext("task-cancel", "hello")))
	}()
	<-started
	collect(executor.Cancel(t.Context(), requestContext("task-cancel", "ignored")))
	<-done

	span := recorder.spans(t, 1)[0]
	if got := stringAttribute(t, span, tracing.AttributeDisposition); got != tracing.DispositionCanceled {
		t.Errorf("%s = %q, want %q", tracing.AttributeDisposition, got, tracing.DispositionCanceled)
	}
	if got := stringAttribute(t, span, tracing.AttributeTaskState); got != string(a2atype.TaskStateCanceled) {
		t.Errorf("%s = %q, want %q", tracing.AttributeTaskState, got, a2atype.TaskStateCanceled)
	}
	if span.Status.Code == codes.Error {
		t.Error("cancellation was reported as an error")
	}
}

// A consumer that stops reading is not a cancellation the client requested.
func TestInvocationRecordsAbandonedStream(t *testing.T) {
	recorder := newTraceRecorder(t)
	executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		if err := sink.TextDelta(runtime.TextDelta{Text: "partial"}); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Outcome{}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	for range executor.Execute(recorder.segment(t.Context()), requestContext("task-abandon", "hello")) {
		break
	}

	span := recorder.spans(t, 1)[0]
	if got := stringAttribute(t, span, tracing.AttributeDisposition); got != tracing.DispositionAbandoned {
		t.Errorf("%s = %q, want %q", tracing.AttributeDisposition, got, tracing.DispositionAbandoned)
	}
	if _, ok := attributeOf(span, tracing.AttributeTaskState); ok {
		t.Error("an abandoned stream reported a task state it never published")
	}
}

func TestCaptureRecordsBoundedTurnContent(t *testing.T) {
	recorder := newTraceRecorder(t)
	// Each character is three bytes, so a ten-byte budget cannot hold four of
	// them and must not split the fourth either.
	executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		for range 4 {
			if err := sink.TextDelta(runtime.TextDelta{Text: "日"}); err != nil {
				return runtime.Outcome{}, err
			}
		}
		return runtime.Outcome{}, nil
	}}, &fakeContinuation{}, captureTelemetry(10))
	if err != nil {
		t.Fatal(err)
	}

	collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-capture", strings.Repeat("p", 32))))

	span := recorder.spans(t, 1)[0]
	if got, want := stringAttribute(t, span, tracing.AttributeInputMessages), tracing.InputMessages(strings.Repeat("p", 10)); got != want {
		t.Errorf("%s = %s, want the first ten bytes of the prompt as %s", tracing.AttributeInputMessages, got, want)
	}
	if !boolAttribute(t, span, tracing.AttributeInputTruncated) {
		t.Error("an oversized prompt did not report truncation")
	}
	if got, want := stringAttribute(t, span, tracing.AttributeOutputMessages), tracing.OutputMessages("日日日", tracing.FinishReasonStop); got != want {
		t.Errorf("%s = %s, want three whole characters as %s", tracing.AttributeOutputMessages, got, want)
	}
	if !boolAttribute(t, span, tracing.AttributeOutputTruncated) {
		t.Error("an oversized response did not report truncation")
	}
}

// Capture that is enabled but produced nothing is distinguishable from capture
// that is off, which records no attributes at all.
func TestCaptureRecordsEmptyOutput(t *testing.T) {
	recorder := newTraceRecorder(t)
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{}, nil
	}}, &fakeContinuation{}, captureTelemetry(0))
	if err != nil {
		t.Fatal(err)
	}

	collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-empty", "hello")))

	span := recorder.spans(t, 1)[0]
	if got, want := stringAttribute(t, span, tracing.AttributeOutputMessages), tracing.OutputMessages("", tracing.FinishReasonStop); got != want {
		t.Errorf("%s = %s, want an empty message %s", tracing.AttributeOutputMessages, got, want)
	}
	if boolAttribute(t, span, tracing.AttributeOutputTruncated) {
		t.Error("an empty capture reported truncation")
	}
	if got, want := stringAttribute(t, span, tracing.AttributeInputMessages), tracing.InputMessages("hello"); got != want {
		t.Errorf("%s = %s, want the prompt as %s", tracing.AttributeInputMessages, got, want)
	}
}

// A failed turn still exports what it produced before the failure, and still
// keeps the failure category out of the captured content.
func TestCaptureRecordsOutputOfAFailedTurn(t *testing.T) {
	recorder := newTraceRecorder(t)
	executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		if err := sink.TextDelta(runtime.TextDelta{Text: "partial answer"}); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Outcome{Failure: &runtime.Failure{Message: "provider rejected the request"}}, nil
	}}, &fakeContinuation{}, captureTelemetry(0))
	if err != nil {
		t.Fatal(err)
	}

	collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-capture-failure", "hello")))

	span := recorder.spans(t, 1)[0]
	if got, want := stringAttribute(t, span, tracing.AttributeOutputMessages), tracing.OutputMessages("partial answer", tracing.FinishReasonError); got != want {
		t.Errorf("%s = %s, want the text produced before the failure as %s", tracing.AttributeOutputMessages, got, want)
	}
	if got := stringAttribute(t, span, tracing.AttributeErrorType); got != "runtime_failure" {
		t.Errorf("%s = %q, want a safe category", tracing.AttributeErrorType, got)
	}
}

// Both segments of an approval belong to the same task. The resumed segment
// links back to the one that parked it and captures only its own text.
func TestResumeLinksToTheOriginatingSegment(t *testing.T) {
	recorder := newTraceRecorder(t)
	executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		if err := sink.TextDelta(runtime.TextDelta{Text: "before"}); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Outcome{Pending: &fakePendingTurn{
			request: &runtime.ApprovalRequest{ID: "7", CallID: "call-1", Name: "tools.write", Hint: "Allow tools.write?"},
			resume: func(_ context.Context, _ runtime.InputResponse, sink runtime.EventSink) (runtime.Outcome, error) {
				if err := sink.TextDelta(runtime.TextDelta{Text: "after"}); err != nil {
					return runtime.Outcome{}, err
				}
				return runtime.Outcome{}, nil
			},
		}}, nil
	}}, &fakeContinuation{}, captureTelemetry(0))
	if err != nil {
		t.Fatal(err)
	}

	first := requestContext("task-resume", "write")
	events, errs := collect(executor.Execute(recorder.segment(t.Context()), first))
	if len(errs) != 0 || len(events) != 3 {
		t.Fatalf("first segment events/errors = %#v/%v", events, errs)
	}
	update, ok := events[2].(*a2atype.TaskStatusUpdateEvent)
	if !ok || update.Status.State != a2atype.TaskStateInputRequired {
		t.Fatalf("input-required event = %#v", events[2])
	}
	decision := a2atype.NewMessage(a2atype.MessageRoleUser)
	decision.TaskID, decision.ContextID = first.TaskID, first.ContextID
	if err := apia2a.AttachHITL(decision, apia2a.ToolApprovalResponse{
		Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{{ID: "7", Approved: true}},
	}); err != nil {
		t.Fatal(err)
	}
	second := &a2asrv.ExecutorContext{
		TaskID: first.TaskID, ContextID: first.ContextID, Message: decision,
		StoredTask: &a2atype.Task{ID: first.TaskID, ContextID: first.ContextID, Status: a2atype.TaskStatus{
			State: a2atype.TaskStateInputRequired, Message: update.Status.Message,
		}},
	}
	if _, errs := collect(executor.Execute(recorder.segment(t.Context()), second)); len(errs) != 0 {
		t.Fatalf("second segment errors = %v", errs)
	}

	spans := recorder.spans(t, 2)
	origin, resumed := spans[0], spans[1]
	if got := stringAttribute(t, origin, tracing.AttributeSegment); got != tracing.SegmentInitial {
		t.Errorf("origin %s = %q, want %q", tracing.AttributeSegment, got, tracing.SegmentInitial)
	}
	if got := stringAttribute(t, origin, tracing.AttributeTaskState); got != string(a2atype.TaskStateInputRequired) {
		t.Errorf("origin %s = %q, want %q", tracing.AttributeTaskState, got, a2atype.TaskStateInputRequired)
	}
	if got := stringAttribute(t, resumed, tracing.AttributeSegment); got != tracing.SegmentResumed {
		t.Errorf("resumed %s = %q, want %q", tracing.AttributeSegment, got, tracing.SegmentResumed)
	}
	if len(resumed.Links) != 1 || resumed.Links[0].SpanContext.SpanID() != origin.SpanContext.SpanID() {
		t.Fatalf("resumed links = %#v, want one link to the originating segment", resumed.Links)
	}
	if got := linkAttribute(resumed.Links[0], tracing.AttributeLinkRelationship); got != tracing.RelationshipResumeOrigin {
		t.Errorf("link %s = %q, want %q", tracing.AttributeLinkRelationship, got, tracing.RelationshipResumeOrigin)
	}
	if got := stringAttribute(t, resumed, tracing.AttributeTaskID); got != string(first.TaskID) {
		t.Errorf("resumed %s = %q, want the same task", tracing.AttributeTaskID, got)
	}
	if got, want := stringAttribute(t, origin, tracing.AttributeOutputMessages), tracing.OutputMessages("before", tracing.FinishReasonToolCall); got != want {
		t.Errorf("origin %s = %s, want only the text that segment produced", tracing.AttributeOutputMessages, got)
	}
	if got, want := stringAttribute(t, resumed, tracing.AttributeOutputMessages), tracing.OutputMessages("after", tracing.FinishReasonStop); got != want {
		t.Errorf("resumed %s = %s, want only the text that segment produced", tracing.AttributeOutputMessages, got)
	}
	// An approval decision is structured outcome metadata, not prompt text, so
	// the resumed segment records no input messages at all.
	if got, ok := attributeOf(resumed, tracing.AttributeInputMessages); ok {
		t.Errorf("resumed %s = %s, want no captured prompt", tracing.AttributeInputMessages, got.AsString())
	}
}

func boolAttribute(t *testing.T, span tracetest.SpanStub, key string) bool {
	t.Helper()
	value, ok := attributeOf(span, key)
	if !ok {
		t.Fatalf("span is missing attribute %s", key)
	}
	return value.AsBool()
}

func linkAttribute(link sdktrace.Link, key string) string {
	for _, attr := range link.Attributes {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

// Cancel publishes its canceled event as soon as the executor releases the
// Actor, and the gateway may suspend the Actor on that event. The segment has
// to be exported before the event is yielded, not merely before execution
// returns.
func TestCancellationExportsBeforeYieldingItsEvent(t *testing.T) {
	recorder := newTraceRecorder(t)
	started := make(chan struct{})
	executor, err := New(fakeRunner{run: func(ctx context.Context, _ runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
		close(started)
		<-ctx.Done()
		return runtime.Outcome{}, ctx.Err()
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		collect(executor.Execute(recorder.segment(context.Background()), requestContext("task-cancel-order", "hello")))
	}()
	<-started

	events := 0
	for event, err := range executor.Cancel(t.Context(), requestContext("task-cancel-order", "ignored")) {
		if err != nil {
			t.Fatalf("cancellation error = %v", err)
		}
		events++
		if _, ok := event.(*a2atype.TaskStatusUpdateEvent); !ok {
			t.Fatalf("cancellation event = %#v", event)
		}
		if exported := len(recorder.exporter.GetSpans()); exported != 1 {
			t.Fatalf("exported %d spans when the canceled event was published, want the segment already exported", exported)
		}
	}
	<-done
	if events != 1 {
		t.Fatalf("cancellation yielded %d events, want one", events)
	}
	recorder.spans(t, 1)
}

func TestFinishReason(t *testing.T) {
	for _, test := range []struct {
		name   string
		result tracing.Result
		want   string
	}{
		{name: "completed", result: tracing.Result{TaskState: string(a2atype.TaskStateCompleted)}, want: tracing.FinishReasonStop},
		{name: "parked for input", result: tracing.Result{TaskState: string(a2atype.TaskStateInputRequired)}, want: tracing.FinishReasonToolCall},
		{name: "failed", result: tracing.Result{TaskState: string(a2atype.TaskStateFailed), Error: "runtime_failure"}, want: tracing.FinishReasonError},
		{name: "rejected", result: tracing.Result{Error: "invalid_request"}, want: tracing.FinishReasonError},
		{name: "canceled", result: tracing.Result{TaskState: string(a2atype.TaskStateCanceled), Disposition: tracing.DispositionCanceled}, want: tracing.DispositionCanceled},
		{name: "abandoned", result: tracing.Result{Disposition: tracing.DispositionAbandoned}, want: tracing.DispositionAbandoned},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := finishReason(test.result); got != test.want {
				t.Fatalf("finishReason() = %q, want %q", got, test.want)
			}
		})
	}
}

// A request rejected before execution yields a JSON-RPC error rather than a
// task event. The segment is exported before that error leaves, as it is for
// the quiescent paths, and still names the task it belonged to.
func TestValidationFailureExportsBeforeYieldingItsError(t *testing.T) {
	recorder := newTraceRecorder(t)
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		t.Fatal("the runner ran for an invalid request")
		return runtime.Outcome{}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	request := requestContext("task-invalid", "hello")
	request.Message.Role = a2atype.MessageRoleAgent
	errs := 0
	for event, err := range executor.Execute(recorder.segment(t.Context()), request) {
		if err == nil {
			t.Fatalf("invalid request produced event %#v", event)
		}
		errs++
		if exported := len(recorder.exporter.GetSpans()); exported != 1 {
			t.Fatalf("exported %d spans when the error was yielded, want the segment already exported", exported)
		}
	}
	if errs != 1 {
		t.Fatalf("invalid request yielded %d errors, want one", errs)
	}
	span := recorder.spans(t, 1)[0]
	if got := stringAttribute(t, span, tracing.AttributeErrorType); got != "invalid_request" {
		t.Errorf("%s = %q, want %q", tracing.AttributeErrorType, got, "invalid_request")
	}
	if got := stringAttribute(t, span, tracing.AttributeTaskID); got != "task-invalid" {
		t.Errorf("%s = %q, want the rejected task", tracing.AttributeTaskID, got)
	}
}

// A panic in the runner is a defect. The segment still exports with a fixed
// category, never the panic value, and the panic still propagates.
func TestInvocationRecordsARunnerPanic(t *testing.T) {
	recorder := newTraceRecorder(t)
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		panic("provider said sk-secret")
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the runner panic did not propagate")
			}
		}()
		collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-panic", "hello")))
	}()

	span := recorder.spans(t, 1)[0]
	if got := stringAttribute(t, span, tracing.AttributeErrorType); got != "runtime_panic" {
		t.Errorf("%s = %q, want %q", tracing.AttributeErrorType, got, "runtime_panic")
	}
	if span.Status.Code != codes.Error {
		t.Errorf("status = %v, want an error status", span.Status.Code)
	}
	if strings.Contains(span.Status.Description, "sk-secret") {
		t.Errorf("status description leaked the panic value: %q", span.Status.Description)
	}
}

// approvalDecision builds the request that answers the approval a segment
// parked on.
func approvalDecision(t *testing.T, first *a2asrv.ExecutorContext, parked *a2atype.TaskStatusUpdateEvent, id string) *a2asrv.ExecutorContext {
	t.Helper()
	decision := a2atype.NewMessage(a2atype.MessageRoleUser)
	decision.TaskID, decision.ContextID = first.TaskID, first.ContextID
	if err := apia2a.AttachHITL(decision, apia2a.ToolApprovalResponse{
		Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{{ID: id, Approved: true}},
	}); err != nil {
		t.Fatal(err)
	}
	return &a2asrv.ExecutorContext{
		TaskID: first.TaskID, ContextID: first.ContextID, Message: decision,
		StoredTask: &a2atype.Task{ID: first.TaskID, ContextID: first.ContextID, Status: a2atype.TaskStatus{
			State: a2atype.TaskStateInputRequired, Message: parked.Status.Message,
		}},
	}
}

func parkedStatus(t *testing.T, events []a2atype.Event, errs []error) *a2atype.TaskStatusUpdateEvent {
	t.Helper()
	if len(errs) != 0 || len(events) == 0 {
		t.Fatalf("segment events/errors = %#v/%v", events, errs)
	}
	update, ok := events[len(events)-1].(*a2atype.TaskStatusUpdateEvent)
	if !ok || update.Status.State != a2atype.TaskStateInputRequired {
		t.Fatalf("last event = %#v, want input required", events[len(events)-1])
	}
	return update
}

// A turn that pauses twice keeps one native process, so both resumed segments
// link to the segment that started it rather than forming a chain.
func TestResumedSegmentsLinkToTheNativeOriginAcrossPauses(t *testing.T) {
	recorder := newTraceRecorder(t)
	second := &fakePendingTurn{
		request: &runtime.ApprovalRequest{ID: "2", CallID: "call-2", Name: "tools.write", Hint: "Allow tools.write?"},
		resume: func(context.Context, runtime.InputResponse, runtime.EventSink) (runtime.Outcome, error) {
			return runtime.Outcome{}, nil
		},
	}
	first := &fakePendingTurn{
		request: &runtime.ApprovalRequest{ID: "1", CallID: "call-1", Name: "tools.read", Hint: "Allow tools.read?"},
		resume: func(context.Context, runtime.InputResponse, runtime.EventSink) (runtime.Outcome, error) {
			return runtime.Outcome{Pending: second}, nil
		},
	}
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{Pending: first}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	request := requestContext("task-two-pauses", "read then write")
	events, errs := collect(executor.Execute(recorder.segment(t.Context()), request))
	parkedOnce := parkedStatus(t, events, errs)
	events, errs = collect(executor.Execute(recorder.segment(t.Context()), approvalDecision(t, request, parkedOnce, "1")))
	parkedTwice := parkedStatus(t, events, errs)
	if _, errs := collect(executor.Execute(recorder.segment(t.Context()), approvalDecision(t, request, parkedTwice, "2"))); len(errs) != 0 {
		t.Fatalf("final segment errors = %v", errs)
	}

	spans := recorder.spans(t, 3)
	origin := spans[0].SpanContext.SpanID()
	for index, resumed := range spans[1:] {
		if got := stringAttribute(t, resumed, tracing.AttributeSegment); got != tracing.SegmentResumed {
			t.Errorf("segment %d %s = %q, want %q", index+2, tracing.AttributeSegment, got, tracing.SegmentResumed)
		}
		if len(resumed.Links) != 1 || resumed.Links[0].SpanContext.SpanID() != origin {
			t.Errorf("segment %d links = %#v, want one link to the originating segment", index+2, resumed.Links)
		}
	}
}
