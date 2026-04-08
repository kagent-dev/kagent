package a2a

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/types"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// a2aTracingMiddleware is an A2A server middleware that creates an invoke_agent
// span for each inbound A2A request, annotated with GenAI semantic convention
// attributes. The span becomes the parent of any outbound proxy calls made by
// traceInjectHandler, giving a clean agent-invocation span hierarchy in Jaeger.
type a2aTracingMiddleware struct {
	agentRef types.NamespacedName
	provider attribute.KeyValue
}

func newA2ATracingMiddleware(agentRef types.NamespacedName, provider attribute.KeyValue) *a2aTracingMiddleware {
	return &a2aTracingMiddleware{agentRef: agentRef, provider: provider}
}

func (m *a2aTracingMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("kagent").Start(r.Context(), "invoke_agent",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.GenAIOperationNameInvokeAgent,
				m.provider,
				semconv.GenAIAgentName(m.agentRef.Name),
				semconv.GenAIAgentID(m.agentRef.String()),
			),
		)
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// traceInjectHandler wraps an HTTPReqHandler and injects W3C TraceContext
// headers (traceparent, tracestate) from the Go context into every outgoing
// proxy request, so the downstream agent receives the active span as its parent.
type traceInjectHandler struct {
	next a2aclient.HTTPReqHandler
}

func (h *traceInjectHandler) Handle(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	return h.next.Handle(ctx, client, req)
}

// resolveProviderName looks up the ModelConfig for a declarative agent and
// returns the corresponding gen_ai.provider.name attribute. Falls back to "kagent"
// for BYO agents or if the ModelConfig cannot be fetched.
func resolveProviderName(ctx context.Context, cache crcache.Cache, agent *v1alpha2.Agent) attribute.KeyValue {
	if agent.Spec.Declarative == nil {
		return semconv.GenAIProviderNameKey.String("kagent")
	}
	mcName := agent.Spec.Declarative.ModelConfig
	if mcName == "" {
		mcName = "default-model-config"
	}
	mc := &v1alpha2.ModelConfig{}
	if err := cache.Get(ctx, types.NamespacedName{Namespace: agent.GetNamespace(), Name: mcName}, mc); err != nil {
		return semconv.GenAIProviderNameKey.String("kagent")
	}
	return genAIProviderName(mc.Spec.Provider)
}

// genAIProviderName maps kagent's ModelProvider values to the standard
// gen_ai.provider.name attributes defined by the OpenTelemetry GenAI semantic
// conventions. Custom values are used for providers not in the standard list.
func genAIProviderName(p v1alpha2.ModelProvider) attribute.KeyValue {
	switch p {
	case v1alpha2.ModelProviderOpenAI:
		return semconv.GenAIProviderNameOpenAI
	case v1alpha2.ModelProviderAzureOpenAI:
		return semconv.GenAIProviderNameAzureAIOpenAI
	case v1alpha2.ModelProviderAnthropic:
		return semconv.GenAIProviderNameAnthropic
	case v1alpha2.ModelProviderGemini:
		return semconv.GenAIProviderNameGCPGemini
	case v1alpha2.ModelProviderGeminiVertexAI:
		return semconv.GenAIProviderNameGCPVertexAI
	case v1alpha2.ModelProviderAnthropicVertexAI:
		return semconv.GenAIProviderNameKey.String("anthropic.vertex_ai")
	case v1alpha2.ModelProviderBedrock:
		return semconv.GenAIProviderNameAWSBedrock
	case v1alpha2.ModelProviderOllama:
		return semconv.GenAIProviderNameKey.String("ollama")
	default:
		return semconv.GenAIProviderNameKey.String("kagent")
	}
}
