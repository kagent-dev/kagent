package env

// CLI-specific environment variables used by the kagent CLI tool.
var (
	KagentDefaultModelProvider = RegisterStringVar(
		"KAGENT_DEFAULT_MODEL_PROVIDER",
		"openAI",
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
		"Helm chart version to deploy. When unset, the CLI uses its own version.",
		ComponentCLI,
	)

	KagentHelmExtraArgs = RegisterStringVar(
		"KAGENT_HELM_EXTRA_ARGS",
		"",
		"Additional Helm --set overrides for the Kagent chart.",
		ComponentCLI,
	)

	KagentBundledPostgresImage = RegisterStringVar(
		"KAGENT_BUNDLED_POSTGRES_IMAGE",
		"postgres:18-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15",
		"PostgreSQL image that kagent install deploys. Point it at a mirror for air-gapped clusters.",
		ComponentCLI,
	)

	KagentSubstrateHelmRepo = RegisterStringVar(
		"KAGENT_SUBSTRATE_HELM_REPO",
		"oci://ghcr.io/kagent-dev/substrate/helm/",
		"Helm repository URL for Substrate charts.",
		ComponentCLI,
	)

	KagentSubstrateHelmVersion = RegisterStringVar(
		"KAGENT_SUBSTRATE_HELM_VERSION",
		"",
		"Substrate Helm chart version to deploy. When unset, the CLI uses its pinned Substrate version.",
		ComponentCLI,
	)

	KagentSubstrateHelmExtraArgs = RegisterStringVar(
		"KAGENT_SUBSTRATE_HELM_EXTRA_ARGS",
		"",
		"Additional Helm --set overrides for the Substrate chart.",
		ComponentCLI,
	)

	KagentSubstratePodCertificateHelmRepo = RegisterStringVar(
		"KAGENT_SUBSTRATE_PODCERT_HELM_REPO",
		"",
		"Helm repository URL for the Substrate PodCertificate chart. When unset, the CLI uses KAGENT_SUBSTRATE_HELM_REPO.",
		ComponentCLI,
	)

	KagentSubstratePodCertificateHelmVersion = RegisterStringVar(
		"KAGENT_SUBSTRATE_PODCERT_HELM_VERSION",
		"",
		"Substrate PodCertificate Helm chart version to deploy. When unset, the CLI uses KAGENT_SUBSTRATE_HELM_VERSION or its pinned Substrate version.",
		ComponentCLI,
	)
)
