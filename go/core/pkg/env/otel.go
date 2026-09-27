package env

// OpenTelemetry environment variables. The controller forwards supported signal
// settings to runtimes; SDK-only settings below apply to the receiving process.
var (
	OtelTracesExporter = RegisterStringVar(
		"OTEL_TRACES_EXPORTER",
		"",
		"Trace exporter, otlp or none. Managed runtime export requires explicit otlp and an endpoint; unset disables forwarding. Standalone SDKs may default to otlp.",
		ComponentController, ComponentAgentRuntime,
	)

	OtelMetricsExporter = RegisterStringVar(
		"OTEL_METRICS_EXPORTER",
		"",
		"Metric exporter, otlp or none. Managed runtime export requires explicit otlp and an endpoint; unset disables forwarding. Standalone SDKs may default to otlp.",
		ComponentController, ComponentAgentRuntime,
	)

	OtelLogsExporter = RegisterStringVar(
		"OTEL_LOGS_EXPORTER",
		"",
		"Log exporter, otlp or none. Managed runtime export requires explicit otlp and an endpoint; unset disables forwarding. Standalone SDKs may default to otlp.",
		ComponentController, ComponentAgentRuntime,
	)

	OtelExporterOTLPEndpoint = RegisterStringVar(
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"",
		"OTLP endpoint for every signal. `OTEL_EXPORTER_OTLP_<SIGNAL>_ENDPOINT` overrides it for one signal.",
		ComponentController, ComponentAgentRuntime,
	)

	OtelExporterOTLPProtocol = RegisterStringVar(
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"grpc",
		"OTLP protocol, grpc or http/protobuf. `OTEL_EXPORTER_OTLP_<SIGNAL>_PROTOCOL` overrides it for one signal.",
		ComponentController, ComponentAgentRuntime,
	)

	OtelCaptureMessageContent = RegisterStringVar(
		"OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT",
		"NO_CONTENT",
		"SPAN_ONLY records prompts and responses on agent spans. NO_CONTENT disables capture. Managed runtimes support these two modes; standalone Python ADK also recognizes SPAN_AND_EVENT. Captured content may be sensitive.",
		ComponentController, ComponentAgentRuntime,
	)

	OtelResourceAttributes = RegisterStringVar(
		"KAGENT_OTEL_RESOURCE_ATTRIBUTES",
		"",
		"Resource attributes, as key=value pairs, added to every agent runtime.",
		ComponentController,
	)
)

