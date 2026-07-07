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
//   - attrs   gen_ai.token.type, gen_ai.operation.name, gen_ai.provider.name,
//     gen_ai.request.model, gen_ai.response.model, error.type
//
// https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/
const (
	metricGenAIClientTokenUsage = "gen_ai_client_token_usage"

	labelGenAITokenType     = "gen_ai_token_type"
	labelGenAIOperationName = "gen_ai_operation_name"
	labelGenAIProviderName  = "gen_ai_provider_name"
	labelGenAIRequestModel  = "gen_ai_request_model"
	labelGenAIResponseModel = "gen_ai_response_model"
	labelErrorType          = "error_type"

	tokenTypeInput  = "input"
	tokenTypeOutput = "output"

	// operationChat is the gen_ai.operation.name for the chat-completion calls
	// that produce the token usage recorded here.
	operationChat = "chat"
)

// tokenUsageBuckets is the explicit bucket layout recommended by the GenAI
// metrics semantic conventions for gen_ai.client.token.usage.
// https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/#metric-gen_aiclienttokenusage
var tokenUsageBuckets = []float64{1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864}

// tokenUsage is the gen_ai.client.token.usage histogram, registered on the
// default Prometheus registry so it is served by MetricsHandler alongside the
// standard Go/process collectors.
var tokenUsage = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    metricGenAIClientTokenUsage,
		Help:    "Measures the number of input and output tokens used by GenAI requests.",
		Buckets: tokenUsageBuckets,
	},
	[]string{
		labelGenAITokenType,
		labelGenAIOperationName,
		labelGenAIProviderName,
		labelGenAIRequestModel,
		labelGenAIResponseModel,
		labelErrorType,
	},
)

// SemconvProviderName maps a kagent model type (adk.Model.GetType()) to the
// OpenTelemetry GenAI well-known gen_ai.provider.name value. Types without a
// well-known mapping (e.g. ollama, sap_ai_core, custom) pass through unchanged.
// https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/#gen-ai-provider-name
func SemconvProviderName(modelType string) string {
	switch modelType {
	case "openai":
		return "openai"
	case "azure_openai":
		return "azure.ai.openai"
	case "anthropic":
		return "anthropic"
	case "gemini":
		return "gcp.gemini"
	case "gemini_vertex_ai", "gemini_anthropic":
		return "gcp.vertex_ai"
	case "bedrock":
		return "aws.bedrock"
	default:
		return modelType
	}
}

// TokenUsage carries the per-request labels and counts for one recording on the
// gen_ai.client.token.usage histogram.
type TokenUsage struct {
	// RequestModel is gen_ai.request.model (the configured model).
	RequestModel string
	// ResponseModel is gen_ai.response.model (the model the provider actually
	// served). Falls back to RequestModel when empty.
	ResponseModel string
	// Provider is gen_ai.provider.name (a semconv well-known value).
	Provider string
	// ErrorType is error.type; empty for successful requests.
	ErrorType string
	// InputTokens / OutputTokens are the token counts (output = candidate +
	// reasoning tokens). Non-positive counts are skipped.
	InputTokens  int64
	OutputTokens int64
}

// MetricsHandler returns an http.Handler that serves the agent's Prometheus
// metrics, intended to be mounted at /metrics for scraping.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// RecordTokenUsage records input/output token counts on the
// gen_ai.client.token.usage histogram. Zero/negative counts are skipped.
func RecordTokenUsage(u TokenUsage) {
	responseModel := u.ResponseModel
	if responseModel == "" {
		responseModel = u.RequestModel
	}
	if u.InputTokens > 0 {
		tokenUsage.WithLabelValues(tokenTypeInput, operationChat, u.Provider, u.RequestModel, responseModel, u.ErrorType).
			Observe(float64(u.InputTokens))
	}
	if u.OutputTokens > 0 {
		tokenUsage.WithLabelValues(tokenTypeOutput, operationChat, u.Provider, u.RequestModel, responseModel, u.ErrorType).
			Observe(float64(u.OutputTokens))
	}
}
