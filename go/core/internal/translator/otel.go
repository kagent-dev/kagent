package translator

import (
	"maps"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	corev1 "k8s.io/api/core/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

type Signal string

const (
	SignalTraces  Signal = "traces"
	SignalMetrics Signal = "metrics"
	SignalLogs    Signal = "logs"
)

type TelemetryConfig struct {
	Runtime               tracing.RuntimeTelemetry
	Traces, Metrics, Logs SignalConfig
}

type SignalConfig struct {
	Enabled                      bool
	Endpoint, Protocol, Hostname string
}

func (c *TelemetryConfig) signal(signal Signal) *SignalConfig {
	switch signal {
	case SignalTraces:
		return &c.Traces
	case SignalMetrics:
		return &c.Metrics
	default:
		return &c.Logs
	}
}
func (c TelemetryConfig) Enabled() bool {
	return c.Traces.Enabled || c.Metrics.Enabled || c.Logs.Enabled
}

// Destinations are the egress origins of the enabled signals.
func (c TelemetryConfig) Destinations(environment []corev1.EnvVar) []string {
	raw := make([]string, 0, len(environment))
	for _, item := range environment {
		raw = append(raw, item.Name+"="+item.Value)
	}
	cfg := telemetry.Config{}
	if c.Runtime.Exporters != nil {
		cfg = *c.Runtime.Exporters
	}
	raw = telemetry.Environment(raw, cfg)
	var hosts []string
	for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
		if exporter := telemetry.OTLPExporter(raw, signal); exporter != nil {
			hosts = appendURLOrigin(hosts, exporter.Endpoint)
		}
	}
	return hosts
}

// RuntimeTelemetry is the compiler-owned telemetry contract for one compiled
// agent. Identity is what the runtime reports on every invocation span and on
// its resource, including the model the agent is bound to, so usage a native
// runtime reports without naming the model can still be attributed.
func (c TelemetryConfig) RuntimeTelemetry(runtime tracing.Runtime, agentName, namespace string, model v1alpha3.ModelConfigSpec) tracing.RuntimeTelemetry {
	result := c.Runtime
	result.Runtime, result.AgentName, result.AgentNamespace = runtime, agentName, namespace
	result.Provider, result.Model = ProviderName(model.Provider), strings.TrimSpace(model.Model)
	return result
}

// ProviderName maps a ModelConfig provider onto the GenAI conventions'
// provider vocabulary. Providers the conventions do not list get a lowercase
// identifier of the same shape, which the conventions permit as a custom value.
func ProviderName(provider v1alpha3.ModelProvider) string {
	switch provider {
	case v1alpha3.ModelProviderAnthropic:
		return conv.GenAIProviderNameAnthropic
	case v1alpha3.ModelProviderOpenAI:
		return conv.GenAIProviderNameOpenAI
	case v1alpha3.ModelProviderAzureOpenAI:
		return conv.GenAIProviderNameAzureAIOpenAI
	case v1alpha3.ModelProviderBedrock:
		return conv.GenAIProviderNameAWSBedrock
	case v1alpha3.ModelProviderGemini:
		return conv.GenAIProviderNameGCPGemini
	case v1alpha3.ModelProviderGeminiVertexAI, v1alpha3.ModelProviderAnthropicVertexAI:
		return conv.GenAIProviderNameGCPVertexAI
	case v1alpha3.ModelProviderFoundry:
		return conv.GenAIProviderNameAzureAIInference
	case v1alpha3.ModelProviderOllama:
		return "ollama"
	case v1alpha3.ModelProviderSAPAICore:
		return "sap.ai_core"
	default:
		return strings.ToLower(string(provider))
	}
}

// TelemetryEnvironment adapts settings for opaque BYO images.
func (c TelemetryConfig) TelemetryEnvironment(identity tracing.RuntimeTelemetry, environment []corev1.EnvVar) []corev1.EnvVar {
	cfg := telemetry.Config{}
	if c.Runtime.Exporters != nil {
		cfg = *c.Runtime.Exporters
	}
	// Opaque SDKs do not read the runtime JSON. Supply a service-name default
	// through resource attributes so their own OTEL_SERVICE_NAME still wins.
	cfg.ResourceAttributes = maps.Clone(cfg.ResourceAttributes)
	if cfg.ResourceAttributes == nil {
		cfg.ResourceAttributes = make(map[string]string)
	}
	if identity.AgentName != "" {
		cfg.ResourceAttributes[string(semconv.ServiceNameKey)] = identity.AgentName
	}
	raw := make([]string, 0, len(environment))
	provided := make(map[string]corev1.EnvVar, len(environment))
	for _, item := range environment {
		raw = append(raw, item.Name+"="+item.Value)
		provided[item.Name] = item
	}
	raw = tracing.ResourceEnvironment(telemetry.Environment(raw, cfg), resourceIdentity(identity))
	result := make([]corev1.EnvVar, 0, len(raw))
	for _, value := range raw {
		name, v, _ := strings.Cut(value, "=")
		if original, ok := provided[name]; ok && original.ValueFrom != nil {
			result = append(result, original)
			continue
		}
		result = append(result, corev1.EnvVar{Name: name, Value: v})
	}
	return result
}

// resourceIdentity leaves kagent.runtime to the runtime, which knows what it is.
func resourceIdentity(identity tracing.RuntimeTelemetry) []attribute.KeyValue {
	attributes := []attribute.KeyValue{
		semconv.ServiceNamespaceKey.String(identity.AgentNamespace),
		conv.GenAIMainAgentNameKey.String(identity.AgentName),
		conv.GenAIMainAgentIDKey.String(identity.AgentID()),
	}
	if identity.Provider != "" {
		attributes = append(attributes, conv.GenAIProviderNameKey.String(identity.Provider))
	}
	if identity.Model != "" {
		attributes = append(attributes, conv.GenAIRequestModelKey.String(identity.Model))
	}
	return attributes
}
