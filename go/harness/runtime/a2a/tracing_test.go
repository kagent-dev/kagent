package a2a

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	a2alog "github.com/a2aproject/a2a-go/v2/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
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

// Both resumed segments link to the initial task invocation across pauses.
func TestResumedSegmentsLinkToTheTaskOriginAcrossPauses(t *testing.T) {
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
				StopReason: "stop", Start: start, End: start.Add(time.Second),
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
			wantFinish: []string{"stop"},
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
				sink.ModelCall(tc.call)
				return runtime.Outcome{}, nil
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

func TestUsageOnlyModelCallRecordsNoSpan(t *testing.T) {
	recorder := newTraceRecorder(t)
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		sink.ModelCall(runtime.ModelCall{OutputTokens: 90, UsageOnly: true})
		return runtime.Outcome{}, nil
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
	for range executor.Execute(recorder.segment(t.Context()), requestContext("task-usage", "hello")) {
	}
	if invoke := recorder.spans(t, 1)[0]; !strings.HasPrefix(invoke.Name, "invoke_agent") {
		t.Errorf("span = %q, want only invoke_agent", invoke.Name)
	}
}

func TestToolCallsRecordExecuteToolSpans(t *testing.T) {
	recorder := newTraceRecorder(t)
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		for _, err := range []error{
			sink.ToolCall(runtime.ToolCall{ID: "call_ok", Name: "Bash"}),
			sink.ToolCall(runtime.ToolCall{ID: "call_bad", Name: "mcp__kagent__k8s_get", MCPServer: "kagent", MCPTool: "k8s_get"}),
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
		name, id, server, errorType string
	}{
		{name: "k8s_get", id: "call_bad", server: "kagent", errorType: "tool_error"},
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
		if got, _ := attributeOf(span, "kagent.mcp.server.name"); got.AsString() != want.server {
			t.Errorf("%s kagent.mcp.server.name = %q, want %q", want.id, got.AsString(), want.server)
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

func TestToolSpanCapturesArgumentsAndResult(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		capture                bool
		limit                  int
		arguments              map[string]any
		result                 any
		isError                bool
		wantArguments          string // "" means absent
		wantResult             string
		wantArgumentsShortened bool
		wantResultShortened    bool
	}{
		{name: "capture off", arguments: map[string]any{"path": "notes.txt"}, result: "hello"},
		{
			name: "captured", capture: true, arguments: map[string]any{"path": "notes.txt"}, result: "hello",
			wantArguments: `{"path":"notes.txt"}`, wantResult: `"hello"`,
		},
		{
			// Cut like invoke_agent content: the first bytes within the budget, flagged as shortened.
			name: "arguments over the budget are cut", capture: true, limit: 8, arguments: map[string]any{"path": "notes.txt"}, result: "x",
			wantArguments: `{"path":`, wantResult: `"x"`, wantArgumentsShortened: true,
		},
		{
			name: "a result over the budget is cut", capture: true, limit: 4, arguments: map[string]any{}, result: "hello",
			wantArguments: `{}`, wantResult: `"hel`, wantResultShortened: true,
		},
		{
			name: "a failed call keeps its arguments only", capture: true, arguments: map[string]any{"a": 1}, result: "boom", isError: true,
			wantArguments: `{"a":1}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				if err := sink.ToolCall(runtime.ToolCall{ID: "c1", Name: "Read", Arguments: tc.arguments}); err != nil {
					return runtime.Outcome{}, err
				}
				return runtime.Outcome{}, sink.ToolResult(runtime.ToolResult{ID: "c1", Name: "Read", Result: tc.result, IsError: tc.isError})
			}
			executor := newExecutor(t, run, tracing.RuntimeTelemetry{CaptureContent: tc.capture, MaxCaptureBytes: tc.limit})
			if _, errs := collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-capture", "go"))); len(errs) != 0 {
				t.Fatalf("errors = %v", errs)
			}

			tool := recorder.spans(t, 2)[0]
			for key, want := range map[string]string{"gen_ai.tool.call.arguments": tc.wantArguments, "gen_ai.tool.call.result": tc.wantResult} {
				if got, ok := attributeOf(tool, key); ok != (want != "") || got.AsString() != want {
					t.Errorf("%s = %q (present %v), want %q", key, got.AsString(), ok, want)
				}
			}
			for key, want := range map[string]struct{ present, value bool }{
				"kagent.capture.input_truncated":  {tc.wantArguments != "", tc.wantArgumentsShortened},
				"kagent.capture.output_truncated": {tc.wantResult != "", tc.wantResultShortened},
			} {
				if got, ok := attributeOf(tool, key); ok != want.present || got.AsBool() != want.value {
					t.Errorf("%s = %v (present %v), want %v (present %v)", key, got.AsBool(), ok, want.value, want.present)
				}
			}
		})
	}
}

// Arguments are captured when the call starts, so they reach the span written after an approval.
func TestCapturedArgumentsSurviveAnApprovalPause(t *testing.T) {
	recorder := newTraceRecorder(t)
	parkThenResume(t, recorder, gatedTurn{
		first: func(sink runtime.EventSink) error {
			return sink.ToolCall(runtime.ToolCall{ID: "call-1", Name: "tools.write", Arguments: map[string]any{"value": 7}})
		},
		resume: func(sink runtime.EventSink) error {
			return sink.ToolResult(runtime.ToolResult{ID: "call-1", Name: "tools.write", Result: "ok"})
		},
		approved:  true,
		telemetry: tracing.RuntimeTelemetry{CaptureContent: true},
	})

	tool := recorder.spans(t, 3)[1]
	if got := stringAttribute(t, tool, "gen_ai.tool.call.arguments"); tool.Name != "execute_tool tools.write" || got != `{"value":7}` {
		t.Errorf("span %q arguments = %q, want the arguments from before the pause", tool.Name, got)
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

func TestModelCallReportedAfterRestoreUsesCurrentSegment(t *testing.T) {
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
				sink.ModelCall(call)
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
		"chat asked": segments[1].SpanContext.SpanID(), "chat resumed": segments[1].SpanContext.SpanID(),
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
			return sink.ToolResult(runtime.ToolResult{ID: "call-1", Name: "tools.write", IsError: true, Status: runtime.ToolDeclined})
		},
	})

	for _, span := range recorder.spans(t, 2) {
		if strings.HasPrefix(span.Name, "execute_tool") {
			t.Errorf("span %q recorded for a tool that never ran", span.Name)
		}
	}
}

// Claude can deliver the gated ToolCall after the pause, in the resumed segment. A denied
// call still gets a declined result, so it never gets a span.
func TestLateGatedToolCall(t *testing.T) {
	for _, tc := range []struct {
		name     string
		approved bool
		want     int
	}{
		{name: "denied", want: 2},
		{name: "approved", approved: true, want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			parkThenResume(t, recorder, gatedTurn{
				first: noEvents,
				resume: func(sink runtime.EventSink) error {
					if err := startGatedCall(sink); err != nil {
						return err
					}
					result := runtime.ToolResult{ID: "call-1", Name: "tools.write", IsError: !tc.approved}
					if !tc.approved {
						result.Status = runtime.ToolDeclined
					}
					return sink.ToolResult(result)
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
				sink.ModelCall(tc.call)
				return runtime.Outcome{}, nil
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
		wantError string
	}{
		{name: "interrupted before any usage", call: runtime.ModelCall{UsagePartial: true, StopReason: "error"}, wantError: "model_error"},
		{name: "interrupted with partial usage", call: runtime.ModelCall{InputTokens: 5, UsagePartial: true, StopReason: "error"}, wantUsage: true, wantError: "model_error"},
		{name: "runtime error category", call: runtime.ModelCall{UsagePartial: true, StopReason: "error", ErrorType: "rate_limit"}, wantError: "rate_limit"},
		{name: "final usage of zero", call: runtime.ModelCall{StopReason: "stop"}, wantUsage: true},
		{name: "canceled", call: runtime.ModelCall{InputTokens: 5, UsagePartial: true, StopReason: "error", Canceled: true}, wantUsage: true},
		{name: "response and request IDs", call: runtime.ModelCall{StopReason: "stop", ResponseID: "msg_1", RequestID: "req_1"}, wantUsage: true},
		{name: "reasoning tokens", call: runtime.ModelCall{StopReason: "stop", OutputTokens: 9, ReasoningOutputTokens: 7}, wantUsage: true},
		{name: "retried", call: runtime.ModelCall{StopReason: "stop", Retries: 2}, wantUsage: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			call := tc.call
			call.Provider, call.ResponseModel, call.Start, call.End = "anthropic", "claude-opus-5-5", start, start
			run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				sink.ModelCall(call)
				return runtime.Outcome{}, nil
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
			if partial, ok := attributeOf(chat, "kagent.inference.usage_partial"); ok != tc.call.UsagePartial || (ok && !partial.AsBool()) {
				t.Errorf("usage_partial = %v (present %v), want present %v", partial.AsBool(), ok, tc.call.UsagePartial)
			}
			if got, ok := attributeOf(chat, "gen_ai.usage.reasoning.output_tokens"); ok != (tc.call.ReasoningOutputTokens != 0) || got.AsInt64() != tc.call.ReasoningOutputTokens {
				t.Errorf("reasoning tokens = %d (present %v), want %d", got.AsInt64(), ok, tc.call.ReasoningOutputTokens)
			}
			if got, ok := attributeOf(chat, "kagent.inference.retry.count"); ok != (tc.call.Retries != 0) || got.AsInt64() != int64(tc.call.Retries) {
				t.Errorf("retry count = %d (present %v), want %d", got.AsInt64(), ok, tc.call.Retries)
			}
			for key, want := range map[string]string{"gen_ai.response.id": tc.call.ResponseID, "kagent.inference.request_id": tc.call.RequestID} {
				if got, ok := attributeOf(chat, key); ok != (want != "") || got.AsString() != want {
					t.Errorf("%s = %q (present %v), want %q", key, got.AsString(), ok, want)
				}
			}
			errorType, _ := attributeOf(chat, "error.type")
			wantCode := codes.Unset
			if tc.wantError != "" {
				wantCode = codes.Error
			}
			if chat.Status.Code != wantCode || chat.Status.Description != tc.wantError || errorType.AsString() != tc.wantError {
				t.Errorf("status = %v %q, error.type = %q, want %q", chat.Status.Code, chat.Status.Description, errorType.AsString(), tc.wantError)
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
				sink.ModelCall(runtime.ModelCall{RequestModel: "m"})
				return nil
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
				sink.ModelCall(runtime.ModelCall{RequestModel: "m", InputTokens: 3})
				return runtime.Outcome{}, nil
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
		sink.ModelCall(runtime.ModelCall{RequestModel: "m", InputTokens: 3})
		return runtime.Outcome{}, nil
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
	telemetry     tracing.RuntimeTelemetry
}

func parkThenResume(t *testing.T, recorder *traceRecorder, turn gatedTurn) {
	t.Helper()
	pending := &fakePendingTurn{
		request: &runtime.ApprovalRequest{ID: "1", CallID: "call-1", Name: "tools.write", Hint: "Allow tools.write?"},
		resume: func(_ context.Context, _ runtime.InputResponse, sink runtime.EventSink) (runtime.Outcome, error) {
			if turn.approved {
				sink.(runtime.ToolExecutionSink).ToolExecutionStarted(runtime.ToolCall{ID: "call-1", Name: "tools.write", Arguments: map[string]any{"value": 7}})
			}
			return runtime.Outcome{}, turn.resume(sink)
		},
	}
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{Pending: pending}, turn.first(sink)
	}
	executor := newExecutor(t, run, turn.telemetry)
	request := requestContext("task-gated", "write")
	events, errs := collect(executor.Execute(recorder.segment(t.Context()), request))
	parked := parkedStatus(t, events, errs)
	verdict := approvalVerdict(t, request, parked, "1", turn.approved)
	if _, errs := collect(executor.Execute(recorder.segment(t.Context()), verdict)); len(errs) != 0 {
		t.Fatalf("resumed segment errors = %v", errs)
	}
}

func TestCancelOfAParkedTaskReportsDriverErrors(t *testing.T) {
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
		if len(got.errs) != 1 || !strings.Contains(got.errs[0].Error(), "drain failed") || len(got.events) != 0 {
			t.Errorf("%s Cancel() events/errors = %#v/%v, want cleanup failure", name, got.events, got.errs)
		}
	}
}

func TestSubagentSpansNestUnderTheirTool(t *testing.T) {
	agentCall := runtime.ToolCall{ID: "call-a", Name: "Agent"}
	agentResult := runtime.ToolResult{ID: "call-a", Name: "Agent"}
	subagentCall := runtime.ModelCall{RequestModel: "m", ParentToolID: "call-a"}
	subagentTool := func(sink runtime.EventSink) error {
		return sink.ToolCall(runtime.ToolCall{ID: "call-r", Name: "Read", ParentToolID: "call-a"})
	}
	readResult := runtime.ToolResult{ID: "call-r", Name: "Read"}
	for _, tc := range []struct {
		name          string
		events        func(sink runtime.EventSink) error
		child         string // the subagent span to check; empty means its model call
		wantUnderTool bool
		wantToolError string
		wantNoTool    bool
	}{
		{name: "call while the tool runs", events: func(sink runtime.EventSink) error {
			err := sink.ToolCall(agentCall)
			sink.ModelCall(subagentCall)
			return errors.Join(err, sink.ToolResult(agentResult))
		}, wantUnderTool: true},
		// A background subagent returns its result before its calls arrive.
		{name: "call after the tool result", events: func(sink runtime.EventSink) error {
			err := errors.Join(sink.ToolCall(agentCall), sink.ToolResult(agentResult))
			sink.ModelCall(subagentCall)
			return err
		}, wantUnderTool: true},
		{name: "tool without a result", events: func(sink runtime.EventSink) error {
			err := sink.ToolCall(agentCall)
			sink.ModelCall(subagentCall)
			return err
		}, wantUnderTool: true, wantToolError: "unfinished"},
		{name: "declined tool", events: func(sink runtime.EventSink) error {
			err := sink.ToolCall(agentCall)
			sink.ModelCall(subagentCall)
			return errors.Join(err, sink.ToolResult(runtime.ToolResult{ID: "call-a", Name: "Agent", Status: runtime.ToolDeclined}))
		}, wantNoTool: true},
		{name: "subagent tool while the tool runs", events: func(sink runtime.EventSink) error {
			return errors.Join(sink.ToolCall(agentCall), subagentTool(sink), sink.ToolResult(readResult), sink.ToolResult(agentResult))
		}, child: "execute_tool Read", wantUnderTool: true},
		{name: "subagent tool after the tool result", events: func(sink runtime.EventSink) error {
			return errors.Join(sink.ToolCall(agentCall), sink.ToolResult(agentResult), subagentTool(sink), sink.ToolResult(readResult))
		}, child: "execute_tool Read", wantUnderTool: true},
		{name: "subagent tool open when the turn ends after its tool ended", events: func(sink runtime.EventSink) error {
			return errors.Join(sink.ToolCall(agentCall), sink.ToolResult(agentResult), subagentTool(sink))
		}, child: "execute_tool Read", wantUnderTool: true},
		{name: "subagent tool open when the turn ends", events: func(sink runtime.EventSink) error {
			return errors.Join(sink.ToolCall(agentCall), subagentTool(sink))
		}, child: "execute_tool Read", wantUnderTool: true, wantToolError: "unfinished"},
		{name: "tool that names itself as parent", events: func(sink runtime.EventSink) error {
			err := sink.ToolCall(runtime.ToolCall{ID: "call-a", Name: "Agent", ParentToolID: "call-a"})
			sink.ModelCall(subagentCall)
			return errors.Join(err, sink.ToolResult(agentResult))
		}, wantUnderTool: true},
		// The declined tool never ran, so its held call goes up to the tool above it.
		{name: "declined subagent tool", events: func(sink runtime.EventSink) error {
			err := errors.Join(sink.ToolCall(agentCall), subagentTool(sink))
			sink.ModelCall(runtime.ModelCall{RequestModel: "m", ParentToolID: "call-r"})
			return errors.Join(err, sink.ToolResult(runtime.ToolResult{ID: "call-r", Name: "Read", Status: runtime.ToolDeclined}), sink.ToolResult(agentResult))
		}, wantUnderTool: true},
		{name: "unknown tool", events: func(sink runtime.EventSink) error {
			err := sink.ToolCall(agentCall)
			sink.ModelCall(runtime.ModelCall{RequestModel: "m", ParentToolID: "call-x"})
			return errors.Join(err, sink.ToolResult(agentResult))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			executor := newExecutor(t, func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				return runtime.Outcome{}, tc.events(sink)
			}, tracing.RuntimeTelemetry{})
			for range executor.Execute(recorder.segment(t.Context()), requestContext("task-subagent", "go")) {
			}

			want := 3
			if tc.wantNoTool {
				want = 2
			}
			spans := spansByName(recorder.spans(t, want))
			child, tool, invoke := spans[cmp.Or(tc.child, "chat m")], spans["execute_tool Agent"], spans["invoke_agent"]
			wantParent := invoke.SpanContext.SpanID()
			if tc.wantUnderTool {
				wantParent = tool.SpanContext.SpanID()
			}
			if child.Parent.SpanID() != wantParent {
				t.Errorf("%s parent = %s, want %s (under tool %v)", child.Name, child.Parent.SpanID(), wantParent, tc.wantUnderTool)
			}
			if got, _ := attributeOf(tool, "error.type"); got.AsString() != tc.wantToolError {
				t.Errorf("tool error.type = %q, want %q", got.AsString(), tc.wantToolError)
			}
		})
	}
}

// A subagent's own subagent nests one level deeper, whatever order the spans end in.
func TestNestedSubagentSpansNestUnderTheirTool(t *testing.T) {
	outer := runtime.ToolCall{ID: "call-a", Name: "Agent"}
	inner := runtime.ToolCall{ID: "call-b", Name: "spawn", ParentToolID: "call-a"}
	end := func(id, name string) runtime.ToolResult { return runtime.ToolResult{ID: id, Name: name} }
	nestedCall := runtime.ModelCall{RequestModel: "m", ParentToolID: "call-b"}
	for _, tc := range []struct {
		name   string
		events func(sink runtime.EventSink) error
	}{
		{name: "call while both tools run", events: func(sink runtime.EventSink) error {
			err := errors.Join(sink.ToolCall(outer), sink.ToolCall(inner))
			sink.ModelCall(nestedCall)
			return errors.Join(err, sink.ToolResult(end("call-b", "spawn")), sink.ToolResult(end("call-a", "Agent")))
		}},
		{name: "call after the inner tool ends", events: func(sink runtime.EventSink) error {
			err := errors.Join(sink.ToolCall(outer), sink.ToolCall(inner), sink.ToolResult(end("call-b", "spawn")))
			sink.ModelCall(nestedCall)
			return errors.Join(err, sink.ToolResult(end("call-a", "Agent")))
		}},
		{name: "call while all tools stay open", events: func(sink runtime.EventSink) error {
			err := errors.Join(sink.ToolCall(outer), sink.ToolCall(inner))
			sink.ModelCall(nestedCall)
			return err
		}},
		{name: "call after both tools end", events: func(sink runtime.EventSink) error {
			err := errors.Join(sink.ToolCall(outer), sink.ToolCall(inner), sink.ToolResult(end("call-b", "spawn")), sink.ToolResult(end("call-a", "Agent")))
			sink.ModelCall(nestedCall)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := newTraceRecorder(t)
			executor := newExecutor(t, func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				return runtime.Outcome{}, tc.events(sink)
			}, tracing.RuntimeTelemetry{})
			for range executor.Execute(recorder.segment(t.Context()), requestContext("task-nested", "go")) {
			}

			spans := spansByName(recorder.spans(t, 4))
			chat, spawn, agent := spans["chat m"], spans["execute_tool spawn"], spans["execute_tool Agent"]
			if chat.Parent.SpanID() != spawn.SpanContext.SpanID() || spawn.Parent.SpanID() != agent.SpanContext.SpanID() {
				t.Errorf("chat parent %s, spawn parent %s; want chat under spawn %s under Agent %s",
					chat.Parent.SpanID(), spawn.Parent.SpanID(), spawn.SpanContext.SpanID(), agent.SpanContext.SpanID())
			}
		})
	}
}

// Three levels of open tools all nest when the turn ends without their results.
func TestUnfinishedSubagentToolsNestAtAnyDepth(t *testing.T) {
	recorder := newTraceRecorder(t)
	executor := newExecutor(t, func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{}, errors.Join(
			sink.ToolCall(runtime.ToolCall{ID: "call-a", Name: "a"}),
			sink.ToolCall(runtime.ToolCall{ID: "call-b", Name: "b", ParentToolID: "call-a"}),
			sink.ToolCall(runtime.ToolCall{ID: "call-c", Name: "c", ParentToolID: "call-b"}),
		)
	}, tracing.RuntimeTelemetry{})
	for range executor.Execute(recorder.segment(t.Context()), requestContext("task-deep", "go")) {
	}

	spans := spansByName(recorder.spans(t, 4))
	for child, parent := range map[string]string{"execute_tool c": "execute_tool b", "execute_tool b": "execute_tool a", "execute_tool a": "invoke_agent"} {
		if spans[child].Parent.SpanID() != spans[parent].SpanContext.SpanID() {
			t.Errorf("%s parent = %s, want %s", child, spans[child].Parent.SpanID(), parent)
		}
	}
}

// A subagent's spans go under its tool span across a pause, whether the tool is still open or ended.

func spansByName(spans tracetest.SpanStubs) map[string]tracetest.SpanStub {
	named := map[string]tracetest.SpanStub{}
	for _, span := range spans {
		named[span.Name] = span
	}
	return named
}

type tracedPendingTurn struct {
	*fakePendingTurn
	origin runtime.TraceOrigin
}

func (p *tracedPendingTurn) TraceOrigin() runtime.TraceOrigin { return p.origin }

func TestRestoredLegacyPendingHasNoOriginLink(t *testing.T) {
	recorder := newTraceRecorder(t)
	request := requestContext("task-legacy-trace", "write")
	message, err := inputRequiredMessage(request, &runtime.ApprovalRequest{ID: "1", CallID: "call-1", Name: "tools.write"})
	if err != nil {
		t.Fatal(err)
	}
	parked := &a2atype.TaskStatusUpdateEvent{Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: message}}
	for _, metadata := range []bool{false, true} {
		var pending runtime.PendingTurn = &fakePendingTurn{
			request: &runtime.ApprovalRequest{ID: "1", CallID: "call-1", Name: "tools.write"},
			resume: func(context.Context, runtime.InputResponse, runtime.EventSink) (runtime.Outcome, error) {
				return runtime.Outcome{}, nil
			},
		}
		if metadata {
			pending = &tracedPendingTurn{fakePendingTurn: pending.(*fakePendingTurn)}
		}
		executor, err := New(&restoringRunner{taskID: string(request.TaskID), contextID: request.ContextID, pending: pending}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
		if err != nil {
			t.Fatal(err)
		}
		_, errs := collect(executor.Execute(recorder.segment(t.Context()), approvalDecision(t, request, parked, "1")))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
	}
	for _, span := range recorder.spans(t, 2) {
		if len(span.Links) != 0 {
			t.Fatalf("legacy journal invented an origin link: %#v", span.Links)
		}
	}
}

func TestNewTaskGetsFreshTraceOrigin(t *testing.T) {
	recorder := newTraceRecorder(t)
	var origins []runtime.TraceOrigin
	executor, err := New(fakeRunner{run: func(_ context.Context, turn runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
		origins = append(origins, turn.Origin)
		return runtime.Outcome{}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"task-1", "task-2"} {
		_, errs := collect(executor.Execute(recorder.segment(t.Context()), requestContext(a2atype.TaskID(id), "hello")))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
	}
	spans := recorder.spans(t, 2)
	for i, origin := range origins {
		if !origin.SpanContext().Equal(spans[i].SpanContext) || len(spans[i].Links) != 0 {
			t.Fatalf("new task %d inherited an earlier origin", i)
		}
	}
	if origins[0].SpanContext().TraceID() == origins[1].SpanContext().TraceID() {
		t.Fatal("independent invocations share an origin")
	}
}

func TestRestoredSegmentsKeepTaskOriginAcrossPauses(t *testing.T) {
	recorder := newTraceRecorder(t)
	var saved runtime.TraceOrigin
	request := requestContext("task-restored-trace", "read then write")
	makePending := func(id string, again bool) *tracedPendingTurn {
		return &tracedPendingTurn{origin: saved, fakePendingTurn: &fakePendingTurn{
			request: &runtime.ApprovalRequest{ID: id, CallID: "call-" + id, Name: "tools.write"},
			resume: func(ctx context.Context, _ runtime.InputResponse, _ runtime.EventSink) (runtime.Outcome, error) {
				if trace.SpanContextFromContext(ctx).TraceID() == saved.SpanContext().TraceID() {
					t.Fatal("resume was reparented to the persisted origin")
				}
				if again {
					return runtime.Outcome{Pending: &fakePendingTurn{request: &runtime.ApprovalRequest{ID: "2", CallID: "call-2", Name: "tools.write"}}}, nil
				}
				return runtime.Outcome{}, nil
			},
		}}
	}
	executor, err := New(fakeRunner{run: func(_ context.Context, turn runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
		saved = turn.Origin
		return runtime.Outcome{Pending: makePending("1", true)}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}
	events, errs := collect(executor.Execute(recorder.segment(t.Context()), request))
	parked := parkedStatus(t, events, errs)
	for index, id := range []string{"1", "2"} {
		// DATA restores only serialized metadata, into a fresh executor and handle.
		data, err := json.Marshal(saved)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		runner := &restoringRunner{taskID: string(request.TaskID), contextID: request.ContextID, pending: makePending(id, index == 0)}
		executor, err = New(runner, &fakeContinuation{}, tracing.RuntimeTelemetry{})
		if err != nil {
			t.Fatal(err)
		}
		events, errs = collect(executor.Execute(recorder.segment(t.Context()), approvalDecision(t, request, parked, id)))
		if index == 0 {
			parked = parkedStatus(t, events, errs)
		} else if len(errs) != 0 {
			t.Fatalf("resume errors: %v", errs)
		}
	}
	spans := recorder.spans(t, 3)
	for _, span := range spans[1:] {
		if span.SpanContext.TraceID() == spans[0].SpanContext.TraceID() || len(span.Links) != 1 {
			t.Fatalf("expected separate trace with one origin link: %#v", span)
		}
		linked := span.Links[0].SpanContext
		if linked.TraceID() != spans[0].SpanContext.TraceID() || linked.SpanID() != spans[0].SpanContext.SpanID() {
			t.Fatalf("link does not point to initial invocation: %#v", span.Links)
		}
	}
}

func TestOpenToolsFinishAtDurableInputBoundary(t *testing.T) {
	recorder := newTraceRecorder(t)
	_, run := gatedPair(func(sink runtime.EventSink) error {
		if err := sink.ToolCall(runtime.ToolCall{ID: "other", Name: "Bash"}); err != nil {
			return err
		}
		return startGatedCall(sink)
	})
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
	events, errs := collect(executor.Execute(recorder.segment(t.Context()), requestContext("task-boundary", "go")))
	parkedStatus(t, events, errs)
	spans := recorder.spans(t, 2)
	if spans[0].Name != "execute_tool Bash" || stringAttribute(t, spans[0], "error.type") != "unfinished" || spans[0].Parent.SpanID() != spans[1].SpanContext.SpanID() {
		t.Fatalf("boundary spans = %+v", spans)
	}
}

func TestApprovedToolSpanAfterDataRestore(t *testing.T) {
	recorder := newTraceRecorder(t)
	request := requestContext("task-restored-tool", "write")
	approval := &runtime.ApprovalRequest{ID: "approval", CallID: "call", Name: "protected.write", Args: map[string]any{"value": 7}}
	var origin runtime.TraceOrigin
	makePending := func() *tracedPendingTurn {
		return &tracedPendingTurn{origin: origin, fakePendingTurn: &fakePendingTurn{
			request: approval,
			resume: func(_ context.Context, _ runtime.InputResponse, sink runtime.EventSink) (runtime.Outcome, error) {
				telemetry := sink.(runtime.ToolExecutionSink)
				telemetry.ToolExecutionStarted(runtime.ToolCall{ID: "call", Name: approval.Name, Arguments: approval.Args, MCPServer: "protected", MCPTool: "write"})
				result := runtime.ToolResult{ID: "call", Name: approval.Name, Result: "ok"}
				telemetry.ToolExecutionFinished(result)
				// The old subagent parent is absent after restore, so this call uses the current invocation.
				sink.ModelCall(runtime.ModelCall{RequestModel: "subagent", ParentToolID: "old-agent"})
				return runtime.Outcome{}, sink.ToolResult(result)
			},
		}}
	}
	executor := newExecutor(t, func(_ context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		origin = turn.Origin
		if err := sink.ToolCall(runtime.ToolCall{ID: "call", Name: approval.Name, Arguments: approval.Args}); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Outcome{Pending: makePending()}, nil
	}, tracing.RuntimeTelemetry{CaptureContent: true})
	events, errs := collect(executor.Execute(recorder.segment(t.Context()), request))
	parked := parkedStatus(t, events, errs)
	// Restore into a fresh executor, retaining only journal metadata.
	restored, err := New(&restoringRunner{taskID: string(request.TaskID), contextID: request.ContextID, pending: makePending()}, &fakeContinuation{}, tracing.RuntimeTelemetry{CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	events, errs = collect(restored.Execute(recorder.segment(t.Context()), approvalDecision(t, request, parked, approval.ID)))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	artifacts := 0
	for _, event := range events {
		if _, ok := event.(*a2atype.TaskArtifactUpdateEvent); ok {
			artifacts++
		}
	}
	if artifacts != 1 {
		t.Fatalf("restored execution emitted %d artifacts, want only its result", artifacts)
	}
	spans := recorder.spans(t, 4)
	initial, tool, chat, resumed := spans[0], spans[1], spans[2], spans[3]
	if tool.Name != "execute_tool write" || tool.Parent.SpanID() != resumed.SpanContext.SpanID() || tool.StartTime.Before(resumed.StartTime) {
		t.Fatalf("restored tool span = %+v", tool)
	}
	if stringAttribute(t, tool, "gen_ai.tool.call.arguments") != `{"value":7}` || stringAttribute(t, tool, "kagent.mcp.server.name") != "protected" {
		t.Fatalf("tool metadata = %+v", tool.Attributes)
	}
	if chat.Parent.SpanID() != resumed.SpanContext.SpanID() {
		t.Fatal("restored subagent call retained an old parent")
	}
	if len(resumed.Links) != 1 || resumed.Links[0].SpanContext.SpanID() != initial.SpanContext.SpanID() {
		t.Fatal("restored invocation lost its origin link")
	}
}
