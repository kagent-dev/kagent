package telemetry

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestSetKAgentSpanAttributes_PropagatesToChildSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(kagentAttributesSpanProcessor{}),
	)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("test")
	ctx, root := tracer.Start(context.Background(), "root")
	ctx = SetKAgentSpanAttributes(ctx, map[string]string{
		"kagent.user_id":         "user-123",
		"gen_ai.task.id":         "task-456",
		"gen_ai.conversation.id": "conversation-789",
	})
	_, child := tracer.Start(ctx, "child")
	child.End()
	root.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	rootAttrs := spanAttributesByName(t, spans, "root")
	childAttrs := spanAttributesByName(t, spans, "child")

	for _, attrs := range []map[string]attribute.Value{rootAttrs, childAttrs} {
		if got := attrs["kagent.user_id"].AsString(); got != "user-123" {
			t.Errorf("kagent.user_id = %q, want %q", got, "user-123")
		}
		if got := attrs["gen_ai.task.id"].AsString(); got != "task-456" {
			t.Errorf("gen_ai.task.id = %q, want %q", got, "task-456")
		}
		if got := attrs["gen_ai.conversation.id"].AsString(); got != "conversation-789" {
			t.Errorf("gen_ai.conversation.id = %q, want %q", got, "conversation-789")
		}
	}
}

func TestStartInvocationSpan_InheritsContextAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(kagentAttributesSpanProcessor{}),
	)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	prevProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prevProvider)
	})

	rootTracer := tp.Tracer("test")
	ctx, root := rootTracer.Start(context.Background(), "root")
	ctx = SetKAgentSpanAttributes(ctx, map[string]string{
		"kagent.user_id":         "user-123",
		"gen_ai.conversation.id": "conversation-789",
	})

	_, invocation := StartInvocationSpan(ctx)
	invocation.End()
	root.End()

	attrs := spanAttributesByName(t, exporter.GetSpans(), "invocation")
	if got := attrs["kagent.user_id"].AsString(); got != "user-123" {
		t.Errorf("kagent.user_id = %q, want %q", got, "user-123")
	}
	if got := attrs["gen_ai.conversation.id"].AsString(); got != "conversation-789" {
		t.Errorf("gen_ai.conversation.id = %q, want %q", got, "conversation-789")
	}
}

func TestSetLLMAttributes_OnActiveSpan(t *testing.T) {
	t.Setenv(captureMessageContentEnvVar, "true")

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "generate_content gpt-4.1-mini")

	req := &model.LLMRequest{
		Model: "gpt-4.1-mini",
		Contents: []*genai.Content{{
			Role: string(genai.RoleUser),
			Parts: []*genai.Part{
				{Text: "Hello"},
			},
		}},
	}
	SetLLMRequestAttributes(ctx, "gpt-4.1-mini", req)

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{
				{Text: "Hi there"},
			},
		},
	}
	SetLLMResponseAttributes(ctx, resp)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	attrs := make(map[string]attribute.Value, len(spans[0].Attributes))
	for _, attr := range spans[0].Attributes {
		attrs[string(attr.Key)] = attr.Value
	}

	if got := attrs["gcp.vertex.agent.llm_request"].AsString(); got == "" || got == "{}" {
		t.Errorf("gcp.vertex.agent.llm_request = %q, want captured payload", got)
	}
	if got := attrs["gcp.vertex.agent.llm_response"].AsString(); got == "" || got == "{}" {
		t.Errorf("gcp.vertex.agent.llm_response = %q, want captured payload", got)
	}
}

func TestSetLLMAttributes_EmitsEmptyPayloadWhenContentCaptureDisabled(t *testing.T) {
	t.Setenv(captureMessageContentEnvVar, "false")

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "generate_content")
	SetLLMRequestAttributes(ctx, "gpt-4.1-mini", &model.LLMRequest{Model: "gpt-4.1-mini"})
	SetLLMResponseAttributes(ctx, &model.LLMResponse{})
	span.End()

	attrs := make(map[string]attribute.Value)
	for _, attr := range exporter.GetSpans()[0].Attributes {
		attrs[string(attr.Key)] = attr.Value
	}

	if got := attrs["gcp.vertex.agent.llm_request"].AsString(); got != "{}" {
		t.Errorf("gcp.vertex.agent.llm_request = %q, want %q", got, "{}")
	}
	if got := attrs["gcp.vertex.agent.llm_response"].AsString(); got != "{}" {
		t.Errorf("gcp.vertex.agent.llm_response = %q, want %q", got, "{}")
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
