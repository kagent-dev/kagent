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
)
