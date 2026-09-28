package env

// Registrations for user-configurable runtime settings.
// Go runtimes read these accessors. Metadata-only entries describe settings
// consumed by Python runtimes or upstream SDKs.
var (
	KagentPort                = RegisterStringVar("KAGENT_PORT", "", "ADK A2A listen port: the Go HTTP/gRPC listener defaults to 8080; the Python gRPC listener defaults to 80. Explicit Go --port/AppConfig.Port or Python a2a_grpc_address takes precedence. The controller sets 80 for managed kagent runtimes. Python's HTTP --port is separate.", ComponentAgentRuntime)
	KagentConfigDir           = RegisterStringVar("KAGENT_CONFIG_DIR", "/config", "Go ADK configuration directory; --filepath takes precedence.", ComponentAgentRuntime)
	KagentA2AMaxContentLength = RegisterStringVar("KAGENT_A2A_MAX_CONTENT_LENGTH", "10485760", "Maximum A2A request size in bytes for Go/Python servers. 0, none, or unlimited disables the limit; invalid values use the default.", ComponentAgentRuntime)
	_                         = RegisterStringVar("KAGENT_BASH_VENV_PATH", "", "Virtual environment used for Python skills shell commands; its bin directory is prepended to PATH and VIRTUAL_ENV is set.", ComponentAgentRuntime)
	_                         = RegisterBoolVar("KAGENT_OPENAI_AGENTS_NATIVE_TRACING", false, "Keep the OpenAI Agents SDK native tracing processor alongside kagent OpenTelemetry export in the Python OpenAI runtime.", ComponentAgentRuntime)
	_                         = RegisterBoolVar("OPENAI_AGENTS_DISABLE_TRACING", false, "Disable OpenAI Agents SDK tracing, including the kagent bridge. The Python OpenAI runtime accepts true or 1.", ComponentAgentRuntime)
)
