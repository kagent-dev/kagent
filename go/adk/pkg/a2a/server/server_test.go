package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"log/slog"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
)

func TestMetricsEndpoint(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  bool
	}{
		{name: "disabled"},
		{name: "enabled", input: "true", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OTEL_METRICS_ENABLED", test.input)
			t.Setenv("KAGENT_PRE_RESPONSE_TRACE_FLUSH", "true")
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() {
				otel.SetTracerProvider(previous)
				require.NoError(t, provider.Shutdown(context.Background()))
			})
			testServer, _ := startTestServer(t)
			telemetry.RecordTokenUsage(telemetry.TokenUsage{RequestModel: "test-model", Provider: "openai", InputTokens: 10})
			response, err := testServer.Client().Get(testServer.URL + "/metrics")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, response.Body.Close()) })
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			if test.want {
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.Contains(t, string(body), "gen_ai_client_token_usage_count")
			} else {
				require.NotContains(t, string(body), "gen_ai_client_token_usage")
			}
			require.Empty(t, exporter.GetSpans())
		})
	}
}

// substrateExecutor mimics KAgentExecutor's telemetry: it starts the
// invocation span from the request-derived context. It does not flush —
// exporting everything (including the otelhttp server span, still open
// until the mux handler returns) is the server's flushing handler's job.
type substrateExecutor struct{ finalState a2atype.TaskState }

func (e substrateExecutor) Execute(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
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
		state := e.finalState
		if state == a2atype.TaskStateUnspecified {
			state = a2atype.TaskStateCompleted
		}
		yield(a2atype.NewStatusUpdateEvent(reqCtx, state, msg), nil)
	}
}

func (substrateExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

func startTestServer(t *testing.T) (*httptest.Server, *grpc.ClientConn) {
	t.Helper()

	srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, slog.New(slog.DiscardHandler), ServerConfig{Port: "0"})
	if err != nil {
		t.Fatalf("NewA2AServer: %v", err)
	}

	testServer := httptest.NewUnstartedServer(srv.httpServer.Handler)
	testServer.Config.Protocols = srv.httpServer.Protocols
	testServer.Start()
	conn, err := grpc.NewClient(testServer.Listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("create gRPC client: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.grpcServer.Stop()
		testServer.Close()
	})
	return testServer, conn
}

func TestHTTPAndGRPCHealthSharePort(t *testing.T) {
	testServer, conn := startTestServer(t)

	resp, err := testServer.Client().Get(testServer.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	health, err := grpc_health_v1.NewHealthClient(conn).Check(t.Context(), &grpc_health_v1.HealthCheckRequest{
		Service: a2apb.A2AService_ServiceDesc.ServiceName,
	})
	if err != nil {
		t.Fatalf("gRPC health check: %v", err)
	}
	if health.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("gRPC health status = %s, want SERVING", health.GetStatus())
	}
}

