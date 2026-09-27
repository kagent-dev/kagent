package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func recordingTracer(t *testing.T) (trace.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider.Tracer("test"), exporter
}

func TestNilInvocationIsUsable(t *testing.T) {
	var invocation *Invocation
	invocation.Adopt()
	invocation.SetAttributes(attribute.String("k", "v"))
	invocation.AddLink(trace.Link{})
	if invocation.IsRecording() || invocation.SpanContext().IsValid() {
		t.Fatal("nil invocation reported a live span")
	}
	if ended, err := invocation.End(t.Context(), Result{}); ended || err != nil {
		t.Fatalf("End() on a nil invocation = %v, %v", ended, err)
	}
}

func TestInvocationEndsExactlyOnce(t *testing.T) {
	tracer, exporter := recordingTracer(t)
	ctx, invocation := StartInvocation(t.Context(), tracer, "invoke_agent", nil,
		attribute.String(AttributeRuntime, "codex"))

	ended, err := invocation.End(ctx, Result{TaskState: "TASK_STATE_COMPLETED"})
	if !ended || err != nil {
		t.Fatalf("first End() = %v, %v", ended, err)
	}
	if ended, _ := invocation.End(ctx, Result{TaskState: "TASK_STATE_FAILED"}); ended {
		t.Fatal("second End() ended the invocation again")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("exported %d spans, want exactly one", len(spans))
	}
	if got := attributeValue(spans[0].Attributes, AttributeTaskState); got != "TASK_STATE_COMPLETED" {
		t.Fatalf("task state = %q, want the first outcome", got)
	}
}

func TestInvocationFlushesOnceAfterEnding(t *testing.T) {
	tracer, exporter := recordingTracer(t)
	flushes := 0
	flush := func(context.Context) error {
		if len(exporter.GetSpans()) != 1 {
			t.Error("flush ran before the invocation span ended")
		}
		flushes++
		return nil
	}
	ctx, invocation := StartInvocation(t.Context(), tracer, "invoke_agent", flush)
	_, _ = invocation.End(ctx, Result{TaskState: "TASK_STATE_COMPLETED"})
	_, _ = invocation.End(ctx, Result{TaskState: "TASK_STATE_FAILED"})
	if flushes != 1 {
		t.Fatalf("flushed %d times, want once", flushes)
	}
}

func TestAdoptedInvocationIgnoresTheTransport(t *testing.T) {
	tracer, exporter := recordingTracer(t)
	ctx, invocation := StartInvocation(t.Context(), tracer, "invoke_agent", nil)
	invocation.Adopt()

	if ended, _ := invocation.EndTransport(ctx, Result{TaskState: "TASK_STATE_COMPLETED"}); ended {
		t.Fatal("transport ended an invocation execution had adopted")
	}
	if len(exporter.GetSpans()) != 0 {
		t.Fatal("adopted invocation was exported before execution finished")
	}
	if ended, _ := invocation.End(ctx, Result{TaskState: "TASK_STATE_FAILED", Error: "runtime_error"}); !ended {
		t.Fatal("execution could not end the invocation it adopted")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("exported %d spans, want exactly one", len(spans))
	}
	if spans[0].Status.Code != codes.Error || spans[0].Status.Description != "runtime_error" {
		t.Fatalf("status = %v %q, want an error status carrying the safe category", spans[0].Status.Code, spans[0].Status.Description)
	}
}

func TestTransportEndsUnadoptedInvocation(t *testing.T) {
	tracer, exporter := recordingTracer(t)
	ctx, invocation := StartInvocation(t.Context(), tracer, "invoke_agent", nil)

	if ended, _ := invocation.EndTransport(ctx, Result{Error: "transport_error"}); !ended {
		t.Fatal("transport could not end an invocation it still owned")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error {
		t.Fatalf("spans = %#v, want one span with an error status", spans)
	}
}

func TestInvocationRecordsDispositionAndLinks(t *testing.T) {
	tracer, exporter := recordingTracer(t)
	originCtx, origin := StartInvocation(t.Context(), tracer, "invoke_agent", nil)
	originContext := origin.SpanContext()
	if _, err := origin.End(originCtx, Result{TaskState: "TASK_STATE_INPUT_REQUIRED"}); err != nil {
		t.Fatal(err)
	}

	ctx, invocation := StartInvocation(t.Context(), tracer, "invoke_agent", nil)
	invocation.AddLink(trace.Link{
		SpanContext: originContext,
		Attributes:  []attribute.KeyValue{attribute.String(AttributeLinkRelationship, RelationshipResumeOrigin)},
	})
	if _, err := invocation.End(ctx, Result{Disposition: DispositionAbandoned}); err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("exported %d spans, want two", len(spans))
	}
	resumed := spans[1]
	if got := attributeValue(resumed.Attributes, AttributeDisposition); got != DispositionAbandoned {
		t.Fatalf("disposition = %q, want %q", got, DispositionAbandoned)
	}
	if len(resumed.Links) != 1 || resumed.Links[0].SpanContext.SpanID() != originContext.SpanID() {
		t.Fatalf("links = %#v, want one link to the origin span", resumed.Links)
	}
	if resumed.Parent.IsValid() {
		t.Fatal("a link must not reparent the resumed segment")
	}
}

func TestFinishedInvocationIgnoresLateWrites(t *testing.T) {
	tracer, exporter := recordingTracer(t)
	ctx, invocation := StartInvocation(t.Context(), tracer, "invoke_agent", nil)
	if _, err := invocation.End(ctx, Result{}); err != nil {
		t.Fatal(err)
	}
	invocation.SetAttributes(attribute.String(AttributeOutputMessages, "late"))

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("exported %d spans, want one", len(spans))
	}
	if got := attributeValue(spans[0].Attributes, AttributeOutputMessages); got != "" {
		t.Fatalf("output = %q, want nothing recorded after completion", got)
	}
}

func attributeValue(attributes []attribute.KeyValue, key string) string {
	for _, attr := range attributes {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

// a2a-go dispatches execution before the caller's subscription is read, so a
// caller that disconnects in that window completes the invocation from the
// transport side. Execution must be told, rather than writing into a span that
// can no longer record any of it.
func TestAdoptRefusesAFinishedInvocation(t *testing.T) {
	tracer, exporter := recordingTracer(t)
	ctx, invocation := StartInvocation(t.Context(), tracer, "invoke_agent", nil)
	if !invocation.Adopt() {
		t.Fatal("Adopt() refused a live invocation")
	}

	_, transport := StartInvocation(t.Context(), tracer, "invoke_agent", nil)
	if _, err := transport.EndTransport(ctx, Result{Error: "transport_error"}); err != nil {
		t.Fatal(err)
	}
	if transport.Adopt() {
		t.Fatal("Adopt() accepted an invocation the transport already completed")
	}
	if ended, _ := transport.End(ctx, Result{TaskState: "TASK_STATE_COMPLETED"}); ended {
		t.Fatal("End() reopened a completed invocation")
	}
	if len(exporter.GetSpans()) != 1 {
		t.Fatalf("exported %d spans, want only the transport's", len(exporter.GetSpans()))
	}
}
