package a2a

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	a2alog "github.com/a2aproject/a2a-go/v2/log"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel"
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
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
}

func newTraceRecorder(t *testing.T) *traceRecorder {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return &traceRecorder{exporter: exporter, provider: provider, tracer: provider.Tracer("test")}
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
	return approvalVerdict(t, first, parked, id, true)
}

func approvalVerdict(t *testing.T, first *a2asrv.ExecutorContext, parked *a2atype.TaskStatusUpdateEvent, id string, approved bool) *a2asrv.ExecutorContext {
	t.Helper()
	approval := apia2a.ToolApproval{ID: id, Approved: approved}
	if !approved {
		approval.RejectionReason = "denied"
	}
	decision := a2atype.NewMessage(a2atype.MessageRoleUser)
	decision.TaskID, decision.ContextID = first.TaskID, first.ContextID
	if err := apia2a.AttachHITL(decision, apia2a.ToolApprovalResponse{
		Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{approval},
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

func TestModelCallRecordsChatSpan(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name       string
		call       runtime.ModelCall
		wantName   string
		wantString map[string]string
		wantInt    map[string]int64
		wantFinish []string
		wantAbsent []string
	}{
		{
			name: "all fields reported",
			call: runtime.ModelCall{
				Provider: "anthropic", RequestModel: "opus", ResponseModel: "claude-opus-5-5",
				InputTokens: 38368, CacheReadTokens: 10699, CacheWriteTokens: 27667, OutputTokens: 4,
				StopReason: "end_turn", Start: start, End: start.Add(time.Second),
			},
			wantName: "chat opus",
			wantString: map[string]string{
				"gen_ai.operation.name": "chat", "gen_ai.provider.name": "anthropic", "gen_ai.request.model": "opus",
				"gen_ai.response.model": "claude-opus-5-5", "gen_ai.conversation.id": testContextID,
			},
			wantInt: map[string]int64{
				"gen_ai.usage.input_tokens": 38368, "gen_ai.usage.output_tokens": 4,
				"gen_ai.usage.cache_read.input_tokens": 10699, "gen_ai.usage.cache_write.input_tokens": 27667,
			},
			wantFinish: []string{"end_turn"},
		},
		{
			name:     "unknown fields omitted",
			call:     runtime.ModelCall{Provider: "openai", ResponseModel: "gpt", InputTokens: 10, OutputTokens: 2, UsagePartial: true, Start: start, End: start},
			wantName: "chat",
			wantString: map[string]string{
				"gen_ai.operation.name": "chat", "gen_ai.provider.name": "openai", "gen_ai.response.model": "gpt",
				"gen_ai.conversation.id": testContextID,
			},
			wantInt: map[string]int64{"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2},
			wantAbsent: []string{
				"gen_ai.request.model", "gen_ai.usage.cache_read.input_tokens", "gen_ai.usage.cache_write.input_tokens",
				"gen_ai.response.finish_reasons",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				return runtime.Outcome{}, sink.ModelCall(tc.call)
			}
			executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
			for range executor.Execute(recorder.segment(t.Context()), requestContext("task-chat", "hello")) {
			}

			spans := recorder.spans(t, 2)
			chat, invoke := spans[0], spans[1]
			if chat.Name != tc.wantName || chat.Parent.SpanID() != invoke.SpanContext.SpanID() {
				t.Fatalf("chat span = %q parent %s, want %q child of %s", chat.Name, chat.Parent.SpanID(), tc.wantName, invoke.SpanContext.SpanID())
			}
			if !chat.StartTime.Equal(tc.call.Start) || !chat.EndTime.Equal(tc.call.End) {
				t.Errorf("chat span times = %s..%s", chat.StartTime, chat.EndTime)
			}
			for key, want := range tc.wantString {
				if got := stringAttribute(t, chat, key); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
			for key, want := range tc.wantInt {
				if got, _ := attributeOf(chat, key); got.AsInt64() != want {
					t.Errorf("%s = %d, want %d", key, got.AsInt64(), want)
				}
			}
			if tc.wantFinish != nil {
				if got, _ := attributeOf(chat, "gen_ai.response.finish_reasons"); !slices.Equal(got.AsStringSlice(), tc.wantFinish) {
					t.Errorf("finish_reasons = %v, want %v", got.AsStringSlice(), tc.wantFinish)
				}
			}
			for _, key := range tc.wantAbsent {
				if _, ok := attributeOf(chat, key); ok {
					t.Errorf("%s present, want absent", key)
				}
			}
		})
	}
}

func TestToolCallsRecordExecuteToolSpans(t *testing.T) {
	recorder := newTraceRecorder(t)
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		for _, err := range []error{
			sink.ToolCall(runtime.ToolCall{ID: "call_ok", Name: "Bash"}),
			sink.ToolCall(runtime.ToolCall{ID: "call_bad", Name: "mcp__kagent__k8s_get"}),
			sink.ToolResult(runtime.ToolResult{ID: "call_bad", Name: "mcp__kagent__k8s_get", IsError: true}),
			sink.ToolResult(runtime.ToolResult{ID: "call_ok", Name: "Bash"}),
			sink.ToolCall(runtime.ToolCall{ID: "call_open", Name: "Read"}),
		} {
			if err != nil {
				return runtime.Outcome{}, err
			}
		}
		return runtime.Outcome{}, nil
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
	for range executor.Execute(recorder.segment(t.Context()), requestContext("task-tools", "hello")) {
	}

	spans := recorder.spans(t, 4)
	invoke := spans[3]
	for i, want := range []struct {
		name, id, errorType string
	}{
		{name: "mcp__kagent__k8s_get", id: "call_bad", errorType: "tool_error"},
		{name: "Bash", id: "call_ok"},
		{name: "Read", id: "call_open", errorType: "unfinished"},
	} {
		span := spans[i]
		if span.Name != "execute_tool "+want.name || span.Parent.SpanID() != invoke.SpanContext.SpanID() {
			t.Fatalf("span %d = %q parent %s, want execute_tool %s under invoke_agent", i, span.Name, span.Parent.SpanID(), want.name)
		}
		for key, value := range map[string]string{
			"gen_ai.operation.name": "execute_tool", "gen_ai.tool.name": want.name, "gen_ai.tool.call.id": want.id,
			"gen_ai.conversation.id": testContextID,
		} {
			if got := stringAttribute(t, span, key); got != value {
				t.Errorf("%s %s = %q, want %q", want.id, key, got, value)
			}
		}
		if got, _ := attributeOf(span, "error.type"); got.AsString() != want.errorType {
			t.Errorf("%s error.type = %q, want %q", want.id, got.AsString(), want.errorType)
		}
		wantCode := codes.Unset
		if want.errorType != "" {
			wantCode = codes.Error
		}
		if span.Status.Code != wantCode || span.Status.Description != want.errorType {
			t.Errorf("%s status = %v %q, want %v %q", want.id, span.Status.Code, span.Status.Description, wantCode, want.errorType)
		}
	}
}

func TestToolResultInResumedSegmentRecordsSpan(t *testing.T) {
	recorder := newTraceRecorder(t)
	parkThenResume(t, recorder, gatedTurn{
		first: startGatedCall,
		resume: func(sink runtime.EventSink) error {
			return sink.ToolResult(runtime.ToolResult{ID: "call-1", Name: "tools.write", IsError: true})
		},
		approved: true,
	})

	// The paused segment records no span for the tool awaiting approval; the resumed one records it once.
	spans := recorder.spans(t, 3)
	resumed := spans[1]
	if resumed.Name != "execute_tool tools.write" || resumed.Parent.SpanID() != spans[2].SpanContext.SpanID() {
		t.Fatalf("resumed span = %q parent %s, want execute_tool under the resumed invoke_agent", resumed.Name, resumed.Parent.SpanID())
	}
	if got := stringAttribute(t, resumed, "error.type"); got != "tool_error" || resumed.Status.Code != codes.Error {
		t.Errorf("resumed span error.type = %q status %v, want tool_error", got, resumed.Status.Code)
	}
}

func TestModelCallReportedAfterThePauseStaysInItsSegment(t *testing.T) {
	recorder := newTraceRecorder(t)
	var asked time.Time
	parkThenResume(t, recorder, gatedTurn{
		first: func(sink runtime.EventSink) error {
			asked = time.Now()
			return startGatedCall(sink)
		},
		resume: func(sink runtime.EventSink) error {
			now := time.Now()
			for _, call := range []runtime.ModelCall{
				{RequestModel: "asked", Start: asked, End: asked.Add(time.Millisecond)},
				{RequestModel: "resumed", Start: now, End: now.Add(time.Millisecond)},
			} {
				if err := sink.ModelCall(call); err != nil {
					return err
				}
			}
			return nil
		},
	})

	spans := recorder.spans(t, 4)
	var segments tracetest.SpanStubs
	parents := map[string]trace.SpanID{}
	for _, span := range spans {
		if strings.HasPrefix(span.Name, "invoke_agent") {
			segments = append(segments, span)
		} else {
			parents[span.Name] = span.Parent.SpanID()
		}
	}
	slices.SortFunc(segments, func(a, b tracetest.SpanStub) int { return a.StartTime.Compare(b.StartTime) })
	if len(segments) != 2 {
		t.Fatalf("invoke_agent spans = %d, want 2", len(segments))
	}
	for name, want := range map[string]trace.SpanID{
		"chat asked": segments[0].SpanContext.SpanID(), "chat resumed": segments[1].SpanContext.SpanID(),
	} {
		if parents[name] != want {
			t.Errorf("%s parent = %s, want %s", name, parents[name], want)
		}
	}
}

func TestApprovedToolWithoutResultRecordsUnfinished(t *testing.T) {
	recorder := newTraceRecorder(t)
	parkThenResume(t, recorder, gatedTurn{first: startGatedCall, resume: noEvents, approved: true})

	spans := recorder.spans(t, 3)
	tool, resumed := spans[1], spans[2]
	if tool.Name != "execute_tool tools.write" || tool.Parent.SpanID() != resumed.SpanContext.SpanID() {
		t.Fatalf("span = %q parent %s, want execute_tool under the resumed invoke_agent", tool.Name, tool.Parent.SpanID())
	}
	if got := stringAttribute(t, tool, "error.type"); got != "unfinished" || tool.StartTime.Before(resumed.StartTime) {
		t.Errorf("error.type = %q start %s, want unfinished from the resume", got, tool.StartTime)
	}
}

// Both real drivers answer a denial with a failed ToolResult for the gated call.
func TestDeniedToolRecordsNoSpan(t *testing.T) {
	recorder := newTraceRecorder(t)
	parkThenResume(t, recorder, gatedTurn{
		first: startGatedCall,
		resume: func(sink runtime.EventSink) error {
			return sink.ToolResult(runtime.ToolResult{ID: "call-1", Name: "tools.write", IsError: true})
		},
	})

	for _, span := range recorder.spans(t, 2) {
		if strings.HasPrefix(span.Name, "execute_tool") {
			t.Errorf("span %q recorded for a tool that never ran", span.Name)
		}
	}
}

// Claude can deliver the gated ToolCall after the pause, in the resumed segment.
func TestLateGatedToolCall(t *testing.T) {
	for _, tc := range []struct {
		name     string
		approved bool
		noResult bool
		want     int
	}{
		{name: "denied", want: 2},
		{name: "denied without result", noResult: true, want: 2},
		{name: "approved", approved: true, want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			parkThenResume(t, recorder, gatedTurn{
				first: noEvents,
				resume: func(sink runtime.EventSink) error {
					if err := startGatedCall(sink); err != nil || tc.noResult {
						return err
					}
					return sink.ToolResult(runtime.ToolResult{ID: "call-1", Name: "tools.write", IsError: !tc.approved})
				},
				approved: tc.approved,
			})
			recorder.spans(t, tc.want)
		})
	}
}

func TestUnfinishedToolResultRecordsUnfinished(t *testing.T) {
	recorder := newTraceRecorder(t)
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		if err := sink.ToolCall(runtime.ToolCall{ID: "c1", Name: "command_execution"}); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Outcome{}, sink.ToolResult(runtime.ToolResult{ID: "c1", Name: "command_execution", IsError: true, Status: runtime.ToolUnfinished})
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})

	if _, errs := collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-unfinished", "go"))); len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	tool := recorder.spans(t, 2)[0]
	if got := stringAttribute(t, tool, "error.type"); tool.Name != "execute_tool command_execution" || got != "unfinished" {
		t.Errorf("span %q error.type = %q, want execute_tool with unfinished", tool.Name, got)
	}
}

