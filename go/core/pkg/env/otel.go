package env

// OpenTelemetry environment variables. The controller exports with them and
// forwards them to every agent runtime.
var (
	OtelTracesExporter = RegisterStringVar(
		"OTEL_TRACES_EXPORTER",
		"",
		"Trace exporter, otlp or none. Only otlp is forwarded to agent runtimes.",
		ComponentController,
	)

	OtelMetricsExporter = RegisterStringVar(
		"OTEL_METRICS_EXPORTER",
		"",
		"Metric exporter, otlp or none. Only otlp is forwarded to agent runtimes.",
		ComponentController,
	)

	OtelLogsExporter = RegisterStringVar(
		"OTEL_LOGS_EXPORTER",
		"",
		"Log exporter, otlp or none. Only otlp is forwarded to agent runtimes.",
		ComponentController,
	)

	OtelExporterOTLPEndpoint = RegisterStringVar(
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"",
		"OTLP endpoint for every signal. OTEL_EXPORTER_OTLP_<SIGNAL>_ENDPOINT overrides it for one signal.",
		ComponentController,
	)

	OtelExporterOTLPProtocol = RegisterStringVar(
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"grpc",
		"OTLP protocol, grpc or http/protobuf. OTEL_EXPORTER_OTLP_<SIGNAL>_PROTOCOL overrides it for one signal.",
		ComponentController,
	)

	OtelCaptureMessageContent = RegisterStringVar(
		"OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT",
		"NO_CONTENT",
		"SPAN_ONLY records prompts and responses on agent spans. This may expose sensitive content.",
		ComponentController,
	)

	OtelResourceAttributes = RegisterStringVar(
		"KAGENT_OTEL_RESOURCE_ATTRIBUTES",
		"",
		"Resource attributes, as key=value pairs, added to every agent runtime.",
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
