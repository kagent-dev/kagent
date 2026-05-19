package openclaw

const (
	// NemoclawSandboxBaseImage is the default OpenShell VM image for OpenClaw/NemoClaw harnesses.
	NemoclawSandboxBaseImage = "ghcr.io/kagent-dev/nemoclaw/sandbox-base:2026.5.4"

	// bootstrapSecretProviderID is the secrets.providers key written into openclaw.json.
	bootstrapSecretProviderID = "kagent"

	// DefaultInferenceBaseURL is the Model provider baseUrl when ModelConfig does not set an explicit upstream.
	DefaultInferenceBaseURL = "https://inference.local/v1"
)