func TestDeclinedToolRecordsNoSpanWithoutApproval(t *testing.T) {
	recorder := newTraceRecorder(t)
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		if err := sink.ToolCall(runtime.ToolCall{ID: "c1", Name: "command_execution"}); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Outcome{}, sink.ToolResult(runtime.ToolResult{ID: "c1", Name: "command_execution", Status: runtime.ToolDeclined})
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})

	if _, errs := collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-declined", "go"))); len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	recorder.spans(t, 1)
}

func TestPauseKeepsOtherOpenToolsWithTheirStart(t *testing.T) {
	recorder := newTraceRecorder(t)
	_, run := gatedPair(func(sink runtime.EventSink) error {
		time.Sleep(time.Millisecond)
		for _, err := range []error{
			sink.ToolResult(runtime.ToolResult{ID: "call-a", Name: "Bash"}),
			sink.ToolResult(runtime.ToolResult{ID: "call-b", Name: "tools.write"}),
		} {
			if err != nil {
				return err
			}
		}
		return nil
	})
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})

	request := requestContext("task-two-tools", "go")
	events, errs := collect(executor.Execute(recorder.segment(t.Context()), request))
	parked := parkedStatus(t, events, errs)
	if _, errs := collect(executor.Execute(recorder.segment(t.Context()), approvalDecision(t, request, parked, "1"))); len(errs) != 0 {
		t.Fatalf("resumed segment errors = %v", errs)
	}

	spans := recorder.spans(t, 4)
	first, a, b, second := spans[0], spans[1], spans[2], spans[3]
	if a.Name != "execute_tool Bash" || b.Name != "execute_tool tools.write" {
		t.Fatalf("tool spans = %q, %q", a.Name, b.Name)
	}
	if a.StartTime.After(first.EndTime) || a.Parent.SpanID() != first.SpanContext.SpanID() {
		t.Errorf("tool A start %s parent %s, want it to start and stay under the first segment", a.StartTime, a.Parent.SpanID())
	}
	if b.Parent.SpanID() != second.SpanContext.SpanID() {
		t.Errorf("gated tool parent %s, want the resumed segment", b.Parent.SpanID())
	}
	if b.StartTime.Before(second.StartTime) || b.EndTime.Sub(b.StartTime) < time.Millisecond {
		t.Errorf("gated tool span %s..%s, want it to run from the resume to its result", b.StartTime, b.EndTime)
	}
}

