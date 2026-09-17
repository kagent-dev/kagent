package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/resource"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	claudeTracingE2EHarness = "claude-tracing-e2e"
	codexTracingE2EHarness  = "codex-tracing-e2e"
)

type capturedSpan struct {
	serviceName string
	scopeName   string
	span        *tracepb.Span
}

// otlpTraceReceiver is a test-only implementation of the OTLP TraceService,
// not a general-purpose OpenTelemetry Collector. The tracing Harnesses export
// directly to this gRPC server, which retains ResourceSpans in memory so the
// test can inspect the exact data that crossed the runtime's OTLP boundary.
//
// Export records a request before returning its successful response. An SDK
// ForceFlush waits for that response, so spans flushed at the completed-chat
// boundary are observable synchronously; the assertions must not poll and
// accidentally accept a later batch-timer or shutdown export.
type otlpTraceReceiver struct {
	collectortrace.UnimplementedTraceServiceServer
	mu      sync.Mutex
	exports int
	records []*tracepb.ResourceSpans
}

// startOTLPTraceReceiver binds inside the go test process.
func startOTLPTraceReceiver(t *testing.T) *otlpTraceReceiver {
	t.Helper()
	if suiteTraceReceiver != nil {
		return suiteTraceReceiver
	}
	address := os.Getenv("KAGENT_E2E_OTLP_LISTEN_ADDRESS")
	if address == "" {
		address = ":14317"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listen for OTLP traces on %s: %v", address, err)
	}
	receiver, stop := serveOTLPTraceReceiver(listener)
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("serve OTLP traces: %v", err)
		}
	})
	return receiver
}

func serveOTLPTraceReceiver(listener net.Listener) (*otlpTraceReceiver, func() error) {
	receiver := &otlpTraceReceiver{}
	server := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(server, receiver)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	return receiver, func() error {
		server.Stop()
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
		return nil
	}
}

func TestOTLPTraceReceiverFlush(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	receiver, stop := serveOTLPTraceReceiver(listener)
	t.Cleanup(func() { require.NoError(t, stop()) })
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://"+listener.Addr().String())
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")
	// Only an explicit flush can deliver the span during this test.
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "3600000")
	provider, err := tracing.NewTracerProvider(t.Context(), resource.Empty())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, provider.Shutdown(ctx))
	})
	_, span := provider.Tracer("receiver-test").Start(t.Context(), "response")
	span.End()
	// A missing receiver would consume the runtime's three-second flush timeout.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, provider.ForceFlush(ctx))
	traceID := span.SpanContext().TraceID()
	require.Len(t, receiver.selectSpans(traceID[:], "", "receiver-test", "response", nil), 1)
}

func (r *otlpTraceReceiver) Export(_ context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exports++
	for _, resourceSpans := range request.GetResourceSpans() {
		// The receiver owns its retained data after this RPC returns. Clone the
		// protobuf rather than depending on the gRPC request's lifetime.
		r.records = append(r.records, proto.Clone(resourceSpans).(*tracepb.ResourceSpans))
	}
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func (r *otlpTraceReceiver) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exports = 0
	r.records = nil
}

// diagnostic summarizes only the spans useful for explaining a failed
// assertion: a2a.request spans and spans correlated by the injected trace ID.
// Codex can emit hundreds of native spans, so cap the detail included in logs.
func (r *otlpTraceReceiver) diagnostic(expectedTraceID []byte) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	const detailLimit = 12
	totalSpans := 0
	details := make([]string, 0, detailLimit)
	for _, resourceSpans := range r.records {
		serviceName := stringAttribute(resourceSpans.GetResource().GetAttributes(), "service.name")
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				totalSpans++
				if len(details) == detailLimit || !bytes.Equal(span.GetTraceId(), expectedTraceID) && span.GetName() != "a2a.request" {
					continue
				}
				details = append(details, fmt.Sprintf(
					"service=%q scope=%q name=%q trace_id=%x a2a.method=%q a2a.task.state=%q",
					serviceName,
					scopeSpans.GetScope().GetName(),
					span.GetName(),
					span.GetTraceId(),
					stringAttribute(span.GetAttributes(), "a2a.method"),
					stringAttribute(span.GetAttributes(), "a2a.task.state"),
				))
			}
		}
	}
	return fmt.Sprintf("exports=%d resource_spans=%d total_spans=%d relevant_spans=%v", r.exports, len(r.records), totalSpans, details)
}

