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

	// OtelLoggingEnabled controls agent-side OpenTelemetry logging (gen_ai
	// input/output records). It is forwarded verbatim to agent pods (see
	// collectOtelEnvFromProcess), so it must NOT double as the controller's own
	// log-export switch — that is ControllerOtlpLogsEnabled below.
	OtelLoggingEnabled = RegisterBoolVar(
		"OTEL_LOGGING_ENABLED",
		false,
		"Enable OpenTelemetry logging on agents (gen_ai input/output records).",
		ComponentController,
	)

	// ControllerOtlpLogsEnabled turns on OTLP export of the controller's OWN
	// logs. It is intentionally NOT prefixed OTEL_ so it is not propagated to
	// agent pods, keeping controller log export decoupled from agent gen_ai
	// logging (OtelLoggingEnabled).
	ControllerOtlpLogsEnabled = RegisterBoolVar(
		"KAGENT_CONTROLLER_OTLP_LOGS_ENABLED",
		false,
		"Enable OTLP export of the controller's own logs.",
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
