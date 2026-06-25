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

	KagentPropagateToken = RegisterBoolVar(
		"KAGENT_PROPAGATE_TOKEN",
		false,
		"When true, the incoming Authorization token is propagated to downstream MCP servers and A2A agents.",
		ComponentAgentRuntime,
	)

	KagentPropagateTokenOverridesStatic = RegisterBoolVar(
		"KAGENT_PROPAGATE_TOKEN_OVERRIDES_STATIC",
		false,
		"When true, a forwarded or STS-exchanged Authorization takes precedence over a static Authorization on an MCP server, and the displaced static token is sent as the X-Actor-Token actor token for downstream RFC 8693 delegation.",
		ComponentAgentRuntime,
	)

	StsWellKnownURI = RegisterStringVar(
		"STS_WELL_KNOWN_URI",
		"",
		"Well-known endpoint for the Security Token Service (STS) used for token exchange.",
		ComponentAgentRuntime,
	)
)
