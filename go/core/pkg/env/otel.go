package env

// Standard SDK overrides apply only to the receiving process. The controller
// compiles managed runtime defaults from config, never from its environment.
func init() {
	components := []Component{ComponentController, ComponentAgentRuntime}
	RegisterBoolVar("OPENAI_AGENTS_DISABLE_TRACING", false, "Disable OpenAI Agents SDK tracing, including the OpenTelemetry bridge.", ComponentAgentRuntime)
	for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
		RegisterStringVar("OTEL_"+signal+"_EXPORTER", "", "Override the file's exporter selection for this process. none disables the signal; an endpoint or file exporter enables OTLP by default.", components...)
		for _, option := range []string{"ENDPOINT", "PROTOCOL", "TIMEOUT", "HEADERS", "COMPRESSION", "CERTIFICATE", "CLIENT_CERTIFICATE", "CLIENT_KEY"} {
			RegisterStringVar("OTEL_EXPORTER_OTLP_"+signal+"_"+option, "", "Signal-specific SDK override; takes precedence over generic OTLP environment settings and file defaults. Timeouts are milliseconds; HTTP endpoints include the signal path.", components...)
		}
	}
	for _, option := range []string{"ENDPOINT", "PROTOCOL", "TIMEOUT", "HEADERS", "COMPRESSION", "CERTIFICATE", "CLIENT_CERTIFICATE", "CLIENT_KEY"} {
		RegisterStringVar("OTEL_EXPORTER_OTLP_"+option, "", "Generic OTLP SDK override for this process, ahead of file defaults. Timeouts are milliseconds. Native SDK support varies.", components...)
	}
	for name, description := range map[string]string{
		"OTEL_SDK_DISABLED":        "Disable all SDK telemetry for this process when true.",
		"OTEL_SERVICE_NAME":        "Override the SDK service name for this process.",
		"OTEL_RESOURCE_ATTRIBUTES": "Comma-separated key=value resource attributes. Override file attributes per key; compiler-owned runtime identity remains authoritative.",
		"OTEL_PROPAGATORS":         "SDK propagator selection; defaults to tracecontext without baggage.",
		"OTEL_TRACES_SAMPLER":      "SDK trace sampler selection.",
		"OTEL_TRACES_SAMPLER_ARG":  "SDK trace sampler argument.",
		"OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION": "SDK histogram aggregation; defaults to base2_exponential_bucket_histogram.",
	} {
		RegisterStringVar(name, "", description, components...)
	}
	for name, description := range map[string]string{
		"OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT": "Override runtime content capture: SPAN_ONLY enables prompts and responses; NO_CONTENT disables capture.",
		"OTEL_SEMCONV_STABILITY_OPT_IN":                      "Python semantic convention opt-in; defaults to gen_ai_latest_experimental,http.",
		"OTEL_PYTHON_FASTAPI_EXCLUDED_URLS":                  "Python inbound HTTP paths left untraced; defaults exclude health and agent-card endpoints.",
		"OTEL_PYTHON_DISABLED_INSTRUMENTATIONS":              "Python instrumentation entry point names to skip, such as httpx or openai.",
	} {
		RegisterStringVar(name, "", description, ComponentAgentRuntime)
	}
}