func TestCancelWhilePausedRecordsOtherOpenToolsUnfinished(t *testing.T) {
	recorder := newTraceRecorder(t)
	_, run := gatedPair(func(runtime.EventSink) error { return nil })
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})

	request := requestContext("task-cancel-paused", "go")
	events, errs := collect(executor.Execute(recorder.segment(t.Context()), request))
	parkedStatus(t, events, errs)
	if _, errs := collect(executor.Cancel(t.Context(), request)); len(errs) != 0 {
		t.Fatalf("cancel errors = %v", errs)
	}

	spans := recorder.spans(t, 2)
	tool, invoke := spans[1], spans[0]
	if tool.Name != "execute_tool Bash" || tool.Parent.SpanID() != invoke.SpanContext.SpanID() {
		t.Fatalf("span = %q parent %s, want execute_tool Bash under the paused segment", tool.Name, tool.Parent.SpanID())
	}
	if got, _ := attributeOf(tool, "error.type"); got.AsString() != "unfinished" {
		t.Errorf("error.type = %q, want unfinished", got.AsString())
	}
	if got := stringAttribute(t, tool, "gen_ai.conversation.id"); got != testContextID {
		t.Errorf("conversation ID = %q, want %q", got, testContextID)
	}
}

func TestParkedToolsDoNotRetainTheRequestContext(t *testing.T) {
	contextType := reflect.TypeFor[context.Context]()
	for _, typ := range []reflect.Type{reflect.TypeFor[openTools](), reflect.TypeFor[parkedTask]()} {
		for field := range typ.Fields() {
			if field.Type.Implements(contextType) {
				t.Errorf("%s.%s holds a request context", typ.Name(), field.Name)
			}
		}
	}
}

