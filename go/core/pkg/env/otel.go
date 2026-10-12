package env

const (
	otlpDescription       = "Generic OTLP SDK override for this process, ahead of file defaults. Timeouts are milliseconds. Native SDK support varies."
	otlpSignalDescription = "Signal-specific SDK override; takes precedence over generic OTLP environment settings and file defaults. Timeouts are milliseconds; HTTP endpoints include the signal path."
)

// OTELSignalVars are the registered overrides read when resolving one signal.
// The SDK consumes the remaining exporter options directly.
type OTELSignalVars struct {
	Exporter, Endpoint, Protocol, Timeout StringVar
}

var (
	OTELTraces                             = registerOTELSignal("TRACES")
	OTELMetrics                            = registerOTELSignal("METRICS")
	OTELLogs                               = registerOTELSignal("LOGS")
	OTELSDKDisabled                        = RegisterStringVar("OTEL_SDK_DISABLED", "", "Disable all SDK telemetry for this process when true.", ComponentController, ComponentAgentRuntime)
	OTELResourceAttributes                 = RegisterStringVar("OTEL_RESOURCE_ATTRIBUTES", "", "Comma-separated key=value resource attributes. Override file attributes per key; compiler-owned runtime identity remains authoritative.", ComponentController, ComponentAgentRuntime)
	OTELPropagators                        = RegisterStringVar("OTEL_PROPAGATORS", "", "SDK propagator selection; defaults to tracecontext without baggage.", ComponentController, ComponentAgentRuntime)
	OTELExporterOTLPEndpoint               = RegisterStringVar("OTEL_EXPORTER_OTLP_ENDPOINT", "", otlpDescription, ComponentController, ComponentAgentRuntime)
	OTELExporterOTLPProtocol               = RegisterStringVar("OTEL_EXPORTER_OTLP_PROTOCOL", "", otlpDescription, ComponentController, ComponentAgentRuntime)
	OTELExporterOTLPTimeout                = RegisterStringVar("OTEL_EXPORTER_OTLP_TIMEOUT", "", otlpDescription, ComponentController, ComponentAgentRuntime)
	OTELExporterOTLPCompression            = RegisterStringVar("OTEL_EXPORTER_OTLP_COMPRESSION", "", otlpDescription, ComponentController, ComponentAgentRuntime)
	OTELMetricsDefaultHistogramAggregation = RegisterStringVar("OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION", "", "SDK histogram aggregation; defaults to base2_exponential_bucket_histogram.", ComponentController, ComponentAgentRuntime)
	OTELGenAICaptureMessageContent         = RegisterStringVar("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", "", "Override runtime content capture: SPAN_ONLY enables prompts and responses; NO_CONTENT disables capture.", ComponentAgentRuntime)

	OTELLogUserPrompts         = RegisterStringVar("OTEL_LOG_USER_PROMPTS", "", "Claude Code prompt capture flag, managed by the harness content-capture configuration.", ComponentAgentRuntime)
	OTELLogToolDetails         = RegisterStringVar("OTEL_LOG_TOOL_DETAILS", "", "Claude Code tool-detail capture flag, managed by the harness content-capture configuration.", ComponentAgentRuntime)
	OTELLogToolContent         = RegisterStringVar("OTEL_LOG_TOOL_CONTENT", "", "Claude Code tool-content capture flag, managed by the harness content-capture configuration.", ComponentAgentRuntime)
	OTELLogAssistantResponses  = RegisterStringVar("OTEL_LOG_ASSISTANT_RESPONSES", "", "Claude Code response capture flag, managed by the harness content-capture configuration.", ComponentAgentRuntime)
	OTELLogRawAPIBodies        = RegisterStringVar("OTEL_LOG_RAW_API_BODIES", "", "Claude Code raw API body capture flag, managed by the harness captureRawAPIBodies configuration.", ComponentAgentRuntime)
	OTELExporterPrometheusHost = RegisterStringVar("OTEL_EXPORTER_PROMETHEUS_HOST", "", "Claude Code telemetry readiness listener host, managed by the harness.", ComponentAgentRuntime)
	OTELExporterPrometheusPort = RegisterStringVar("OTEL_EXPORTER_PROMETHEUS_PORT", "", "Claude Code telemetry readiness listener port, managed by the harness.", ComponentAgentRuntime)
)

func registerOTELSignal(signal string) OTELSignalVars {
	components := []Component{ComponentController, ComponentAgentRuntime}
	prefix := "OTEL_EXPORTER_OTLP_" + signal + "_"
	for _, option := range []string{"HEADERS", "COMPRESSION", "CERTIFICATE", "CLIENT_CERTIFICATE", "CLIENT_KEY"} {
		RegisterStringVar(prefix+option, "", otlpSignalDescription, components...)
	}
	return OTELSignalVars{
		Exporter: RegisterStringVar("OTEL_"+signal+"_EXPORTER", "", "Override the file's exporter selection for this process. none disables the signal; an endpoint or file exporter enables OTLP by default.", components...),
		Endpoint: RegisterStringVar(prefix+"ENDPOINT", "", otlpSignalDescription, components...),
		Protocol: RegisterStringVar(prefix+"PROTOCOL", "", otlpSignalDescription, components...),
		Timeout:  RegisterStringVar(prefix+"TIMEOUT", "", otlpSignalDescription, components...),
	}
}

// Standard SDK overrides apply only to the receiving process. The controller
// compiles managed runtime defaults from config, never from its environment.
func init() {
	components := []Component{ComponentController, ComponentAgentRuntime}
	RegisterBoolVar("OPENAI_AGENTS_DISABLE_TRACING", false, "Disable OpenAI Agents SDK tracing, including the OpenTelemetry bridge.", ComponentAgentRuntime)
	for _, option := range []string{"HEADERS", "CERTIFICATE", "CLIENT_CERTIFICATE", "CLIENT_KEY"} {
		RegisterStringVar("OTEL_EXPORTER_OTLP_"+option, "", otlpDescription, components...)
	}
	for name, description := range map[string]string{
		"OTEL_SERVICE_NAME":       "Override the SDK service name for this process.",
		"OTEL_TRACES_SAMPLER":     "SDK trace sampler selection.",
		"OTEL_TRACES_SAMPLER_ARG": "SDK trace sampler argument.",
	} {
		RegisterStringVar(name, "", description, components...)
	}
	for name, description := range map[string]string{
		"OTEL_SEMCONV_STABILITY_OPT_IN":         "Python semantic convention opt-in; defaults to gen_ai_latest_experimental,http.",
		"OTEL_PYTHON_FASTAPI_EXCLUDED_URLS":     "Python inbound HTTP paths left untraced; defaults exclude health and agent-card endpoints.",
		"OTEL_PYTHON_DISABLED_INSTRUMENTATIONS": "Python instrumentation entry point names to skip, such as httpx or openai.",
	} {
		RegisterStringVar(name, "", description, ComponentAgentRuntime)
	}
}
