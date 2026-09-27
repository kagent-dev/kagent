package env

var (
	HTTPBindAddress = RegisterStringVar(
		"HTTP_BIND_ADDRESS", ":8083",
		"Listen address for the controller HTTP, gRPC, A2A, and MCP server.", ComponentController,
	)
	GRPCReflection = RegisterBoolVar(
		"GRPC_REFLECTION", false,
		"Enable gRPC server reflection on the controller.", ComponentController,
	)
	WatchNamespaces = RegisterStringVar(
		"WATCH_NAMESPACES", "",
		"Comma-separated namespaces to watch. Empty watches all namespaces.", ComponentController,
	)
)

// Shared settings read by logging and Kubernetes libraries.
var (
	_ = RegisterStringVar("LOG_LEVEL", "info", "Logging level for the controller, CLI, and Go/Python runtimes: debug, info, warn, or error. Python also accepts standard Python logging levels.", ComponentController)
	_ = RegisterStringVar("KUBECONFIG", "", "Kubernetes client configuration file list for the controller, CLI Kubernetes operations, and tests. When unset, client-go uses its normal in-cluster or user kubeconfig discovery.", ComponentController)
)

// The chart supplies these for Kubernetes expansion in OTEL_RESOURCE_ATTRIBUTES.
var (
	_ = RegisterStringVar("K8S_POD_NAME", "", "Controller pod name, injected by the Helm chart through the Downward API for telemetry.", ComponentController)
	_ = RegisterStringVar("K8S_POD_UID", "", "Controller pod UID, injected by the Helm chart through the Downward API for telemetry.", ComponentController)
	_ = RegisterStringVar("K8S_NODE_NAME", "", "Controller node name, injected by the Helm chart through the Downward API for telemetry.", ComponentController)
)