func TestCancelWhilePausedFlushesUnfinishedTools(t *testing.T) {
	exporter, provider := newBatchedProvider(t)
	pending, run := gatedPair(func(runtime.EventSink) error { return nil })
	pending.cancel = func(_ context.Context, sink runtime.ModelCallSink) error {
		return sink.ModelCall(runtime.ModelCall{RequestModel: "m"})
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{Provider: "aws.bedrock"})

	request := requestContext("task-cancel-flush", "go")
	ctx, _ := tracing.StartInvocation(t.Context(), provider.Tracer("test"), "invoke_agent", provider.ForceFlush)
	events, errs := collect(executor.Execute(ctx, request))
	parkedStatus(t, events, errs)
	if _, errs := collect(executor.Cancel(t.Context(), request)); len(errs) != 0 {
		t.Fatalf("cancel errors = %v", errs)
	}

	spans := exporter.GetSpans()
	if got := len(spans); got != 3 {
		t.Fatalf("exported %d spans after cancel, want the paused segment, its unfinished tool and the buffered chat", got)
	}
	chat := spans[slices.IndexFunc(spans, func(span tracetest.SpanStub) bool { return span.Name == "chat m" })]
	if got := stringAttribute(t, chat, "gen_ai.provider.name"); got != "aws.bedrock" {
		t.Errorf("canceled chat provider = %q, want the compiled aws.bedrock", got)
	}
}

