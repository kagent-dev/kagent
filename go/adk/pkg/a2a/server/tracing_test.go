package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"log/slog"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// staticUserInterceptor stands in for UserIDCallInterceptor, which the app
// builder installs ahead of the invocation interceptor.
type staticUserInterceptor struct {
	a2asrv.PassthroughCallInterceptor
	userID string
}

func (i staticUserInterceptor) Before(ctx context.Context, _ *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	return auth.WithUserID(ctx, i.userID), nil, nil
}

// failingExecutor rejects the request the way validation does, before any
// executor takes ownership of the invocation.
type failingExecutor struct{}

func (failingExecutor) Execute(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		yield(nil, fmt.Errorf("harness runtime accepts exactly one user text part"))
	}
}

func (failingExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

// adoptingExecutor mimics a Harness runtime: it takes ownership of the
// invocation, lets the unary response go out while the task is still working,
// and completes the invocation itself at the execution boundary.
type adoptingExecutor struct {
	release <-chan struct{}
	done    chan struct{}
}

func (e adoptingExecutor) Execute(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		invocation := tracing.InvocationFromContext(ctx)
		invocation.Adopt()
		defer close(e.done)
		if !yield(a2atype.NewSubmittedTask(reqCtx, reqCtx.Message), nil) {
			return
		}
		<-e.release
		invocation.SetAttributes(attribute.String(tracing.AttributeSegment, tracing.SegmentInitial))
		if _, err := invocation.End(ctx, tracing.Result{TaskState: string(a2atype.TaskStateCompleted)}); err != nil {
			panic(err)
		}
		yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil), nil)
	}
}

func (adoptingExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

func syncExporter(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return exporter
}

func sendMessage(t *testing.T, server *A2AServer, request *a2atype.SendMessageRequest) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "1", "method": "SendMessage", "params": request,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpRequest := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set(a2atype.SvcParamVersion, string(a2atype.Version))
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, httpRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
}

// isRequestSpan recognizes the span the interceptor opens by the A2A method
// only it records, whichever name the runtime gives it.
func isRequestSpan(span tracetest.SpanStub) bool {
	return spanAttribute(span, tracing.AttributeMethod) != ""
}

func requestSpan(t *testing.T, exporter *tracetest.InMemoryExporter) tracetest.SpanStub {
	t.Helper()
	var found []tracetest.SpanStub
	for _, span := range exporter.GetSpans() {
		if isRequestSpan(span) {
			found = append(found, span)
		}
	}
	if len(found) != 1 {
		t.Fatalf("exported %d request spans, want exactly one", len(found))
	}
	return found[0]
}

