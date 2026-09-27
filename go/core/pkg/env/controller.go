package env

const (
	AuthModeInsecure     = "insecure"
	AuthModeTrustedProxy = "trusted-proxy"
)

var (
	AuthMode = RegisterStringVar(
		"KAGENT_AUTH_MODE", AuthModeInsecure,
		"Controller authentication mode: insecure or trusted-proxy. trusted-proxy requires an upstream credential-validating proxy and network isolation preventing bypass.", ComponentController,
	)
	AuthUserIDClaim = RegisterStringVar(
		"KAGENT_AUTH_USER_ID_CLAIM", "",
		"JWT claim used for the caller identity in trusted-proxy mode. Empty uses sub; a missing or empty custom claim falls back to sub.", ComponentController,
	)
	HTTPBindAddress = RegisterStringVar(
		"KAGENT_HTTP_BIND_ADDRESS", ":8083",
		"Listen address for the controller HTTP, gRPC, A2A, and MCP server.", ComponentController,
	)
	GRPCReflection = RegisterBoolVar(
		"KAGENT_GRPC_REFLECTION", false,
		"Enable gRPC server reflection on the controller.", ComponentController,
	)
	WatchNamespaces = RegisterStringVar(
		"KAGENT_WATCH_NAMESPACES", "",
		"Comma-separated namespaces to watch. Empty watches all namespaces.", ComponentController,
	)
)

// Shared settings read by logging and Kubernetes libraries.
var (
	LogLevel   = RegisterStringVar("KAGENT_LOG_LEVEL", "info", "Logging level for the controller, CLI, and Go/Python runtimes, including the Python ADK HTTP server: debug, info, warn, or error. Python also accepts standard Python logging levels.", ComponentController, ComponentCLI, ComponentAgentRuntime)
	Kubeconfig = RegisterStringVar("KUBECONFIG", "", "Kubernetes client configuration file list for the controller, CLI Kubernetes operations, and tests. When unset, client-go uses its normal in-cluster or user kubeconfig discovery.", ComponentController, ComponentCLI, ComponentTesting)
)
