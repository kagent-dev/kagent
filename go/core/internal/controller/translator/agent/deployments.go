package agent

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Internal to translator - Data added to the deployment spec for an inline agent
// Mostly used for model auth and config.
type modelDeploymentData struct {
	EnvVars      []corev1.EnvVar
	Volumes      []corev1.Volume
	VolumeMounts []corev1.VolumeMount
}

// Internal to translator – a unified deployment spec for any agent.
type resolvedDeployment struct {
	Image string
	Cmd   string // empty → no explicit command
	Args  []string
	Env   []corev1.EnvVar
}

func resolveGoRuntimeImage(registry string, pinDigest bool) (string, error) {
	return resolveRuntimeImage(registry, DefaultImageConfig.Repository, DefaultImageConfig.Tag, AgentImageDigest, pinDigest)
}

// resolveRuntimeImage builds the image reference for a declarative agent runtime.
//
// Regular agents get a tag reference (registry/repository:tag) so mirrored
// registries that do not preserve upstream manifest digests still resolve
// (https://github.com/kagent-dev/kagent/issues/2055).
//
// Sandbox agents require pinDigest: Substrate ActorTemplate validation rejects
// image refs without a digest, so those use the link-time (or flag-overridden)
// runtime image digests.
func resolveRuntimeImage(registry, repository, tag, digest string, pinDigest bool) (string, error) {
	if !pinDigest {
		return fmt.Sprintf("%s/%s:%s", registry, repository, tag), nil
	}
	if d := normalizeImageDigest(digest); d != "" {
		return fmt.Sprintf("%s/%s@%s", registry, repository, d), nil
	}
	return "", fmt.Errorf("agent image digest is not set; rebuild the controller after pushing the agent image, or override it via --image-digest")
}

func resolveInlineDeployment(agent *v1alpha3.SandboxAgent, mdd *modelDeploymentData) (*resolvedDeployment, error) {
	specRef := agent.GetAgentSpec()
	spec := specRef.Declarative

	// Per-agent spec overrides take precedence over all defaults.
	registry := DefaultImageConfig.Registry
	if spec.ImageRegistry != "" {
		registry = spec.ImageRegistry
	}

	// Substrate ActorTemplates reject tag refs, so runtime images are pinned.
	image, err := resolveGoRuntimeImage(registry, true)
	if err != nil {
		return nil, err
	}

	dep := &resolvedDeployment{
		Image: image,
		Env:   append(slices.Clone(spec.Env), mdd.EnvVars...),
	}

	return dep, nil
}

func resolveByoDeployment(agent *v1alpha3.SandboxAgent) (*resolvedDeployment, error) {
	spec := agent.GetAgentSpec().BYO
	if spec == nil {
		return nil, fmt.Errorf("BYO spec is required")
	}

	image := spec.Image
	if image == "" {
		// This should never happen as it's required by the API
		return nil, fmt.Errorf("image is required for BYO agent")
	}

	cmd := ""
	if spec.Cmd != nil && *spec.Cmd != "" {
		cmd = *spec.Cmd
	}

	var args []string
	if len(spec.Args) != 0 {
		args = spec.Args
	}

	dep := &resolvedDeployment{
		Image: image,
		Cmd:   cmd,
		Args:  args,
		Env:   slices.Clone(spec.Env),
	}

	return dep, nil
}
