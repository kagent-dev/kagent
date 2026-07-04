package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// GenAI token-usage instrumentation using the native Prometheus client library.
//
// Metric and attribute names follow the OpenTelemetry GenAI semantic
// conventions (semconv 1.40.0), mapped to Prometheus naming (dots -> underscores):
//   - metric  gen_ai.client.token.usage -> gen_ai_client_token_usage
//   - attrs   gen_ai.token.type, gen_ai.request.model, gen_ai.provider.name,
//     gen_ai.operation.name
//
// https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/
const (
	metricGenAIClientTokenUsage = "gen_ai_client_token_usage"

	labelGenAITokenType     = "gen_ai_token_type"
	labelGenAIRequestModel  = "gen_ai_request_model"
	labelGenAIProviderName  = "gen_ai_provider_name"
	labelGenAIOperationName = "gen_ai_operation_name"

	tokenTypeInput  = "input"
	tokenTypeOutput = "output"

	// operationChat is the gen_ai.operation.name for the chat-completion calls
	// that produce the token usage recorded here.
	operationChat = "chat"
)

// tokenUsage is the gen_ai.client.token.usage histogram, registered on the
// default Prometheus registry so it is served by MetricsHandler alongside the
// standard Go/process collectors.
var tokenUsage = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name: metricGenAIClientTokenUsage,
		Help: "Measures the number of input and output tokens used by GenAI requests.",
		// Token-count buckets spanning short prompts to large-context requests.
		Buckets: []float64{1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576},
	},
	[]string{labelGenAITokenType, labelGenAIRequestModel, labelGenAIProviderName, labelGenAIOperationName},
)

// MetricsHandler returns an http.Handler that serves the agent's Prometheus
// metrics, intended to be mounted at /metrics for scraping.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// RecordTokenUsage records input/output token counts on the
// gen_ai.client.token.usage histogram, labelled with the request model, provider
// and (implicit) chat operation. Zero counts are skipped; empty model/provider
// are recorded as empty label values.
func RecordTokenUsage(model, provider string, inputTokens, outputTokens int64) {
	if inputTokens > 0 {
		tokenUsage.WithLabelValues(tokenTypeInput, model, provider, operationChat).Observe(float64(inputTokens))
	}
	if outputTokens > 0 {
		tokenUsage.WithLabelValues(tokenTypeOutput, model, provider, operationChat).Observe(float64(outputTokens))
	}
}