var (
	OtelSDKDisabled                                    = RegisterStringVar("OTEL_SDK_DISABLED", "false", "Disable SDK telemetry and forwarding to managed runtimes when true (case-insensitive). Other values are treated as false.", ComponentController, ComponentAgentRuntime)
	OtelServiceName                                    = RegisterStringVar("OTEL_SERVICE_NAME", "", "SDK service name for the current process. Defaults to kagent-controller in the controller; the controller supplies the agent name to managed runtimes.", ComponentController, ComponentAgentRuntime)
	OtelSDKResourceAttributes                          = RegisterStringVar("OTEL_RESOURCE_ATTRIBUTES", "", "Comma-separated SDK resource attributes for the current process. Helm injects controller identity; kagent constructs runtime identity separately. Use KAGENT_OTEL_RESOURCE_ATTRIBUTES for attributes shared with managed agents.", ComponentController, ComponentAgentRuntime)
	OtelExporterOTLPTimeout                            = RegisterStringVar("OTEL_EXPORTER_OTLP_TIMEOUT", "", "OTLP export timeout in milliseconds. The controller forwards positive integers; absent values use each SDK's default (normally 10000 ms).", ComponentController, ComponentAgentRuntime)
	OtelExporterOTLPTracesEndpoint                     = RegisterStringVar("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "", "Trace endpoint override. Falls back to OTEL_EXPORTER_OTLP_ENDPOINT; an HTTP override must include its signal path.", ComponentController, ComponentAgentRuntime)
	OtelExporterOTLPMetricsEndpoint                    = RegisterStringVar("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "", "Metric endpoint override. Falls back to OTEL_EXPORTER_OTLP_ENDPOINT; an HTTP override must include its signal path.", ComponentController, ComponentAgentRuntime)
	OtelExporterOTLPLogsEndpoint                       = RegisterStringVar("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "", "Log endpoint override. Falls back to OTEL_EXPORTER_OTLP_ENDPOINT; an HTTP override must include its signal path.", ComponentController, ComponentAgentRuntime)
	OtelExporterOTLPTracesProtocol                     = RegisterStringVar("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "", "Trace protocol override: grpc or http/protobuf. Falls back to OTEL_EXPORTER_OTLP_PROTOCOL.", ComponentController, ComponentAgentRuntime)
	OtelExporterOTLPMetricsProtocol                    = RegisterStringVar("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "", "Metric protocol override: grpc or http/protobuf. Falls back to OTEL_EXPORTER_OTLP_PROTOCOL.", ComponentController, ComponentAgentRuntime)
	OtelExporterOTLPLogsProtocol                       = RegisterStringVar("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "", "Log protocol override: grpc or http/protobuf. Falls back to OTEL_EXPORTER_OTLP_PROTOCOL.", ComponentController, ComponentAgentRuntime)
	_                                                  = RegisterStringVar("OTEL_EXPORTER_OTLP_TRACES_TIMEOUT", "", "Trace SDK timeout in milliseconds, overriding OTEL_EXPORTER_OTLP_TIMEOUT. Not forwarded by the controller.", ComponentController, ComponentAgentRuntime)
	_                                                  = RegisterStringVar("OTEL_EXPORTER_OTLP_METRICS_TIMEOUT", "", "Metric SDK timeout in milliseconds, overriding OTEL_EXPORTER_OTLP_TIMEOUT. Not forwarded by the controller.", ComponentController, ComponentAgentRuntime)
	_                                                  = RegisterStringVar("OTEL_EXPORTER_OTLP_LOGS_TIMEOUT", "", "Log SDK timeout in milliseconds, overriding OTEL_EXPORTER_OTLP_TIMEOUT. Not forwarded by the controller.", ComponentController, ComponentAgentRuntime)
	OtelPropagators                                    = RegisterStringVar("OTEL_PROPAGATORS", "tracecontext", "SDK trace propagators. Kagent defaults to W3C tracecontext without baggage and supplies that default to managed runtimes.", ComponentController, ComponentAgentRuntime)
	OtelExporterOTLPCompression                        = RegisterStringVar("OTEL_EXPORTER_OTLP_COMPRESSION", "gzip", "OTLP compression default applied by kagent. The native Codex process has this variable removed because its exporter does not support gzip.", ComponentController, ComponentAgentRuntime)
	OtelExporterOTLPMetricsDefaultHistogramAggregation = RegisterStringVar("OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION", "base2_exponential_bucket_histogram", "Default SDK histogram aggregation applied by kagent and supplied to managed runtimes.", ComponentController, ComponentAgentRuntime)
	OtelCaptureRawAPIBodies                            = RegisterBoolVar("KAGENT_OTEL_CAPTURE_RAW_API_BODIES", false, "Set to true, t, or 1 (case-insensitive) to enable native Claude raw API body logging when log export is enabled. Independent of span content capture; bodies may contain sensitive data.", ComponentController)
	OtelMaxCaptureBytes                                = RegisterIntVar("KAGENT_OTEL_MAX_CAPTURE_BYTES", 16384, "Per-input/output content capture budget in bytes when capture is enabled. Valid values are 1 through 65536; absent or invalid values use 16384.", ComponentController)
	_                                                  = RegisterStringVar("ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN", "2", "Python Google ADK telemetry schema version; set by kagent when absent.", ComponentAgentRuntime)
	_                                                  = RegisterStringVar("OTEL_SEMCONV_STABILITY_OPT_IN", "gen_ai_latest_experimental", "Python Google ADK semantic-convention opt-in; set by kagent when absent.", ComponentAgentRuntime)
	_                                                  = RegisterStringVar("ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS", "", "Python Google ADK span content capture. When absent, derived from OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT (true for SPAN_ONLY or SPAN_AND_EVENT, false otherwise).", ComponentAgentRuntime)
)