// selectSpans returns all spans that match the given trace ID, service name,
// scope name, span name, and attributes.
func (r *otlpTraceReceiver) selectSpans(traceID []byte, serviceName, scopeName, spanName string, attributes map[string]string) []capturedSpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	var selected []capturedSpan
	for _, resourceSpans := range r.records {
		resourceServiceName := stringAttribute(resourceSpans.GetResource().GetAttributes(), "service.name")
		if serviceName != "" && resourceServiceName != serviceName {
			continue
		}
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			currentScopeName := scopeSpans.GetScope().GetName()
			if scopeName != "" && currentScopeName != scopeName {
				continue
			}
			for _, span := range scopeSpans.GetSpans() {
				if !bytes.Equal(span.GetTraceId(), traceID) || spanName != "" && span.GetName() != spanName {
					continue
				}
				matches := true
				for key, value := range attributes {
					if stringAttribute(span.GetAttributes(), key) != value {
						matches = false
						break
					}
				}
				if matches {
					selected = append(selected, capturedSpan{serviceName: resourceServiceName, scopeName: currentScopeName, span: span})
				}
			}
		}
	}
	return selected
}

func stringAttribute(attributes []*commonpb.KeyValue, key string) string {
	for _, attribute := range attributes {
		if attribute.GetKey() == key {
			return attribute.GetValue().GetStringValue()
		}
	}
	return ""
}

func TestE2ECompletedChatFlushesTraces(t *testing.T) {
	target := interactionTarget(t)
	requireTracingHarnesses(t)
	receiver := startOTLPTraceReceiver(t)

	for _, test := range []struct {
		name          string
		harness       string
		templateLabel string
		modelURL      func(*testing.T) string
		createModel   func(*testing.T, string) string
		prompt        string
		assertNative  func(*testing.T, *otlpTraceReceiver, []byte)
	}{
		{
			name: "claude", harness: claudeTracingE2EHarness, templateLabel: "claude-tracing",
			modelURL: func(t *testing.T) string {
				return reachableServerURL(t, startMockLLMServer(t, claudeInteractionMocks, "mocks/invoke_claude_agent.json"), "")
			},
			createModel: func(t *testing.T, baseURL string) string {
				return createClaudeMockModel(t, interactionKubeClient(t), baseURL).Name
			},
			prompt: "Return exactly CLAUDE_MOCK_FIRST.",
			assertNative: func(t *testing.T, receiver *otlpTraceReceiver, traceID []byte) {
				t.Helper()
				for _, captured := range receiver.selectSpans(traceID, "", "", "", nil) {
					if strings.HasPrefix(captured.span.GetName(), "claude_code.") {
						return
					}
				}
				t.Fatal("no claude_code.* span was exported before suspension")
			},
		},
		{
			name: "codex", harness: codexTracingE2EHarness, templateLabel: "codex-tracing",
			modelURL: func(t *testing.T) string {
				return reachableModelURL(t, startMockLLMServer(t, codexInteractionMocks, "mocks/invoke_codex_agent.json"))
			},
			createModel: func(t *testing.T, baseURL string) string {
				return createCodexMockModel(t, interactionKubeClient(t), baseURL).Name
			},
			prompt: "Return exactly CODEX_MOCK_FIRST.",
			assertNative: func(t *testing.T, receiver *otlpTraceReceiver, traceID []byte) {
				t.Helper()
				for _, captured := range receiver.selectSpans(traceID, "", "", "", nil) {
					if captured.serviceName == "codex-app-server" || strings.Contains(strings.ToLower(captured.scopeName), "codex") {
						return
					}
				}
				t.Fatal("no native Codex span was exported before suspension")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// One receiver serves both cases. Clear retained spans for readability;
			// the injected trace ID remains the authoritative correlation key because
			// unrelated controller exports can reach the same listener concurrently.
			receiver.clear()
			modelName := test.createModel(t, test.modelURL(t))
			template := createTracingTemplate(t, test.harness, test.templateLabel, modelName)
			fixture := newInteractionFixtureForHarnessTemplate(t, target, test.harness, template)
			traceID, traceparent := sampledTraceparent(t)
			fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, "traceparent", traceparent)

			streamed := sendTracingMessage(t, fixture, test.prompt)
			if streamed != a2atype.TaskStateCompleted {
				t.Fatalf("streamed task state = %s, want COMPLETED", streamed)
			}
			// Wait for the Actor to suspend then assert that the completed a2a.request span was exported.
			assertActorSuspended(t, fixture)
			if spans := receiver.selectSpans(traceID, "", "", "a2a.request", map[string]string{
				"a2a.method":     "SendStreamingMessage",
				"a2a.task.state": string(a2atype.TaskStateCompleted),
			}); len(spans) == 0 {
				t.Fatalf("completed a2a.request span was not exported before suspension: %s", receiver.diagnostic(traceID))
			}
			test.assertNative(t, receiver, traceID)
		})
	}
}