func TestGRPCAndJSONRPCShareRequestHandler(t *testing.T) {
	testServer, conn := startTestServer(t)
	client := a2apb.NewA2AServiceClient(conn)

	pbReq, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	})
	if err != nil {
		t.Fatalf("convert request: %v", err)
	}
	result, err := client.SendMessage(t.Context(), pbReq)
	if err != nil {
		t.Fatalf("gRPC SendMessage: %v", err)
	}
	task := result.GetTask()
	if task == nil {
		t.Fatal("gRPC SendMessage did not return a task")
	}

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "GetTask",
		"params":  &a2atype.GetTaskRequest{ID: a2atype.TaskID(task.GetId())},
	})
	if err != nil {
		t.Fatalf("marshal GetTask: %v", err)
	}
	httpResp, err := testServer.Client().Post(testServer.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("JSON-RPC GetTask: %v", err)
	}
	defer httpResp.Body.Close()
	var getTaskResp struct {
		Result a2atype.Task `json:"result"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&getTaskResp); err != nil {
		t.Fatalf("decode GetTask response: %v", err)
	}
	if getTaskResp.Result.ID != a2atype.TaskID(task.GetId()) {
		t.Errorf("JSON-RPC task ID = %q, want %q", getTaskResp.Result.ID, task.GetId())
	}

	stream, err := client.SendStreamingMessage(t.Context(), pbReq)
	if err != nil {
		t.Fatalf("gRPC SendStreamingMessage: %v", err)
	}
	var sawTask, sawStatus bool
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("receive streaming response: %v", err)
		}
		sawTask = sawTask || event.GetTask() != nil
		sawStatus = sawStatus || event.GetStatusUpdate() != nil
	}
	if !sawTask || !sawStatus {
		t.Errorf("stream responses missing task or status update: sawTask=%t sawStatus=%t", sawTask, sawStatus)
	}
}

// runA2ARequest builds a server against an in-memory batch exporter and serves
// one message/send.
func runA2ARequest(t *testing.T, flush bool) tracetest.SpanStubs {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter)))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	config := ServerConfig{Port: "0"}
	if flush {
		config.Flush = tp.ForceFlush
	}
	srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, slog.New(slog.DiscardHandler), config)
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

	return exporter.GetSpans()
}

// The interceptor-owned request span and its invocation descendants are ended
// and flushed at the quiescent event. The otelhttp span remains open until its
// handler returns so it can retain response attributes.
func TestSpansExportedBeforeResponseBodyCloses(t *testing.T) {
	spans := runA2ARequest(t, true)
	exported := map[string]tracetest.SpanStub{}
	for _, span := range spans {
		exported[span.Name] = span
	}
	if _, ok := exported["invocation"]; !ok {
		t.Errorf("invocation span not exported before body close, got %v", exported)
	}
	if _, ok := exported["a2a.request"]; !ok {
		t.Errorf("A2A request span not exported before body close, got %v", exported)
	}
	requestSpan := exported["a2a.request"]
	httpSpan, ok := exported["POST /"]
	if !ok {
		t.Errorf("HTTP server span not exported after handler return, got %v", exported)
		return
	}
	var statusCode int64
	for _, attr := range httpSpan.Attributes {
		if attr.Key == "http.response.status_code" {
			statusCode = attr.Value.AsInt64()
		}
	}
	if statusCode != http.StatusOK {
		t.Errorf("HTTP server span response status = %d, want %d", statusCode, http.StatusOK)
	}
	if requestSpan.Parent.SpanID() != httpSpan.SpanContext.SpanID() {
		t.Errorf("A2A request parent = %s, want HTTP span %s", requestSpan.Parent.SpanID(), httpSpan.SpanContext.SpanID())
	}
	if invocation := exported["invocation"]; invocation.Parent.SpanID() != requestSpan.SpanContext.SpanID() {
		t.Errorf("invocation parent = %s, want A2A request span %s", invocation.Parent.SpanID(), requestSpan.SpanContext.SpanID())
	}
}

func TestNoPreResponseFlushWithoutFlusher(t *testing.T) {
	spans := runA2ARequest(t, false)
	if len(spans) != 0 {
		t.Errorf("spans exported at handler return without a flusher, got %v", spans)
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
			srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, slog.New(slog.DiscardHandler), ServerConfig{Port: "0"})
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
	srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, slog.New(slog.DiscardHandler), ServerConfig{Port: "0"})
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
			got := getMaxContentLength(slog.New(slog.DiscardHandler))
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

// This interceptor observes the last boundary before an event is handed to the
// transport. The controller can suspend the actor as soon as it receives it.
type exportBoundaryObserver struct {
	a2asrv.PassthroughCallInterceptor
	exporter *tracetest.InMemoryExporter
	observed chan bool
}

func (o *exportBoundaryObserver) After(_ context.Context, _ *a2asrv.CallContext, response *a2asrv.Response) error {
	update, ok := response.Payload.(*a2atype.TaskStatusUpdateEvent)
	if !ok {
		return nil
	}
	if !update.Status.State.Terminal() && update.Status.State != a2atype.TaskStateInputRequired && update.Status.State != a2atype.TaskStateAuthRequired {
		return nil
	}
	exported := false
	for _, span := range o.exporter.GetSpans() {
		if span.Name == "a2a.request" {
			exported = true
		}
	}
	o.observed <- exported
	return nil
}

func TestRequestSpanExportedBeforeQuiescentEvent(t *testing.T) {
	for _, state := range []a2atype.TaskState{a2atype.TaskStateCompleted, a2atype.TaskStateFailed, a2atype.TaskStateCanceled, a2atype.TaskStateInputRequired, a2atype.TaskStateAuthRequired} {
		t.Run(string(state), func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Hour)))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() {
				otel.SetTracerProvider(previous)
				_ = provider.Shutdown(context.Background())
			})
			observer := &exportBoundaryObserver{exporter: exporter, observed: make(chan bool, 1)}
			srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{finalState: state}, slog.New(slog.DiscardHandler), ServerConfig{Port: "0", Flush: provider.ForceFlush}, a2asrv.WithCallInterceptors(observer))
			if err != nil {
				t.Fatal(err)
			}
			testServer := httptest.NewUnstartedServer(srv.httpServer.Handler)
			testServer.Config.Protocols = srv.httpServer.Protocols
			testServer.Start()
			defer testServer.Close()
			defer srv.grpcServer.Stop()
			conn, err := grpc.NewClient(testServer.Listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi"))})
			if err != nil {
				t.Fatal(err)
			}

			stream, err := a2apb.NewA2AServiceClient(conn).SendStreamingMessage(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			for {
				event, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				domainEvent, err := pbconv.FromProtoStreamResponse(event)
				if err != nil {
					t.Fatal(err)
				}
				if update, ok := domainEvent.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State == state {
					break
				}
			}
			select {
			case exported := <-observer.observed:
				if !exported {
					t.Fatal("A2A request span was not exported before the quiescent event was sent")
				}
			default:
				t.Fatal("quiescent event was not observed")
			}
		})
	}
}

func TestRejectsNonLiteralHealthPath(t *testing.T) {
	for _, path := range []string{"ping", "/", "/ping/", "/ping/{id}", "/ping {x}"} {
		if _, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, slog.New(slog.DiscardHandler), ServerConfig{Port: "0", HealthPaths: []string{path}}); err == nil {
			t.Errorf("NewA2AServer accepted health path %q", path)
		}
	}
}

func TestConfiguredHealthPaths(t *testing.T) {
	srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, slog.New(slog.DiscardHandler), ServerConfig{Port: "0", HealthPaths: []string{"/ping"}, HealthHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"Healthy"}`))
	})})
	if err != nil {
		t.Fatalf("NewA2AServer: %v", err)
	}
	testServer := httptest.NewServer(srv.httpServer.Handler)
	t.Cleanup(func() { srv.grpcServer.Stop(); testServer.Close() })
	// The JSON-RPC handler owns "/", so an unregistered probe path never returns the probe body.
	for path, want := range map[string]bool{"/ping": true, "/ping/child": false, "/healthz": false} {
		resp, err := testServer.Client().Get(testServer.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if got := resp.StatusCode == http.StatusOK && string(body) == `{"status":"Healthy"}`; got != want {
			t.Fatalf("GET %s = %d %q, want probe=%v", path, resp.StatusCode, body, want)
		}
	}
}
