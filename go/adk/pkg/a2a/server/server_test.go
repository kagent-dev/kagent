package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
)

// substrateExecutor mimics KAgentExecutor's telemetry: it starts the
// invocation span from the request-derived context. It does not flush —
// exporting everything (including the otelhttp server span, still open
// until the mux handler returns) is the server's flushing handler's job.
type substrateExecutor struct{}

func (substrateExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	_, span := telemetry.StartInvocationSpan(ctx)
	span.End()

	msg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: "done"})
	msg.ContextID = reqCtx.ContextID
	msg.TaskID = reqCtx.TaskID
	return queue.Write(ctx, msg)
}

func (substrateExecutor) Cancel(context.Context, *a2asrv.RequestContext, eventqueue.Queue) error {
	return nil
}

// The A2A server must export every span of a request's trace — including the
// otelhttp server span, which only ends after the inner handler returns —
// before the response body closes: Agent Substrate checkpoints the actor at
// body close, freezing any still-buffered spans into the snapshot. ServeHTTP
// returning is the last instant before net/http closes the body, so
// everything must be exported by then.
func TestSpansExportedBeforeResponseBodyCloses(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter)))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, logr.Discard(), ServerConfig{Port: "0"})
	if err != nil {
		t.Fatalf("NewA2AServer: %v", err)
	}

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "message/send",
		"params": &a2atype.MessageSendParams{
			Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.TextPart{Text: "hi"}),
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}

	exported := map[string]bool{}
	for _, s := range exporter.GetSpans() {
		exported[s.Name] = true
	}
	if !exported["invocation"] {
		t.Errorf("invocation span not exported before body close, got %v", exported)
	}
	if !exported["POST /"] {
		t.Errorf("server span not exported before body close, got %v", exported)
	}
}