func TestModelCallDefaultsToCompiledModel(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		call                  runtime.ModelCall
		wantProvider, wantReq string
	}{
		{name: "driver reports none", wantProvider: "aws.bedrock", wantReq: "compiled"},
		{name: "driver reports its own", call: runtime.ModelCall{Provider: "anthropic", RequestModel: "native"}, wantProvider: "anthropic", wantReq: "native"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				return runtime.Outcome{}, sink.ModelCall(tc.call)
			}
			executor := newExecutor(t, run, tracing.RuntimeTelemetry{Provider: "aws.bedrock", Model: "compiled"})
			for range executor.Execute(recorder.segment(t.Context()), requestContext("task-model", "hello")) {
			}

			chat := recorder.spans(t, 2)[0]
			if got := stringAttribute(t, chat, "gen_ai.provider.name"); got != tc.wantProvider {
				t.Errorf("provider = %q, want %q", got, tc.wantProvider)
			}
			if got := stringAttribute(t, chat, "gen_ai.request.model"); got != tc.wantReq {
				t.Errorf("request model = %q, want %q", got, tc.wantReq)
			}
		})
	}
}

func TestModelCallUsageAndStatus(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name      string
		call      runtime.ModelCall
		wantUsage bool
		wantError bool
	}{
		{name: "interrupted before any usage", call: runtime.ModelCall{UsagePartial: true, StopReason: "error"}, wantError: true},
		{name: "interrupted with partial usage", call: runtime.ModelCall{InputTokens: 5, UsagePartial: true, StopReason: "error"}, wantUsage: true, wantError: true},
		{name: "final usage of zero", call: runtime.ModelCall{StopReason: "end_turn"}, wantUsage: true},
		{name: "canceled", call: runtime.ModelCall{InputTokens: 5, UsagePartial: true, StopReason: "error", Canceled: true}, wantUsage: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			call := tc.call
			call.Provider, call.ResponseModel, call.Start, call.End = "anthropic", "claude-opus-5-5", start, start
			run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				return runtime.Outcome{}, sink.ModelCall(call)
			}
			executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
			for range executor.Execute(recorder.segment(t.Context()), requestContext("task-usage", "hello")) {
			}

			chat := recorder.spans(t, 2)[0]
			for _, key := range []string{"gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens"} {
				if _, ok := attributeOf(chat, key); ok != tc.wantUsage {
					t.Errorf("%s present = %v, want %v", key, ok, tc.wantUsage)
				}
			}
			if partial, ok := attributeOf(chat, "kagent.model_call.usage_partial"); ok != tc.call.UsagePartial || (ok && !partial.AsBool()) {
				t.Errorf("usage_partial = %v (present %v), want present %v", partial.AsBool(), ok, tc.call.UsagePartial)
			}
			errorType, _ := attributeOf(chat, "error.type")
			if gotError := chat.Status.Code == codes.Error && errorType.AsString() == "model_error"; gotError != tc.wantError {
				t.Errorf("status = %v error.type = %q, want error %v", chat.Status.Code, errorType.AsString(), tc.wantError)
			}
		})
	}
}