func spanAttribute(span tracetest.SpanStub, key string) string {
	for _, attr := range span.Attributes {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

func TestRequestSpanCarriesStaticIdentityAndTrustedUser(t *testing.T) {
	exporter := syncExporter(t)
	server, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, slog.New(slog.DiscardHandler),
		ServerConfig{Port: "0", Telemetry: tracing.RuntimeTelemetry{
			Runtime: tracing.RuntimeCodex, AgentName: "reporter-codex", AgentNamespace: "team",
			Provider: "openai", Model: "gpt-5.2-codex",
		}},
		a2asrv.WithCallInterceptors(staticUserInterceptor{userID: "person-1"}))
	if err != nil {
		t.Fatal(err)
	}

	sendMessage(t, server, &a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	})

	span := requestSpan(t, exporter)
	// The conventions name an invoke_agent span after the agent it invokes.
	if span.Name != "invoke_agent reporter-codex" {
		t.Errorf("span name = %q, want %q", span.Name, "invoke_agent reporter-codex")
	}
	if span.InstrumentationScope.SchemaURL != tracing.SchemaURL {
		t.Errorf("schema URL = %q, want %q", span.InstrumentationScope.SchemaURL, tracing.SchemaURL)
	}
	for key, want := range map[string]string{
		tracing.AttributeOperationName: tracing.OperationInvokeAgent,
		tracing.AttributeMethod:        "SendMessage",
		tracing.AttributeRuntime:       "codex",
		tracing.AttributeAgentName:     "reporter-codex",
		tracing.AttributeAgentID:       "team/reporter-codex",
		tracing.AttributeProviderName:  "openai",
		tracing.AttributeRequestModel:  "gpt-5.2-codex",
		tracing.AttributeUserID:        "person-1",
		tracing.AttributeTaskState:     string(a2atype.TaskStateCompleted),
	} {
		if got := spanAttribute(span, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

// An ADK runtime emits its own invoke_agent spans, so its request span stays a
// transport span that carries the identity but no operation.
func TestRequestSpanStaysATransportSpanForTheADK(t *testing.T) {
	exporter := syncExporter(t)
	server, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, slog.New(slog.DiscardHandler),
		ServerConfig{Port: "0", Telemetry: tracing.RuntimeTelemetry{
			Runtime: tracing.RuntimeADKGo, AgentName: "assistant-kagent", AgentNamespace: "team",
		}})
	if err != nil {
		t.Fatal(err)
	}

	sendMessage(t, server, &a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	})

	span := requestSpan(t, exporter)
	if span.Name != tracing.TransportSpanName {
		t.Errorf("span name = %q, want %q", span.Name, tracing.TransportSpanName)
	}
	if got := spanAttribute(span, tracing.AttributeOperationName); got != "" {
		t.Errorf("%s = %q, want none on an ADK request span", tracing.AttributeOperationName, got)
	}
	for key, want := range map[string]string{
		tracing.AttributeRuntime:   "adk-go",
		tracing.AttributeAgentName: "assistant-kagent",
		tracing.AttributeAgentID:   "team/assistant-kagent",
	} {
		if got := spanAttribute(span, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	invocations := 0
	for _, exported := range exporter.GetSpans() {
		if exported.Name != "invocation" {
			continue
		}
		invocations++
		// The kagent-owned invocation span keeps the ADK scope name but declares
		// the contract's schema, like every other tracer kagent creates.
		if exported.InstrumentationScope.SchemaURL != tracing.SchemaURL {
			t.Errorf("invocation schema URL = %q, want %q", exported.InstrumentationScope.SchemaURL, tracing.SchemaURL)
		}
	}
	if invocations != 1 {
		t.Errorf("exported %d invocation spans, want exactly one", invocations)
	}
}

// blockingExecutor never adopts the invocation and holds the task in the
// working state until released, the way an ADK turn does while a model call
// is in flight.
type blockingExecutor struct {
	release <-chan struct{}
}

func (e blockingExecutor) Execute(_ context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if !yield(a2atype.NewSubmittedTask(reqCtx, reqCtx.Message), nil) {
			return
		}
		if !yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil), nil) {
			return
		}
		<-e.release
		yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil), nil)
	}
}

