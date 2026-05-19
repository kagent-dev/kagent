package hermes

const (
	// HermesSandboxBaseImage is the default OpenShell VM image for Hermes harnesses.
	HermesSandboxBaseImage = "ghcr.io/nvidia/nemoclaw/hermes-sandbox-base:3e56f808"

	// HermesConfigDir is the in-sandbox Hermes config root (HERMES_HOME).
	HermesConfigDir = "/sandbox/.hermes"

	// HermesConfigHashFile is the root-owned integrity anchor written at bootstrap.
	HermesConfigHashFile = "/etc/nemoclaw/hermes.config-hash"

	// HermesInternalGatewayPort is where Hermes binds (127.0.0.1 only).
	HermesInternalGatewayPort = 18642

	// HermesPublicGatewayPort is exposed via socat for OpenShell port forwarding.
	HermesPublicGatewayPort = 8642

	// DefaultInferenceBaseURL is the model base_url when routing through OpenShell inference.local.
	DefaultInferenceBaseURL = "https://inference.local/v1"
)