func TestCancelWinningTheParkRaceFlushesOpenTools(t *testing.T) {
	exporter, provider := newBatchedProvider(t)
	started := make(chan struct{})
	run := func(ctx context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		if err := sink.ToolCall(runtime.ToolCall{ID: "call-a", Name: "Bash"}); err != nil {
			return runtime.Outcome{}, err
		}
		close(started)
		<-ctx.Done()
		return runtime.Outcome{Pending: &fakePendingTurn{
			request: &runtime.ApprovalRequest{ID: "1", CallID: "call-b", Name: "tools.write"},
			cancel: func(_ context.Context, sink runtime.ModelCallSink) error {
				return sink.ModelCall(runtime.ModelCall{RequestModel: "m"})
			},
		}}, nil
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})

	request := requestContext("task-park-race", "go")
	ctx, _ := tracing.StartInvocation(t.Context(), provider.Tracer("test"), "invoke_agent", provider.ForceFlush)
	done := make(chan struct{})
	go func() {
		defer close(done)
		collect(executor.Execute(ctx, request))
	}()
	<-started
	if _, errs := collect(executor.Cancel(t.Context(), request)); len(errs) != 0 {
		t.Fatalf("cancel errors = %v", errs)
	}
	<-done

	if got := len(exporter.GetSpans()); got != 3 {
		t.Fatalf("exported %d spans after cancel, want the segment, its unfinished tool and the buffered chat", got)
	}
}

func TestModelCallRecordedWithoutAnAdoptedInvocation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		invocation bool
	}{
		{name: "caller disconnected first", invocation: true},
		{name: "no invocation", invocation: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(recorder.provider)
			t.Cleanup(func() { otel.SetTracerProvider(previous) })
			ctx := t.Context()
			if tc.invocation {
				var invocation *tracing.Invocation
				ctx, invocation = tracing.StartInvocation(ctx, recorder.tracer, "invoke_agent", nil)
				_, _ = invocation.EndTransport(ctx, tracing.Result{})
			}
			run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				return runtime.Outcome{}, sink.ModelCall(runtime.ModelCall{RequestModel: "m", InputTokens: 3})
			}
			executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
			for range executor.Execute(ctx, requestContext("task-unadopted", "hello")) {
			}

			spans := recorder.exporter.GetSpans()
			if len(spans) == 0 || spans[len(spans)-1].Name != "chat m" {
				t.Fatalf("spans = %v, want a chat span", spans.Snapshots())
			}
			chat := spans[len(spans)-1]
			if tc.invocation && chat.Parent.SpanID() != spans[0].SpanContext.SpanID() {
				t.Errorf("chat parent = %s, want the ended invocation %s", chat.Parent.SpanID(), spans[0].SpanContext.SpanID())
			}
			if !tc.invocation && chat.Parent.IsValid() {
				t.Errorf("chat parent = %s, want a new root", chat.Parent.SpanID())
			}
		})
	}
}

func TestUnadoptedInvocationFlushesRuntimeSpans(t *testing.T) {
	exporter, provider := newBatchedProvider(t)
	ctx, invocation := tracing.StartInvocation(t.Context(), provider.Tracer("test"), "invoke_agent", provider.ForceFlush)
	_, _ = invocation.EndTransport(ctx, tracing.Result{})
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{}, sink.ModelCall(runtime.ModelCall{RequestModel: "m", InputTokens: 3})
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
	collect(executor.Execute(ctx, requestContext("task-unadopted-flush", "hello")))

	if got := len(exporter.GetSpans()); got != 2 {
		t.Fatalf("exported %d spans, want the ended invocation and the chat span", got)
	}
}

