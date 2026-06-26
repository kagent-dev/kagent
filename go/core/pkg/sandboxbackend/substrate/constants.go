package substrate

import (
	"fmt"
	"strings"
)

// Default Substrate workload images for the acp-shim agent targets
// (docker/acp-sandbox/Dockerfile). Substrate admission requires digest-pinned
// refs, so only the image DIGEST is baked into the controller binary at link
// time by scripts/controller-digest-ldflags.sh (run by `make build-controller`)
// from the just-pushed images. The registry and repository are composed at
// runtime from --image-registry/--image-repository (the same DefaultImageConfig
// values used for declarative agent images), so the same controller binary
// works against ghcr.io, cr.kagent.dev, localhost:5001, or a private/mirrored
// registry without editing this file (kagent-dev/kagent#2055).
const (
	acpSandboxOpenClawImageName = "acp-sandbox-openclaw"
	acpSandboxHermesImageName   = "acp-sandbox-hermes"
)

// AcpSandboxOpenClawImageDigest and AcpSandboxHermesImageDigest are the
// link-time-injected image digests (sha256:...) for the acp-sandbox workload
// images, set via -X ...substrate.AcpSandbox*ImageDigest=... They are empty in
// source and in unit tests, in which case resolution returns an error rather
// than an unpinned ref.
var (
	AcpSandboxOpenClawImageDigest string
	AcpSandboxHermesImageDigest   string
)

// acpSandboxImageConfig carries the runtime registry/repository used to compose
// digest-pinned acp-sandbox workload image refs. Registry and Repository come
// from --image-registry/--image-repository (the same DefaultImageConfig values
// used for declarative agent images), so private or mirrored registries resolve
// correctly. Repository is the agent app repository (e.g.
// "kagent-dev/kagent/app"); its parent path is the shared base for all kagent
// images, onto which the acp-sandbox image name is appended.
type acpSandboxImageConfig struct {
	Registry   string
	Repository string
}

// acpSandboxOpenClawImage resolves the default Substrate workload image for
// OpenClaw harnesses: the acp-sandbox openclaw target, which layers the
// acp-shim and the restore-proof gateway-ensure scripts onto an OpenClaw
// install.
func acpSandboxOpenClawImage(cfg acpSandboxImageConfig) (string, error) {
	return cfg.resolve(acpSandboxOpenClawImageName, AcpSandboxOpenClawImageDigest)
}

// acpSandboxHermesImage resolves the acp-sandbox "hermes" target image.
func acpSandboxHermesImage(cfg acpSandboxImageConfig) (string, error) {
	return cfg.resolve(acpSandboxHermesImageName, AcpSandboxHermesImageDigest)
}

// resolve composes the digest-pinned ref registry/repo/name@sha256:... for an
// acp-sandbox target. Substrate admission requires a digest, so a missing
// link-time digest is a hard error: the controller must be rebuilt after
// pushing the acp-sandbox images, or the harness/cluster must specify an
// explicit digest-pinned workload image. A missing registry or repository is
// likewise an error, since both are required to form a resolvable ref.
func (cfg acpSandboxImageConfig) resolve(name, digest string) (string, error) {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return "", fmt.Errorf(
			"acp-sandbox %s image digest is not set at link time; rebuild the controller after pushing the acp-sandbox images (or set a digest-pinned Substrate.WorkloadImage)",
			name,
		)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		digest = "sha256:" + digest
	}

	registry := strings.Trim(strings.TrimSpace(cfg.Registry), "/")
	if registry == "" {
		return "", fmt.Errorf("acp-sandbox %s image registry is not configured (set --image-registry)", name)
	}
	repoBase := acpSandboxRepositoryBase(cfg.Repository)
	if repoBase == "" {
		return "", fmt.Errorf("acp-sandbox %s image repository is not configured (set --image-repository)", name)
	}

	return fmt.Sprintf("%s/%s/%s@%s", registry, repoBase, name, digest), nil
}

// acpSandboxRepositoryBase derives the shared kagent image repository base from
// the agent app repository by stripping its last path segment, e.g.
// "kagent-dev/kagent/app" -> "kagent-dev/kagent". The acp-sandbox image name is
// then appended to this base. A single-segment repository (no "/") is returned
// as-is and used directly as the base.
func acpSandboxRepositoryBase(repository string) string {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return ""
	}
	if i := strings.LastIndex(repository, "/"); i >= 0 {
		return repository[:i]
	}
	return repository
}