func requireTracingHarnesses(t *testing.T) {
	t.Helper()
	kube := interactionKubeClient(t)
	for _, name := range []string{claudeTracingE2EHarness, codexTracingE2EHarness} {
		var harness v1alpha3.Harness
		if err := kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: name}, &harness); apierrors.IsNotFound(err) {
			t.Skip("dedicated tracing Harnesses are not installed")
		} else if err != nil {
			t.Fatalf("get tracing Harness %s: %v", name, err)
		}
	}
}

func createTracingTemplate(t *testing.T, harnessName, runtimeLabel, modelName string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: harnessName + "-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": runtimeLabel},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: modelName},
			Description:  "Completed-chat trace flush E2E fixture",
			SystemPrompt: "Reply concisely and follow the requested output format exactly.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harnessName)
	return template.Name
}

// sampledTraceparent generates a trace ID and span ID, and returns a traceparent
// string that can be used to correlate spans.
func sampledTraceparent(t *testing.T) ([]byte, string) {
	t.Helper()
	traceID := make([]byte, 16)
	spanID := make([]byte, 8)
	if _, err := rand.Read(traceID); err != nil {
		t.Fatalf("generate trace ID: %v", err)
	}
	if _, err := rand.Read(spanID); err != nil {
		t.Fatalf("generate span ID: %v", err)
	}
	return traceID, fmt.Sprintf("00-%x-%x-01", traceID, spanID)
}

func sendTracingMessage(t *testing.T, fixture *interactionFixture, text string) a2atype.TaskState {
	t.Helper()
	_, request := newMessageRequest(t, text)
	stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
	if err != nil {
		t.Fatalf("start streaming traced A2A message: %v", err)
	}
	var terminalState a2atype.TaskState
	terminalEvents := 0
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if terminalEvents != 1 {
				t.Fatalf("traced stream terminal event count = %d, want 1", terminalEvents)
			}
			return terminalState
		}
		if err != nil {
			t.Fatalf("receive traced A2A stream: %v", err)
		}
		if terminalEvents != 0 {
			t.Fatalf("traced stream emitted an event after terminal state %s", terminalState)
		}
		event, err := pbconv.FromProtoStreamResponse(response)
		if err != nil {
			t.Fatalf("decode traced A2A stream: %v", err)
		}
		var state a2atype.TaskState
		switch event := event.(type) {
		case *a2atype.Task:
			state = event.Status.State
		case *a2atype.TaskStatusUpdateEvent:
			state = event.Status.State
		}
		if state.Terminal() {
			terminalEvents++
			terminalState = state
		}
	}
}

func assertActorSuspended(t *testing.T, fixture *interactionFixture) {
	t.Helper()
	actorID := substrate.ActorName(fixture.instanceID)
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 30*time.Second)
	defer cancel()
	err := wait.PollUntilContextTimeout(ctx, time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		actor, err := findSubstrateActor(ctx, fixture.system, "", actorID)
		if err != nil {
			return false, err
		}
		return actor != nil && actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_SUSPENDED, nil
	})
	if err != nil {
		t.Fatalf("Actor %s did not reach Suspended: %v", actorID, err)
	}
}
