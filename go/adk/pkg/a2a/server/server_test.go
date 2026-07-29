package server

import (
	"bytes"
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
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

func (substrateExecutor) Execute(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		_, span := telemetry.StartInvocationSpan(ctx)
		defer span.End()

		if !yield(a2atype.NewSubmittedTask(reqCtx, reqCtx.Message), nil) {
			return
		}
		if !yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil), nil) {
			return
		}

		msg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("done"))
		msg.ContextID = reqCtx.ContextID
		msg.TaskID = reqCtx.TaskID
		yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, msg), nil)
	}
}

func (substrateExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

// runA2ARequest builds a server against an in-memory batch exporter, serves
// one message/send, and returns the span names exported by the time ServeHTTP
// returned — the last instant before net/http closes the response body (on
// Agent Substrate, the checkpoint instant).
func runA2ARequest(t *testing.T) map[string]bool {
	t.Helper()

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
		"method":  "SendMessage",
		"params": &a2atype.SendMessageRequest{
			Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(a2atype.SvcParamVersion, string(a2atype.Version))
	rec := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}

	exported := map[string]bool{}
	for _, s := range exporter.GetSpans() {
		exported[s.Name] = true
	}
	return exported
}

// With KAGENT_PRE_RESPONSE_TRACE_FLUSH (set by the controller on Agent
// Substrate actors), every span of a request's trace — including the otelhttp
// server span, which only ends after the inner handler returns — must be
// exported before the response body closes: substrate checkpoints the actor at
// body close, freezing any still-buffered spans into the snapshot.
func TestSpansExportedBeforeResponseBodyCloses(t *testing.T) {
	t.Setenv("KAGENT_PRE_RESPONSE_TRACE_FLUSH", "true")

	exported := runA2ARequest(t)
	if !exported["invocation"] {
		t.Errorf("invocation span not exported before body close, got %v", exported)
	}
	if !exported["POST /"] {
		t.Errorf("server span not exported before body close, got %v", exported)
	}
}

// Without the opt-in, spans stay in the batch processor for its timer to
// export — no per-request flush.
func TestNoPreResponseFlushByDefault(t *testing.T) {
	exported := runA2ARequest(t)
	if len(exported) != 0 {
		t.Errorf("spans exported at handler return without opt-in, got %v", exported)
	}
}

func TestA2ARequestSizeLimit(t *testing.T) {
	tests := []struct {
		name          string
		contentLength int64
		wantStatus    int
		wantBody      string
	}{
		{
			name:          "declared content length",
			contentLength: 6,
			wantStatus:    http.StatusRequestEntityTooLarge,
			wantBody:      "Payload too large",
		},
		{
			name:          "unknown content length",
			contentLength: -1,
			wantStatus:    http.StatusOK,
			wantBody:      "request body too large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(a2aMaxContentLengthEnvVar, "5")
			srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, logr.Discard(), ServerConfig{Port: "0"})
			if err != nil {
				t.Fatalf("NewA2AServer: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("123456"))
			req.ContentLength = tt.contentLength
			rec := httptest.NewRecorder()

			srv.httpServer.Handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("unexpected status %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("response body %q does not contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestA2ARequestSizeLimitDisabled(t *testing.T) {
	t.Setenv(a2aMaxContentLengthEnvVar, "unlimited")
	srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, logr.Discard(), ServerConfig{Port: "0"})
	if err != nil {
		t.Fatalf("NewA2AServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("123456"))
	rec := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("request size limit was not disabled: %s", rec.Body.String())
	}
}

func TestRequestSizeLimitPreservesFlusher(t *testing.T) {
	flusherAvailable := false
	handler := withRequestSizeLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, flusherAvailable = w.(http.Flusher)
		w.WriteHeader(http.StatusNoContent)
	}), 5)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !flusherAvailable {
		t.Error("request size limit did not preserve http.Flusher")
	}
}

func TestGetMaxContentLength(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      int64
		unlimited bool
	}{
		{name: "positive integer", value: "1024", want: 1024},
		{name: "whitespace", value: " 1024 ", want: 1024},
		{name: "zero", value: "0", unlimited: true},
		{name: "none", value: "none", unlimited: true},
		{name: "unlimited", value: "unlimited", unlimited: true},
		{name: "invalid", value: "invalid", want: defaultMaxContentLength},
		{name: "negative", value: "-1", want: defaultMaxContentLength},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(a2aMaxContentLengthEnvVar, tt.value)
			got := getMaxContentLength(logr.Discard())
			if tt.unlimited {
				if got != nil {
					t.Errorf("expected unlimited request size, got %d", *got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected request size limit, got unlimited")
			}
			if *got != tt.want {
				t.Errorf("unexpected request size limit %d, want %d", *got, tt.want)
			}
		})
	}
}
