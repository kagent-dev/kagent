package env

// Core kagent environment variables used by the controller and agent runtime.
var (
	LeaderElect = RegisterBoolVar(
		"LEADER_ELECT",
		true,
		"Enable controller leader election, including during single-replica rolling updates. Required for sandbox lifecycle coordination.",
		ComponentController,
	)

	MetricsBindAddress = RegisterStringVar(
		"METRICS_BIND_ADDRESS",
		"0",
		"Address the controller-runtime metrics server binds to, e.g. :8080. "+
			"\"0\" (the default) serves no metrics, so an installation that does not "+
			"set this is unchanged. The Helm chart renders this variable, and its "+
			"ServiceMonitor, from controller.metrics.",
		ComponentController,
	)

	MetricsSecure = RegisterBoolVar(
		"METRICS_SECURE",
		false,
		"Serve the metrics endpoint over HTTPS with authentication and authorization. "+
			"A scraper then needs a token bound to the metrics-reader ClusterRole.",
		ComponentController,
	)

	KagentNamespace = RegisterStringVar(
		"KAGENT_NAMESPACE",
		"kagent",
		"Kubernetes namespace where kagent resources are deployed. The controller injects the agent namespace into runtimes; Python runtimes require it.",
		ComponentController,
	)

	KagentControllerName = RegisterStringVar(
		"KAGENT_CONTROLLER_NAME",
		"kagent-controller",
		"Name of the kagent controller service.",
		ComponentController,
	)

	// Variables injected into agent runtimes (not read by the controller itself).

	KagentName = RegisterStringVar(
		"KAGENT_NAME",
		"",
		"Name of the agent, injected by the controller. Required by Python runtimes.",
		ComponentAgentRuntime,
	)

	KagentAPIURL = RegisterStringVar(
		"KAGENT_API_URL",
		"",
		"Base URL for kagent control-plane API calls, injected into managed runtimes. Required by Python runtimes.",
		ComponentAgentRuntime,
	)

	KagentGatewayURL = RegisterStringVar(
		"KAGENT_GATEWAY_URL",
		"",
		"Base URL for A2A and MCP traffic. The controller falls back to http://127.0.0.1:8083; Python runtimes require a value.",
		ComponentAgentRuntime,
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

	// Registered here for `kagent env` CLI discoverability only -- the
	// actual gate is read independently (raw os.Getenv, not via this var)
	// in go/adk/pkg/tools/skills.go's enableFileSearchToolsEnv. The two
	// literals are pinned together by that package's
	// TestEnableFileSearchToolsEnvMatchesRegistry.
	KagentEnableFileSearchTools = RegisterBoolVar(
		"KAGENT_ENABLE_FILE_SEARCH_TOOLS",
		false,
		"When true, enables the list_files and grep_file skills tools, which let an agent "+
			"enumerate and search the filesystem under its session/skills roots without a "+
			"shell. Disabled by default; set in Harness env to opt in.",
		ComponentAgentRuntime,
	)

	StsWellKnownURI = RegisterStringVar(
		"STS_WELL_KNOWN_URI",
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
		"DATABASE_VECTOR_ENABLED",
		false,
		"Enable vector database migrations and vector-backed database functionality.",
		ComponentDatabase,
	)

	SkipMigrations = RegisterBoolVar(
		"SKIP_MIGRATIONS",
		false,
		"Verify required database migrations at startup without applying them.",
		ComponentDatabase,
	)
)
