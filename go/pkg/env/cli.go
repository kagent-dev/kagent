package env

// CLI-specific environment variables used by the kagent CLI tool.
var (
	KagentDefaultModelProvider = RegisterStringVar(
		"KAGENT_DEFAULT_MODEL_PROVIDER",
		"OpenAI",
		"Default LLM provider for agents (e.g. openAI, anthropic, ollama, azureOpenAI).",
		ComponentCLI,
	)

	KagentHelmRepo = RegisterStringVar(
		"KAGENT_HELM_REPO",
		"oci://ghcr.io/kagent-dev/kagent/helm/",
		"Helm repository URL for kagent charts.",
		ComponentCLI,
	)

	KagentHelmVersion = RegisterStringVar(
		"KAGENT_HELM_VERSION",
		"",
		"Helm chart version to deploy.",
		ComponentCLI,
	)

	KagentHelmExtraArgs = RegisterStringVar(
		"KAGENT_HELM_EXTRA_ARGS",
		"",
		"Additional arguments to pass to Helm commands.",
		ComponentCLI,
	)
)
