package a2a

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/types"
)

// a2aTracingMiddleware is an A2A server middleware that creates an invoke_agent
// span for each inbound A2A request, annotated with GenAI semantic convention
// attributes. Outbound client interceptors inject that span into proxied agent
// calls, giving a clean agent-invocation span hierarchy in Jaeger.
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
