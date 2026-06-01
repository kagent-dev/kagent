package openclaw

const (
	// NemoclawSandboxBaseImage is the default OpenShell VM image for OpenClaw/NemoClaw harnesses.
	// Substrate requires workload images to use @sha256:... refs (see pinImageRef). (OpenShell doesn't care)
	// Tag: 2026.5.4
	NemoclawSandboxBaseImage = "ghcr.io/kagent-dev/nemoclaw/sandbox-base@sha256:d52bee415dc4c0dba7164f9eabe727574c056d4f211781f20af249707883a3b4"

	// openshellSecretProviderID is the secrets.providers key written into openclaw.json for OpenShell sandboxes.
	openshellSecretProviderID = "kagent"

	// substrateSecretProviderID is the env SecretRef provider id for native OpenClaw on Substrate.
	substrateSecretProviderID = "default"

	// DefaultInferenceBaseURL is the Model provider baseUrl when ModelConfig does not set an explicit upstream (OpenShell).
	DefaultInferenceBaseURL = "https://inference.local/v1"

	// SubstrateBootstrapDefaultBaseURL is passed when building openclaw.json for Substrate harnesses.
	// When ModelConfig has no explicit provider URL, the models section is omitted entirely so
	// OpenClaw is not given a partial providers.* block (baseUrl is required when present).
	SubstrateBootstrapDefaultBaseURL = ""
)
