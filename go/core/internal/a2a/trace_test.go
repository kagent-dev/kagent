package a2a

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"k8s.io/apimachinery/pkg/types"
)

func TestA2ATracingMiddleware_SetsGenAIAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	agentRef := types.NamespacedName{Namespace: "default", Name: "my-agent"}
	mw := newA2ATracingMiddleware(agentRef, semconv.GenAIProviderNameOpenAI)

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rr := httptest.NewRecorder()
	mw.Wrap(inner).ServeHTTP(rr, req)

	if !called {
		t.Fatal("inner handler was not called")
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	wantAttrs := map[string]string{
		"gen_ai.operation.name": "invoke_agent",
		"gen_ai.provider.name":  "openai",
		"gen_ai.agent.name":     "my-agent",
		"gen_ai.agent.id":       "default/my-agent",
	}
	gotAttrs := make(map[string]string)
	for _, a := range spans[0].Attributes {
		gotAttrs[string(a.Key)] = a.Value.AsString()
	}
	for k, want := range wantAttrs {
		if got := gotAttrs[k]; got != want {
			t.Errorf("attribute %s: want %q, got %q", k, want, got)
		}
	}

	if spans[0].Name != "invoke_agent" {
		t.Errorf("span name: want %q, got %q", "invoke_agent", spans[0].Name)
	}
}
