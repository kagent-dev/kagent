package telemetry

import (
	"context"
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
