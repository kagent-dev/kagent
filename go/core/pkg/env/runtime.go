package env

// Documentation-only registrations for settings consumed outside the controller.
// Keep their defaults aligned with the Go/Python readers; importing this package
// is not required for a runtime or an upstream SDK to read its environment.
var (
	_ = RegisterStringVar("PORT", "8080", "Go ADK listen port; --port takes precedence. The controller injects 80 for managed kagent runtimes. Codex and Claude use a fixed private port.", ComponentAgentRuntime)
	_ = RegisterStringVar("CONFIG_DIR", "/config", "Go ADK configuration directory; --filepath takes precedence.", ComponentAgentRuntime)
	_ = RegisterStringVar("A2A_MAX_CONTENT_LENGTH", "10485760", "Maximum A2A request size in bytes for Go/Python servers. 0, none, or unlimited disables the limit; invalid values use the default.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_A2A_GRPC_ADDRESS", "[::]:80", "Python ADK gRPC listen address.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_CONFIG_JSON", "", "Compiled harness configuration JSON injected by the controller. Required by Codex and Claude; materialized to config.json by the ADKs when present.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_AGENT_CARD_JSON", "", "A2A agent card JSON injected by the controller. Required by Codex and Claude; materialized to agent-card.json by the ADKs when present.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_TOKEN", "", "Runtime authentication token, materialized to /var/run/secrets/tokens/kagent-token by the ADKs when present.", ComponentAgentRuntime)
	_ = RegisterStringVar("UVICORN_LOG_LEVEL", "", "Python ADK HTTP server log level. Falls back to LOG_LEVEL, then info.", ComponentAgentRuntime)
	_ = RegisterStringVar("BASH_VENV_PATH", "", "Virtual environment used for Python skills shell commands; its bin directory is prepended to PATH and VIRTUAL_ENV is set.", ComponentAgentRuntime)
	_ = RegisterBoolVar("KAGENT_OPENAI_AGENTS_NATIVE_TRACING", false, "Keep the OpenAI Agents SDK native tracing processor alongside kagent OpenTelemetry export in the Python OpenAI runtime.", ComponentAgentRuntime)
	_ = RegisterBoolVar("OPENAI_AGENTS_DISABLE_TRACING", false, "Disable OpenAI Agents SDK tracing, including the kagent bridge. The Python OpenAI runtime accepts true or 1.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_CREDENTIAL_<NAME>", "", "Controller-generated credential variables referenced by compiled ADK configuration; names depend on the configured tools and models.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_CLAUDE_MCP_CREDENTIAL_<NAME>", "", "Controller-generated Claude MCP header credentials; names depend on the configured tools.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_CODEX_MCP_CREDENTIAL_<NAME>", "", "Controller-generated Codex MCP header credentials; names depend on the configured tools.", ComponentAgentRuntime)
)

