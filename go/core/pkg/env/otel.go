package env

// OpenTelemetry environment variables. These are typically set on the controller
// process and forwarded to agent pods.
var (
	OtelTracingEnabled = RegisterBoolVar(
		"OTEL_TRACING_ENABLED",
		false,
		"Enable OpenTelemetry tracing.",
		ComponentController,
	)

	OtelLoggingEnabled = RegisterBoolVar(
		"OTEL_LOGGING_ENABLED",
		false,
		"Enable OpenTelemetry logging.",
		ComponentController,
	)

	OtelExporterOTLPEndpoint = RegisterStringVar(
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"",
		"Default OTLP exporter endpoint for both traces and logs.",
		ComponentController,
	)

	OtelExporterOTLPTracesEndpoint = RegisterStringVar(
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"",
		"OTLP exporter endpoint for traces. Takes precedence over OTEL_EXPORTER_OTLP_ENDPOINT for traces.",
		ComponentController,
	)

	OtelExporterOTLPLogsEndpoint = RegisterStringVar(
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"",
		"OTLP exporter endpoint for logs. Takes precedence over OTEL_EXPORTER_OTLP_ENDPOINT for logs.",
		ComponentController,
	)

	KagentTraceContextKeys = RegisterStringVar(
		"KAGENT_TRACE_CONTEXT_KEYS",
		"",
		"Allowlist of caller-supplied context keys copied from W3C baggage onto every agent span. "+
			"Accepts a comma-separated list of keys, or a JSON array of strings and {from, to} "+
			"objects. Destination names are used as-is; renaming and hashing belong in the "+
			"collector or gateway. Empty (the default) disables promotion.",
		ComponentAgentRuntime,
	)
)
