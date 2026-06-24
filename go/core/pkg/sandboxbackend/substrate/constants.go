package substrate

import (
	"fmt"
	"strings"
)

// Default Substrate workload image refs for the acp-shim agent targets
// (docker/acp-sandbox/Dockerfile). Substrate admission requires digest-pinned
// refs, so the fully resolved ref (registry/repo/name@sha256:...) is baked into
// the controller binary at link time by scripts/controller-digest-ldflags.sh
// (run by `make build-controller`) from the just-pushed images. Because the ref
// is derived from the image the build actually pushed, it tracks whatever
// registry was used — ghcr.io for releases, localhost:5001 for local
// kind/`make helm-install` — so the same controller binary works in both, and
// republishing the images never requires editing this file.
const (
	acpSandboxOpenClawImageName = "acp-sandbox-openclaw"
	acpSandboxHermesImageName   = "acp-sandbox-hermes"
)

// AcpSandboxOpenClawImageRef and AcpSandboxHermesImageRef are the digest-pinned
// workload image refs (registry/repo/name@sha256:...), set at controller link
// time via -X ...substrate.AcpSandbox*ImageRef=... They are empty in source and
// in unit tests, in which case resolution returns an error rather than an
// unpinned (or wrong-registry) ref.
var (
	AcpSandboxOpenClawImageRef string
	AcpSandboxHermesImageRef   string
)

// AcpSandboxOpenClawImage returns the default Substrate workload image for
// OpenClaw harnesses: the acp-sandbox openclaw target, which layers the
// acp-shim and the restore-proof gateway-ensure scripts onto an OpenClaw
// install. It errors when the link-time ref was not injected.
func AcpSandboxOpenClawImage() (string, error) {
	return acpSandboxImage(acpSandboxOpenClawImageName, AcpSandboxOpenClawImageRef)
}

// AcpSandboxHermesImage returns the acp-sandbox "hermes" target image. It errors
// when the link-time ref was not injected.
func AcpSandboxHermesImage() (string, error) {
	return acpSandboxImage(acpSandboxHermesImageName, AcpSandboxHermesImageRef)
}

// acpSandboxImage returns the link-time-injected, digest-pinned ref for an
// acp-sandbox target. Substrate admission requires a digest, so a missing or
// unpinned ref is a hard error: the controller must be rebuilt after pushing
// the acp-sandbox images, or the harness/cluster must specify an explicit
// digest-pinned workload image.
func acpSandboxImage(name, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf(
			"acp-sandbox %s image ref is not set at link time; rebuild the controller after pushing the acp-sandbox images (or set a digest-pinned Substrate.WorkloadImage)",
			name,
		)
	}
	if !strings.Contains(ref, "@") {
		return "", fmt.Errorf("acp-sandbox %s image ref %q is not digest-pinned (missing @sha256:...)", name, ref)
	}
	return ref, nil
}
