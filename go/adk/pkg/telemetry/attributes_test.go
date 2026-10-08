package telemetry

import (
	"context"
	"strings"
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
func TestSetMessageMetadataAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")

	SetMessageMetadataAttributes(ctx, map[string]any{
		"approver_email": "admin@example.com",
		"attempt_count":  float64(3),
		"dry_run":        true,
		"nested":         map[string]any{"should": "be skipped"},
		"list_val":       []string{"also", "skipped"},
		"empty_str":      "",
	})
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans recorded")
	}
	attrs := make(map[string]attribute.Value)
	for _, a := range spans[0].Attributes {
		attrs[string(a.Key)] = a.Value
	}

	if got := attrs["a2a.message.metadata.approver_email"].AsString(); got != "admin@example.com" {
		t.Errorf("approver_email: got %q, want %q", got, "admin@example.com")
	}
	if got := attrs["a2a.message.metadata.attempt_count"].AsString(); got != "3" {
		t.Errorf("attempt_count: got %q, want %q", got, "3")
	}
	if got := attrs["a2a.message.metadata.dry_run"].AsBool(); !got {
		t.Errorf("dry_run: got %v, want true", got)
	}
	if _, exists := attrs["a2a.message.metadata.nested"]; exists {
		t.Error("nested map should not be set as span attribute")
	}
	if _, exists := attrs["a2a.message.metadata.list_val"]; exists {
		t.Error("list value should not be set as span attribute")
	}
	if _, exists := attrs["a2a.message.metadata.empty_str"]; exists {
		t.Error("empty string should not be set as span attribute")
	}
}

func TestSetMessageMetadataAttributes_SkipsAllowlistedSources(t *testing.T) {
	setAllowlist(t, `[{"from":"sub","to":"user.id"}]`)

	attrs := recordMessageMetadata(t, map[string]any{
		"sub":     "opaque-subject",
		"channel": "C0AB1",
	})

	if _, exists := attrs["a2a.message.metadata.sub"]; exists {
		t.Fatal("allowlisted source must not be stamped as a2a.message.metadata.sub")
	}
	if got := attrs["a2a.message.metadata.channel"].AsString(); got != "C0AB1" {
		t.Errorf("non-allowlisted channel = %q, want C0AB1", got)
	}
}

// The executor stamps leftover A2A metadata via SetMessageMetadataAttributes.
// A rejected leftover {hash:...} mapping must still suppress its source so
// the raw value cannot appear as a2a.message.metadata.<from>.
func TestSetMessageMetadataAttributes_SkipsRejectedHashSources(t *testing.T) {
	setAllowlist(t, `[{"from":"email","to":"user.hash","hash":"hmac-sha256"}]`)

	attrs := recordMessageMetadata(t, map[string]any{
		"email":   "person@example.test",
		"channel": "C0AB1",
	})

	if _, exists := attrs["a2a.message.metadata.email"]; exists {
		t.Fatal("rejected hash mapping source must not be stamped as a2a.message.metadata.email")
	}
	if _, exists := attrs["user.hash"]; exists {
		t.Fatal("rejected hash mappings must not emit user.hash")
	}
	if got := attrs["a2a.message.metadata.channel"].AsString(); got != "C0AB1" {
		t.Errorf("non-allowlisted channel = %q, want C0AB1", got)
	}
}

func TestSetMessageMetadataAttributes_SkipsInvalidMappingSources(t *testing.T) {
	setAllowlist(t, `[{"from":"email","to":"bad key"}]`)

	attrs := recordMessageMetadata(t, map[string]any{
		"email":   "person@example.test",
		"channel": "C0AB1",
	})

	if _, exists := attrs["a2a.message.metadata.email"]; exists {
		t.Fatal("invalid mapping source must not be stamped as a2a.message.metadata.email")
	}
	if got := attrs["a2a.message.metadata.channel"].AsString(); got != "C0AB1" {
		t.Errorf("non-allowlisted channel = %q, want C0AB1", got)
	}
}

func recordMessageMetadata(t *testing.T, metadata map[string]any) map[string]attribute.Value {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	SetMessageMetadataAttributes(ctx, metadata)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans recorded")
	}
	attrs := make(map[string]attribute.Value)
	for _, a := range spans[0].Attributes {
		attrs[string(a.Key)] = a.Value
	}
	return attrs
}

func TestSetMessageMetadataAttributes_NilAndEmpty(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")

	SetMessageMetadataAttributes(ctx, nil)
	SetMessageMetadataAttributes(ctx, map[string]any{})
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans recorded")
	}
	for _, a := range spans[0].Attributes {
		if strings.HasPrefix(string(a.Key), "a2a.message.metadata.") {
			t.Errorf("no metadata attributes expected, got %q", a.Key)
		}
	}
}

func spanAttributesByName(t *testing.T, spans tracetest.SpanStubs, name string) map[string]attribute.Value {
	t.Helper()

	for _, span := range spans {
		if span.Name != name {
			continue
		}
		attrs := make(map[string]attribute.Value, len(span.Attributes))
		for _, attr := range span.Attributes {
			attrs[string(attr.Key)] = attr.Value
		}
		return attrs
	}

	t.Fatalf("span %q not found", name)
	return nil
}
