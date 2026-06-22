package substrate

// Default Substrate workload images for the acp-shim agent targets
// (docker/acp-sandbox/Dockerfile). Substrate admission requires digest-pinned
// refs. Both backend images are kept together here.
const (
	// AcpSandboxOpenClawImage is the default Substrate workload image for
	// OpenClaw harnesses: the kagent acp-sandbox openclaw target, which layers
	// the acp-shim and the restore-proof gateway-ensure scripts onto an
	// OpenClaw install.
	AcpSandboxOpenClawImage = "ttl.sh/kagent-acp-openclaw@sha256:f608146e4716607b3ff6e979eb2aa21130febaab4f4c1f53ea8c3e02b4dd08ba"

	// AcpSandboxHermesImage is the acp-sandbox "hermes" target.
	AcpSandboxHermesImage = "ttl.sh/kagent-acp-hermes@sha256:76550d1fc3534f88de71338e223ce07f4f59fc7983d89b647d483ca833ca7c24"
)
