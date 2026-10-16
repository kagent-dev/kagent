package env

import "time"

const (
	AuthModeInsecure     = "insecure"
	AuthModeTrustedProxy = "trusted-proxy"

	AuthorizationModeNoop       = "noop"
	AuthorizationModeKubernetes = "kubernetes"
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
	AuthorizationMode = RegisterStringVar(
		"KAGENT_AUTHORIZATION_MODE", AuthorizationModeNoop,
		"Controller authorization mode: noop permits every authenticated caller; kubernetes asks the API server with a SubjectAccessReview for the caller's user and groups, so Kubernetes RBAC on the kagent resources decides what each caller sees and runs. The reviewed username is the authenticated user ID (KAGENT_AUTH_USER_ID_CLAIM), so point that claim at the API server's --oidc-username-claim.", ComponentController,
	)
	AuthorizationUsernamePrefix = RegisterStringVar(
		"KAGENT_AUTHORIZATION_USERNAME_PREFIX", "",
		"Prefix put before the authenticated user ID in kubernetes authorization mode, as the API server's --oidc-username-prefix.", ComponentController,
	)
	AuthorizationGroupsClaim = RegisterStringVar(
		"KAGENT_AUTHORIZATION_GROUPS_CLAIM", "",
		"JWT claim the trusted-proxy authentication reads the caller's groups from, as the API server's --oidc-groups-claim; the kubernetes authorization mode reviews the caller with them, and an agent's call carries them for its user. Empty reviews the caller with no group beyond system:authenticated.", ComponentController,
	)
	AuthorizationGroupsPrefix = RegisterStringVar(
		"KAGENT_AUTHORIZATION_GROUPS_PREFIX", "",
		"Prefix put before every Kubernetes group in kubernetes authorization mode, as the API server's --oidc-groups-prefix.", ComponentController,
	)
	AuthorizationNamespaceSelector = RegisterStringVar(
		"KAGENT_AUTHORIZATION_NAMESPACE_SELECTOR", "",
		"Label selector of the namespaces a collection read ranges over in kubernetes authorization mode when the controller watches every namespace. Empty ranges over every namespace; a controller that watches KAGENT_WATCH_NAMESPACES ranges over those.", ComponentController,
	)
	AuthorizationIncludeNamespaces = RegisterStringVar(
		"KAGENT_AUTHORIZATION_INCLUDE_NAMESPACES", "",
		"Comma-separated namespaces a collection read ranges over in kubernetes authorization mode beside those KAGENT_AUTHORIZATION_NAMESPACE_SELECTOR selects, whatever their labels.", ComponentController,
	)
	AuthorizationServiceAccountTokenAudience = RegisterStringVar(
		"KAGENT_AUTHORIZATION_SERVICE_ACCOUNT_TOKEN_AUDIENCE", "kagent",
		"Audience a ServiceAccount token must carry in kubernetes authorization mode: a direct call whose token's sub names a ServiceAccount is authenticated with a TokenReview for this audience and reviewed as that ServiceAccount. It must not be empty.", ComponentController,
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
	ScheduledRunPollInterval = RegisterDurationVar(
		"KAGENT_SCHEDULED_RUN_POLL_INTERVAL", time.Second,
		"Interval between reserving due scheduled runs. Must be positive; occurrences more than 30 seconds late are skipped.", ComponentController,
	)
	ScheduledRunExecutionPollInterval = RegisterDurationVar(
		"KAGENT_SCHEDULED_RUN_EXECUTION_POLL_INTERVAL", time.Second,
		"Interval between scheduled execution reconciliation attempts. Must be positive; longer intervals delay dispatch, status updates, deadline enforcement, and cleanup.", ComponentController,
	)
	RuntimeRevisionGCInterval = RegisterDurationVar(
		"KAGENT_RUNTIME_REVISION_GC_INTERVAL", time.Minute,
		"Interval between unreferenced runtime revision cleanup sweeps. Must be positive.", ComponentController,
	)
)

// Shared settings read by logging and Kubernetes libraries.
var (
	LogLevel   = RegisterStringVar("KAGENT_LOG_LEVEL", "info", "Logging level for the controller, CLI, and Go/Python runtimes, including the Python ADK HTTP server: debug, info, warn, or error. Python also accepts standard Python logging levels.", ComponentController, ComponentCLI, ComponentAgentRuntime)
	Kubeconfig = RegisterStringVar("KUBECONFIG", "", "Kubernetes client configuration file list for the controller, CLI Kubernetes operations, and tests. When unset, client-go uses its normal in-cluster or user kubeconfig discovery.", ComponentController, ComponentCLI, ComponentTesting)
)
