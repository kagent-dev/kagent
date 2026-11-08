package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestCreateTransport_InjectsTraceContext verifies that the HTTP client behind
// an HTTP MCP transport carries the W3C traceparent of the active span, so MCP
// calls stay attached to the invocation trace.
func TestCreateTransport_InjectsTraceContext(t *testing.T) {
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

	var gotTraceparent, gotStatic string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		gotStatic = r.Header.Get("X-Static")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport, err := createTransport(t.Context(), mcpServerParams{
		URL:        srv.URL,
		ServerType: "http",
		Headers:    map[string]string{"X-Static": "yes"},
	})
	if err != nil {
		t.Fatalf("createTransport: %v", err)
	}
	streamable, ok := transport.(*mcpsdk.StreamableClientTransport)
	if !ok {
		t.Fatalf("transport = %T, want *mcpsdk.StreamableClientTransport", transport)
	}

	ctx, span := tp.Tracer("test").Start(t.Context(), "execute_tool")
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := streamable.HTTPClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	if gotStatic != "yes" {
		t.Fatalf("static header lost through propagation layer: %q", gotStatic)
	}
	wantTraceID := span.SpanContext().TraceID().String()
	if !strings.Contains(gotTraceparent, wantTraceID) {
		t.Fatalf("traceparent %q does not carry parent trace id %s", gotTraceparent, wantTraceID)
	}
}
