package env

// Core kagent environment variables used by the controller and agent runtime.
var (
	KagentNamespace = RegisterStringVar(
		"KAGENT_NAMESPACE",
		"kagent",
		"Kubernetes namespace where kagent resources are deployed.",
		ComponentController,
	)

	KagentControllerName = RegisterStringVar(
		"KAGENT_CONTROLLER_NAME",
		"kagent-controller",
		"Name of the kagent controller service.",
		ComponentController,
	)

	KagentA2ADebugAddr = RegisterStringVar(
		"KAGENT_A2A_DEBUG_ADDR",
		"",
		"Debug address for the A2A server. When set, all A2A HTTP requests are dialed to this address.",
		ComponentController,
	)

	KagentA2AClientTimeout = RegisterDurationVar(
		"KAGENT_A2A_CLIENT_TIMEOUT",
		0,
		"HTTP client timeout for A2A requests from the controller to agent pods. "+
			"0 (the default) means no timeout, which is recommended for long-running agents "+
			"that stream responses over SSE. Set a positive duration (e.g. 30m) only if you "+
			"need a hard upper bound on individual A2A calls.",
		ComponentController,
	)

	KagentMCPStateless = RegisterBoolVar(
		"KAGENT_MCP_STATELESS",
		false,
		"When true, the MCP server operates in stateless mode (no session persistence). "+
			"Use when the network path does not provide sticky session routing based on the Mcp-Session-Id header. "+
			"Note: stateless mode disables server-initiated notifications; clients will not receive "+
			"resources/updated events.",
		ComponentController,
	)

	// Variables injected into agent pods (not read by the controller itself).

	KagentName = RegisterStringVar(
		"KAGENT_NAME",
		"",
		"Name of the agent. Injected into agent pods via the controller.",
		ComponentAgentRuntime,
	)

	KagentURL = RegisterStringVar(
		"KAGENT_URL",
		"",
		"Base URL for A2A communication with the kagent controller.",
		ComponentAgentRuntime,
	)

	KagentUIURL = RegisterStringVar(
		"KAGENT_UI_URL",
		"",
		"Public base URL of the kagent UI (e.g. https://kagent.example.com). "+
			"When set, share link tools return full clickable URLs instead of paths.",
		ComponentAgentRuntime,
	)

	KagentSkillsFolder = RegisterStringVar(
		"KAGENT_SKILLS_FOLDER",
		"/skills",
		"Directory path where agent skills are mounted.",
		ComponentAgentRuntime,
	)

	KagentSRTSettingsPath = RegisterStringVar(
		"KAGENT_SRT_SETTINGS_PATH",
		"/config/srt-settings.json",
		"Path to the mounted srt settings file used by sandboxed execution.",
		ComponentAgentRuntime,
	)

	KagentPropagateToken = RegisterStringVar(
		"KAGENT_PROPAGATE_TOKEN",
		"",
		"When set, propagates the authentication token to downstream services.",
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
		"RFC 8707 resource indicator sent on STS token-exchange requests to scope the issued token to a target backend.",
		ComponentAgentRuntime,
	)

	KagentSTSAudience = RegisterStringVar(
		"KAGENT_STS_AUDIENCE",
		"",
		"RFC 8693 audience sent on STS token-exchange requests. Alternate to KAGENT_STS_RESOURCE for servers that key on audience.",
		ComponentAgentRuntime,
	)
)
