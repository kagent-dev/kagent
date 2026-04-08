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
)
