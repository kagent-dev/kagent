package telemetry

import (
	"context"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// GenAI token-usage instrumentation. The metric and attribute names follow
// the OpenTelemetry GenAI semantic conventions, and the attribute set matches
// the one upstream google-adk records for gen_ai.client.token.usage, so a
// single dashboard works across the Go and Python runtimes.
const (
	metricGenAIClientTokenUsage = "gen_ai.client.token.usage"
	genAIMeterScope             = "gcp.vertex.agent"

	attrGenAITokenType     = "gen_ai.token.type"
	attrGenAIRequestModel  = "gen_ai.request.model"
	attrGenAIResponseModel = "gen_ai.response.model"
	attrGenAIProviderName  = "gen_ai.provider.name"
	attrGenAIAgentName     = "gen_ai.agent.name"

	tokenTypeInput  = "input"
	tokenTypeOutput = "output"
)

// tokenUsageHistogram records gen_ai.client.token.usage per LLM call. It is
// set when the meter provider is initialized (metrics enabled); otherwise it
// stays nil and recording is a cheap no-op, keeping the gate default-OFF.
var tokenUsageHistogram metric.Int64Histogram

// TokenUsage carries the per-LLM-call labels and token counts for one
// recording on the gen_ai.client.token.usage histogram. RequestModel and
// ProviderName are resolved at startup from the agent config; ResponseModel
// falls back to RequestModel when a specific response model is unavailable.
type TokenUsage struct {
	RequestModel  string
	ResponseModel string
	ProviderName  string
	AgentName     string
	InputTokens   int64
	OutputTokens  int64
}

// RecordTokenUsage records input + output token counts on the
// gen_ai.client.token.usage histogram, one observation per token type.
// Zero/negative counts are skipped, and nothing is recorded when metrics are
// disabled or initialization failed (the instrument is nil).
func RecordTokenUsage(ctx context.Context, usage TokenUsage) {
	h := tokenUsageHistogram
	if h == nil {
		return
	}
	responseModel := usage.ResponseModel
	if responseModel == "" {
		responseModel = usage.RequestModel
	}
	base := []attribute.KeyValue{
		attribute.String(attrGenAIRequestModel, usage.RequestModel),
		attribute.String(attrGenAIResponseModel, responseModel),
		attribute.String(attrGenAIProviderName, usage.ProviderName),
		attribute.String(attrGenAIAgentName, usage.AgentName),
	}
	recordToken := func(tokenType string, count int64) {
		if count <= 0 {
			return
		}
		opts := append([]attribute.KeyValue{attribute.String(attrGenAITokenType, tokenType)}, base...)
		h.Record(ctx, count, metric.WithAttributes(opts...))
	}
	recordToken(tokenTypeInput, usage.InputTokens)
	recordToken(tokenTypeOutput, usage.OutputTokens)
}

// SemconvProviderName maps a kagent model type to its OpenTelemetry GenAI
// gen_ai.provider.name value. Unknown types pass through unchanged so custom
// providers keep their configured identity.
func SemconvProviderName(modelType string) string {
	switch modelType {
	case "openai":
		return "openai"
	case "azure_openai":
		return "azure.ai.openai"
	case "anthropic":
		return "anthropic"
	case "gemini", "gemini_vertex_ai", "gemini_anthropic":
		return "gcp.gemini"
	case "bedrock":
		return "aws.bedrock"
	default:
		return modelType
	}
}

// newMeterProvider builds a MeterProvider with a periodic OTLP metric exporter,
// sharing the endpoint/protocol resolution used by traces and logs.
func newMeterProvider(ctx context.Context, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	protocol := resolveOTLPProtocol("METRICS")
	endpoint := resolveEndpoint("METRICS")

	var exporter sdkmetric.Exporter
	var err error
	switch protocol {
	case "http/protobuf":
		var opts []otlpmetrichttp.Option
		if endpoint != "" {
			opts = append(opts, otlpmetrichttp.WithEndpointURL(endpoint))
		}
		exporter, err = otlpmetrichttp.New(ctx, opts...)
	default:
		var opts []otlpmetricgrpc.Option
		if endpoint != "" {
			if u, parseErr := url.Parse(endpoint); parseErr == nil && u.Scheme != "" && u.Host != "" {
				opts = append(opts, otlpmetricgrpc.WithEndpointURL(u.String()))
			} else {
				opts = append(opts, otlpmetricgrpc.WithEndpoint(endpoint))
			}
		}
		exporter, err = otlpmetricgrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		sdkmetric.WithResource(res),
	), nil
}

// initTokenUsageRecorder binds the gen_ai.client.token.usage histogram to the
// given meter scope. It is called after setting the global meter provider.
func initTokenUsageRecorder(mp *sdkmetric.MeterProvider) {
	meter := mp.Meter(genAIMeterScope)
	var err error
	tokenUsageHistogram, err = meter.Int64Histogram(
		metricGenAIClientTokenUsage,
		metric.WithUnit("{token}"),
		metric.WithDescription("Number of input and output tokens used by GenAI requests."),
	)
	if err != nil {
		otel.Handle(err)
		tokenUsageHistogram = nil
	}
}
