package env

// Documentation-only registrations for user-configurable runtime settings.
// Keep their defaults aligned with the Go/Python readers; importing this package
// is not required for a runtime or an upstream SDK to read its environment.
var (
	_ = RegisterStringVar("KAGENT_PORT", "8080", "Go ADK listen port; --port takes precedence. The controller injects 80 for managed kagent runtimes. Codex and Claude use a fixed private port.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_CONFIG_DIR", "/config", "Go ADK configuration directory; --filepath takes precedence.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_A2A_MAX_CONTENT_LENGTH", "10485760", "Maximum A2A request size in bytes for Go/Python servers. 0, none, or unlimited disables the limit; invalid values use the default.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_A2A_GRPC_ADDRESS", "[::]:80", "Python ADK gRPC listen address.", ComponentAgentRuntime)
	_ = RegisterStringVar("KAGENT_BASH_VENV_PATH", "", "Virtual environment used for Python skills shell commands; its bin directory is prepended to PATH and VIRTUAL_ENV is set.", ComponentAgentRuntime)
	_ = RegisterBoolVar("KAGENT_OPENAI_AGENTS_NATIVE_TRACING", false, "Keep the OpenAI Agents SDK native tracing processor alongside kagent OpenTelemetry export in the Python OpenAI runtime.", ComponentAgentRuntime)
	_ = RegisterBoolVar("OPENAI_AGENTS_DISABLE_TRACING", false, "Disable OpenAI Agents SDK tracing, including the kagent bridge. The Python OpenAI runtime accepts true or 1.", ComponentAgentRuntime)
)