// Harness-owned process environment. These are outputs of harness compilation or
// runtime setup, not independent switches for operators to configure.
var (
	_ = RegisterStringVar("CODEX_HOME", "", "Codex home directory set by the harness under its durable private state.", ComponentAgentRuntime)
	_ = RegisterStringVar("CLAUDE_CONFIG_DIR", "", "Claude configuration directory set by the harness under its durable private state.", ComponentAgentRuntime)
	_ = RegisterStringVar("DISABLE_UPDATES", "1", "Set by the Claude harness to disable native automatic updates.", ComponentAgentRuntime)
	_ = RegisterStringVar("IS_SANDBOX", "1", "Set by the controller for managed Claude runtimes.", ComponentAgentRuntime)
	_ = RegisterStringVar("CLAUDE_CODE_USE_BEDROCK", "", "Set to 1 by the controller when the Claude ModelConfig uses Bedrock.", ComponentAgentRuntime)
	_ = RegisterStringVar("CLAUDE_CODE_USE_VERTEX", "", "Set to 1 by the controller when the Claude ModelConfig uses Vertex AI.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_CLAUDE_GOOGLE_CREDENTIALS_JSON", "", "Vertex service-account JSON injected into Claude and materialized as the GOOGLE_APPLICATION_CREDENTIALS file; removed before launching Claude.", ComponentAgentRuntime)
	_ = RegisterStringVar("CLAUDE_CODE_ENABLE_TELEMETRY", "", "Set to 1 by the Claude harness when telemetry is enabled.", ComponentAgentRuntime)
	_ = RegisterStringVar("CLAUDE_CODE_ENHANCED_TELEMETRY_BETA", "", "Set to 1 by the Claude harness when telemetry is enabled.", ComponentAgentRuntime)
	_ = RegisterStringVar("OTEL_LOG_USER_PROMPTS", "", "Set to 1 by the Claude harness when content capture is enabled.", ComponentAgentRuntime)
	_ = RegisterStringVar("OTEL_LOG_TOOL_DETAILS", "", "Set to 1 by the Claude harness when content capture is enabled.", ComponentAgentRuntime)
	_ = RegisterStringVar("OTEL_LOG_TOOL_CONTENT", "", "Set to 1 by the Claude harness when trace export and content capture are enabled.", ComponentAgentRuntime)
	_ = RegisterStringVar("OTEL_LOG_ASSISTANT_RESPONSES", "", "Set to 1 by the Claude harness when log export and content capture are enabled.", ComponentAgentRuntime)
	_ = RegisterStringVar("OTEL_LOG_RAW_API_BODIES", "", "Set to 1 for Claude when KAGENT_OTEL_CAPTURE_RAW_API_BODIES enables raw body capture.", ComponentAgentRuntime)
	_ = RegisterStringVar("OTEL_EXPORTER_PROMETHEUS_HOST", "", "Private loopback Prometheus host assigned by the Claude driver for native telemetry readiness checks.", ComponentAgentRuntime)
	_ = RegisterStringVar("OTEL_EXPORTER_PROMETHEUS_PORT", "", "Private Prometheus port assigned by the Claude driver for native telemetry readiness checks.", ComponentAgentRuntime)
	_ = RegisterStringVar("TRACEPARENT", "", "W3C trace context supplied per invocation to the native Claude process.", ComponentAgentRuntime)
	_ = RegisterStringVar("TRACESTATE", "", "W3C trace state supplied per invocation to the native Claude process.", ComponentAgentRuntime)
)

// Substrate replaces these with the managed egress trust bundle.
var (
	_ = RegisterStringVar("SSL_CERT_FILE", "", "Managed runtime and sandbox CA bundle, injected by Substrate for egress TLS trust.", ComponentAgentRuntime)
	_ = RegisterStringVar("SSL_CERT_DIR", "", "Managed agent runtime CA directory, injected by Substrate for egress TLS trust.", ComponentAgentRuntime)
	_ = RegisterStringVar("REQUESTS_CA_BUNDLE", "", "Managed runtime and sandbox CA bundle for Python requests, injected by Substrate.", ComponentAgentRuntime)
	_ = RegisterStringVar("AWS_CA_BUNDLE", "", "Managed runtime and sandbox CA bundle for AWS clients, injected by Substrate.", ComponentAgentRuntime)
	_ = RegisterStringVar("CURL_CA_BUNDLE", "", "Managed runtime and sandbox CA bundle for curl, injected by Substrate.", ComponentAgentRuntime)
	_ = RegisterStringVar("NODE_EXTRA_CA_CERTS", "", "Managed runtime and sandbox CA bundle for Node.js, injected by Substrate.", ComponentAgentRuntime)
	_ = RegisterStringVar("GIT_SSL_CAINFO", "", "Managed runtime and sandbox CA bundle for Git, injected by Substrate.", ComponentAgentRuntime)
	_ = RegisterStringVar("DOCKER_CONFIG", "", "Docker registry credentials directory used by skills-init for OCI pulls; mounted by the controller when image pull secrets are configured.", ComponentAgentRuntime)
)