func (blockingExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

// A streaming client that goes away before the task quiesces gets no final
// After callback from a2a-go, while execution continues detached. The request
// span must still complete and export, recording the abandonment, rather than
// stay open for ever.
func TestRequestSpanCompletesWhenAStreamingClientDisconnects(t *testing.T) {
	exporter := syncExporter(t)
	release := make(chan struct{})
	defer close(release)
	server, err := NewA2AServer(a2atype.AgentCard{}, blockingExecutor{release: release}, slog.New(slog.DiscardHandler), ServerConfig{Port: "0"})
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewUnstartedServer(server.httpServer.Handler)
	testServer.Config.Protocols = server.httpServer.Protocols
	testServer.Start()
	defer testServer.Close()
	defer server.grpcServer.Stop()
	conn, err := grpc.NewClient(testServer.Listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	stream, err := a2apb.NewA2AServiceClient(conn).SendStreamingMessage(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	cancel()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var found []tracetest.SpanStub
		for _, span := range exporter.GetSpans() {
			if isRequestSpan(span) {
				found = append(found, span)
			}
		}
		if len(found) == 1 {
			if got := spanAttribute(found[0], tracing.AttributeDisposition); got != tracing.DispositionAbandoned {
				t.Fatalf("%s = %q, want %q", tracing.AttributeDisposition, got, tracing.DispositionAbandoned)
			}
			if got := spanAttribute(found[0], tracing.AttributeErrorType); got != "" {
				t.Fatalf("a disconnected client was reported as %s=%q", tracing.AttributeErrorType, got)
			}
			return
		}
		if len(found) > 1 {
			t.Fatalf("exported %d request spans, want exactly one", len(found))
		}
		if time.Now().After(deadline) {
			t.Fatal("request span was not exported after the client disconnected")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRequestSpanRecordsEarlyFailure(t *testing.T) {
	exporter := syncExporter(t)
	server, err := NewA2AServer(a2atype.AgentCard{}, failingExecutor{}, slog.New(slog.DiscardHandler),
		ServerConfig{Port: "0", Telemetry: tracing.RuntimeTelemetry{
			Runtime: tracing.RuntimeClaude, AgentName: "reporter-claude", AgentNamespace: "team",
		}})
	if err != nil {
		t.Fatal(err)
	}

	// The JSON-RPC error is the response; the request itself is well formed.
	sendMessage(t, server, &a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	})

	span := requestSpan(t, exporter)
	if span.Status.Code != codes.Error {
		t.Errorf("status = %v, want an error status", span.Status.Code)
	}
	if got := spanAttribute(span, tracing.AttributeErrorType); got != "transport_error" {
		t.Errorf("%s = %q, want %q", tracing.AttributeErrorType, got, "transport_error")
	}
	if got := spanAttribute(span, tracing.AttributeRuntime); got != "claude" {
		t.Errorf("a rejected request lost its runtime identity: %s = %q", tracing.AttributeRuntime, got)
	}
}

// A nonblocking unary request returns while a2a-go keeps running the executor
// detached from the caller. The invocation must stay open until execution
// reaches its own boundary.
func TestAdoptedInvocationOutlivesTheUnaryResponse(t *testing.T) {
	exporter := syncExporter(t)
	release := make(chan struct{})
	executor := adoptingExecutor{release: release, done: make(chan struct{})}
	server, err := NewA2AServer(a2atype.AgentCard{}, executor, slog.New(slog.DiscardHandler), ServerConfig{Port: "0"})
	if err != nil {
		t.Fatal(err)
	}

	sendMessage(t, server, &a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
		Config:  &a2atype.SendMessageConfig{ReturnImmediately: true},
	})

	for _, span := range exporter.GetSpans() {
		if isRequestSpan(span) {
			t.Fatal("the unary response completed an invocation that execution still owned")
		}
	}
	close(release)
	<-executor.done

	span := requestSpan(t, exporter)
	if got := spanAttribute(span, tracing.AttributeTaskState); got != string(a2atype.TaskStateCompleted) {
		t.Errorf("%s = %q, want the state execution reported", tracing.AttributeTaskState, got)
	}
	if got := spanAttribute(span, tracing.AttributeSegment); got != tracing.SegmentInitial {
		t.Errorf("%s = %q, want %q", tracing.AttributeSegment, got, tracing.SegmentInitial)
	}
}

// messageExecutor returns a standalone agent message, which A2A treats as a
// final result. It carries no task state, so the interceptor has to recognize
// it or nothing else will ever complete the invocation.
type messageExecutor struct{}

func (messageExecutor) Execute(_ context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("done"))
		message.TaskID, message.ContextID = reqCtx.TaskID, reqCtx.ContextID
		yield(message, nil)
	}
}

func (messageExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

func TestRequestSpanCompletesOnAStreamingMessageResult(t *testing.T) {
	exporter := syncExporter(t)
	server, err := NewA2AServer(a2atype.AgentCard{}, messageExecutor{}, slog.New(slog.DiscardHandler), ServerConfig{Port: "0"})
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewUnstartedServer(server.httpServer.Handler)
	testServer.Config.Protocols = server.httpServer.Protocols
	testServer.Start()
	defer testServer.Close()
	defer server.grpcServer.Stop()
	conn, err := grpc.NewClient(testServer.Listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := a2apb.NewA2AServiceClient(conn).SendStreamingMessage(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := stream.Recv(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}

	requestSpan(t, exporter)
}
