package models

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestBuildHTTPClient_InjectsTraceContext verifies that requests issued through
// a BuildHTTPClient client carry the W3C traceparent of the span active in the
// request context, so LLM calls stay attached to the invocation trace.
func TestBuildHTTPClient_InjectsTraceContext(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })
	prevProvider := otel.GetTracerProvider()
	prevPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevProvider)
		otel.SetTextMapPropagator(prevPropagator)
	})

	var gotTraceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client, err := BuildHTTPClient(TransportConfig{Headers: map[string]string{"X-Custom": "1"}})
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}

	ctx, span := tp.Tracer("test").Start(t.Context(), "generate_content")
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	wantTraceID := span.SpanContext().TraceID().String()
	if gotTraceparent == "" {
		t.Fatal("expected traceparent header on outbound request, got none")
	}
	if !strings.Contains(gotTraceparent, wantTraceID) {
		t.Fatalf("traceparent %q does not carry parent trace id %s", gotTraceparent, wantTraceID)
	}
}
