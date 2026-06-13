package substrate

// Default Substrate workload images for the acp-shim agent targets
// (docker/acp-sandbox/Dockerfile). Substrate admission requires digest-pinned
// refs. Both backend images are kept together here.
const (
	// AcpSandboxOpenClawImage is the default Substrate workload image for
	// OpenClaw harnesses: the kagent acp-sandbox openclaw target, which layers
	// the acp-shim and the restore-proof gateway-ensure scripts onto an
	// OpenClaw install.
	AcpSandboxOpenClawImage = "ttl.sh/kagent-acp-openclaw@sha256:f8c7b73253dd00098d3f2cb2c3a3d7585fa549daadeefdacd563362e4d40c7e6"

	// AcpSandboxHermesImage is the acp-sandbox "hermes" target.
	AcpSandboxHermesImage = "ttl.sh/kagent-acp-hermes@sha256:119e32b6d6d2f1a1b722540de32dc1c8df9078fd9fc92b820e7b719153e32f62"
)
