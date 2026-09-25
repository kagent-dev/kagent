package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRequestAttributesReachSpansStartedBeneath(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(requestAttributesSpanProcessor{}),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracer := tp.Tracer("test")
	ctx, parent := tracer.Start(t.Context(), "parent")
	ctx = WithRequestAttributes(ctx,
		attribute.String("gen_ai.conversation.id", "conversation-1"),
		attribute.String("a2a.task.id", "task-1"),
	)
	_, child := tracer.Start(ctx, "child")
	child.End()
	parent.End()

	attributes := map[string]map[string]string{}
	for _, span := range exporter.GetSpans() {
		attributes[span.Name] = map[string]string{}
		for _, attr := range span.Attributes {
			attributes[span.Name][string(attr.Key)] = attr.Value.String()
		}
	}
	if got := attributes["child"]; got["gen_ai.conversation.id"] != "conversation-1" || got["a2a.task.id"] != "task-1" {
		t.Fatalf("child attributes = %v", got)
	}
	if got := attributes["parent"]; len(got) != 0 {
		t.Fatalf("a span started before the attributes were set got %v", got)
	}
}

func TestWithRequestAttributesKeepsContextWhenEmpty(t *testing.T) {
	ctx := t.Context()
	if WithRequestAttributes(ctx) != ctx {
		t.Fatal("no attributes should return the same context")
	}
}