func gatedPair(
	resume func(runtime.EventSink) error,
) (*fakePendingTurn, func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error)) {
	pending := &fakePendingTurn{
		request: &runtime.ApprovalRequest{ID: "1", CallID: "call-b", Name: "tools.write", Hint: "Allow tools.write?"},
		resume: func(_ context.Context, _ runtime.InputResponse, sink runtime.EventSink) (runtime.Outcome, error) {
			return runtime.Outcome{}, resume(sink)
		},
	}
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		for _, err := range []error{
			sink.ToolCall(runtime.ToolCall{ID: "call-a", Name: "Bash"}),
			sink.ToolCall(runtime.ToolCall{ID: "call-b", Name: "tools.write"}),
		} {
			if err != nil {
				return runtime.Outcome{}, err
			}
		}
		return runtime.Outcome{Pending: pending}, nil
	}
	return pending, run
}

func newExecutor(
	t *testing.T,
	run func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error),
	telemetry tracing.RuntimeTelemetry,
) *Executor {
	t.Helper()
	executor, err := New(fakeRunner{run: run}, &fakeContinuation{}, telemetry)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func newBatchedProvider(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Hour)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return exporter, provider
}

func startGatedCall(sink runtime.EventSink) error {
	return sink.ToolCall(runtime.ToolCall{ID: "call-1", Name: "tools.write"})
}

func noEvents(runtime.EventSink) error { return nil }

// gatedTurn runs first, parks on approval of call-1, then runs resume after the verdict.
type gatedTurn struct {
	first, resume func(runtime.EventSink) error
	approved      bool
}

func parkThenResume(t *testing.T, recorder *traceRecorder, turn gatedTurn) {
	t.Helper()
	pending := &fakePendingTurn{
		request: &runtime.ApprovalRequest{ID: "1", CallID: "call-1", Name: "tools.write", Hint: "Allow tools.write?"},
		resume: func(_ context.Context, _ runtime.InputResponse, sink runtime.EventSink) (runtime.Outcome, error) {
			return runtime.Outcome{}, turn.resume(sink)
		},
	}
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{Pending: pending}, turn.first(sink)
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
	request := requestContext("task-gated", "write")
	events, errs := collect(executor.Execute(recorder.segment(t.Context()), request))
	parked := parkedStatus(t, events, errs)
	verdict := approvalVerdict(t, request, parked, "1", turn.approved)
	if _, errs := collect(executor.Execute(recorder.segment(t.Context()), verdict)); len(errs) != 0 {
		t.Fatalf("resumed segment errors = %v", errs)
	}
}

func TestCancelOfAParkedTaskFailsOpenOnDriverErrors(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	pending, run := gatedPair(noEvents)
	pending.cancel = func(context.Context, runtime.ModelCallSink) error {
		close(entered)
		<-release
		return errors.New("drain failed")
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
	request := requestContext("task-cancel-error", "go")
	events, errs := collect(executor.Execute(newTraceRecorder(t).segment(t.Context()), request))
	parkedStatus(t, events, errs)

	var logs strings.Builder
	ctx := a2alog.AttachLogger(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
	type result struct {
		events []a2atype.Event
		errs   []error
	}
	first, second := make(chan result, 1), make(chan result, 1)
	go func() {
		events, errs := collect(executor.Cancel(ctx, request))
		first <- result{events, errs}
	}()
	<-entered
	go func() {
		events, errs := collect(executor.Cancel(t.Context(), request))
		second <- result{events, errs}
	}()
	// Let the second Cancel reach the canceling task before the first finishes.
	time.Sleep(100 * time.Millisecond)
	close(release)
	for name, got := range map[string]result{"first": <-first, "second": <-second} {
		if len(got.errs) != 0 || len(got.events) != 1 || got.events[0].(*a2atype.TaskStatusUpdateEvent).Status.State != a2atype.TaskStateCanceled {
			t.Errorf("%s Cancel() events/errors = %#v/%v, want canceled", name, got.events, got.errs)
		}
	}
	if !strings.Contains(logs.String(), "drain failed") {
		t.Errorf("logs = %q, want the driver error", logs.String())
	}
}
