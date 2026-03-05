package a2a

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/types"
)

// mockHTTPReqHandler captures the request passed to Handle for inspection.
type mockHTTPReqHandler struct {
	capturedReq *http.Request
}

func (m *mockHTTPReqHandler) Handle(_ context.Context, _ *http.Client, req *http.Request) (*http.Response, error) {
	m.capturedReq = req
	return &http.Response{StatusCode: http.StatusOK}, nil
}

func TestTraceInjectHandler_InjectsHeader(t *testing.T) {
	const rawTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	ctx := propagation.TraceContext{}.Extract(
		context.Background(),
		propagation.MapCarrier{"traceparent": rawTraceparent},
	)

	mock := &mockHTTPReqHandler{}
	h := &traceInjectHandler{next: mock}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	if _, err := h.Handle(ctx, nil, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := mock.capturedReq.Header.Get("traceparent")
	if got == "" {
		t.Fatal("expected traceparent header on outgoing request, got none")
	}

	// The injected header must carry the same trace ID as the incoming context.
	outCtx := propagation.TraceContext{}.Extract(context.Background(), propagation.HeaderCarrier(mock.capturedReq.Header))
	wantTraceID := trace.SpanContextFromContext(ctx).TraceID()
	gotTraceID := trace.SpanContextFromContext(outCtx).TraceID()
	if wantTraceID != gotTraceID {
		t.Errorf("trace ID: want %s, got %s", wantTraceID, gotTraceID)
	}
}

func TestTraceInjectHandler_NoHeaderWhenNoTrace(t *testing.T) {
	mock := &mockHTTPReqHandler{}
	h := &traceInjectHandler{next: mock}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	if _, err := h.Handle(context.Background(), nil, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := mock.capturedReq.Header.Get("traceparent"); got != "" {
		t.Errorf("expected no traceparent header, got %q", got)
	}
}

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
