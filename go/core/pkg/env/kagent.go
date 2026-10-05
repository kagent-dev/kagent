package env

// Core kagent environment variables used by the controller and agent runtime.
var (
	LeaderElect = RegisterBoolVar(
		"KAGENT_LEADER_ELECT",
		true,
		"Enable controller leader election, including during single-replica rolling updates. Required for sandbox lifecycle coordination.",
		ComponentController,
	)

	MetricsBindAddress = RegisterStringVar(
		"KAGENT_METRICS_BIND_ADDRESS",
		"0",
		"Address the controller-runtime metrics server binds to, e.g. :8080. "+
			"\"0\" (the default) serves no metrics, so an installation that does not "+
			"set this is unchanged. The Helm chart renders this variable, and its "+
			"ServiceMonitor, from controller.metrics.",
		ComponentController,
	)

	MetricsSecure = RegisterBoolVar(
		"KAGENT_METRICS_SECURE",
		false,
		"Serve the metrics endpoint over HTTPS with authentication and authorization. "+
			"A scraper then needs a token bound to the metrics-reader ClusterRole.",
		ComponentController,
	)

	KagentNamespace = RegisterStringVar(
		"KAGENT_NAMESPACE",
		"kagent",
		"Kubernetes namespace where kagent resources are deployed. The controller injects the agent namespace into runtimes; Python runtimes require it.",
		ComponentController, ComponentAgentRuntime,
	)

	KagentControllerName = RegisterStringVar(
		"KAGENT_CONTROLLER_NAME",
		"kagent-controller",
		"Name of the kagent controller service.",
		ComponentController,
	)

	// Standalone runtime settings supplied by the controller in managed runtimes.

	KagentName = RegisterStringVar(
		"KAGENT_NAME",
		"",
		"Agent name for standalone runtimes. Required by Python runtimes; supplied by the controller in managed runtimes.",
		ComponentAgentRuntime,
	)

	KagentAPIURL = RegisterStringVar(
		"KAGENT_API_URL",
		"",
		"Base URL for kagent control-plane API calls. Required by Python runtimes and supplied by the controller in managed runtimes; also used as the E2E test URL when KAGENT_E2E_API_URL is unset.",
		ComponentAgentRuntime, ComponentTesting,
	)

	KagentGatewayURL = RegisterStringVar(
		"KAGENT_GATEWAY_URL",
		"",
		"Base URL for A2A and MCP traffic. The controller falls back to http://127.0.0.1:8083; Python runtimes require a value.",
		ComponentAgentRuntime, ComponentController,
	)

	A2APushAllowPrivateNetworks = RegisterBoolVar(
		"KAGENT_A2A_PUSH_ALLOW_PRIVATE_NETWORKS",
		false,
		"Allow A2A push callbacks to private, loopback, and link-local destinations.",
		ComponentController,
	)
	A2APushAllowHTTP = RegisterBoolVar(
		"KAGENT_A2A_PUSH_ALLOW_HTTP",
		false,
		"Allow HTTP A2A push callbacks. HTTPS is required by default.",
		ComponentController,
	)
	A2APushSigningSeed = RegisterStringVar(
		"KAGENT_A2A_PUSH_SIGNING_SEED",
		"",
		"Base64 Ed25519 seed shared by controller replicas for push notification JWTs. Supply through a Kubernetes Secret.",
		ComponentController,
	)
	A2APushIssuer = RegisterStringVar(
		"KAGENT_A2A_PUSH_ISSUER",
		"",
		"Stable issuer URL for push notification JWTs. Defaults to KAGENT_GATEWAY_URL.",
		ComponentController,
	)

	KagentSkillsFolder = RegisterStringVar(
		"KAGENT_SKILLS_FOLDER",
		"/skills",
		"Skills directory for standalone Python skills tools. The Python ADK adds skills tools when set; managed Go ADK runtimes use their compiled skill configuration.",
		ComponentAgentRuntime,
	)

	KagentPropagateToken = RegisterStringVar(
		"KAGENT_PROPAGATE_TOKEN",
		"",
		"Set to true to propagate authentication tokens to downstream services. Unset or any other value disables propagation.",
		ComponentAgentRuntime,
	)

	KagentEnableFileSearchTools = RegisterBoolVar(
		"KAGENT_ENABLE_FILE_SEARCH_TOOLS",
		false,
		"When true, t, or 1 (case-insensitive), enables the list_files and grep_file skills tools, which let an agent "+
			"enumerate and search the filesystem under its session/skills roots without a "+
			"shell. Disabled by default; set in Harness env to opt in.",
		ComponentAgentRuntime,
	)

	StsWellKnownURI = RegisterStringVar(
		"KAGENT_STS_WELL_KNOWN_URI",
		"",
		"Well-known endpoint for the Security Token Service (STS) used for token exchange.",
		ComponentAgentRuntime,
	)

	KagentSTSResource = RegisterStringVar(
		"KAGENT_STS_RESOURCE",
		"",
		"Comma-separated RFC 8707 resource indicators sent on STS token-exchange requests to scope issued tokens to target backends.",
		ComponentAgentRuntime,
	)

	KagentSTSAudience = RegisterStringVar(
		"KAGENT_STS_AUDIENCE",
		"",
		"Comma-separated RFC 8693 audiences sent on STS token-exchange requests. Alternate to KAGENT_STS_RESOURCE for servers that key on audience.",
		ComponentAgentRuntime,
	)

	DatabaseVectorEnabled = RegisterBoolVar(
		"KAGENT_DATABASE_VECTOR_ENABLED",
		false,
		"Enable vector database migrations and vector-backed database functionality. The controller defaults to false. When unset in the CLI, migrations read the controller ConfigMap and fall back to true if it is unavailable.",
		ComponentDatabase, ComponentController, ComponentCLI,
	)

	SkipMigrations = RegisterBoolVar(
		"KAGENT_SKIP_MIGRATIONS",
		false,
		"Verify required database migrations at startup without applying them.",
		ComponentDatabase, ComponentController,
	)
)
