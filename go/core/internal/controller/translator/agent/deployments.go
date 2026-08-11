package agent

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
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

// getRuntimeImageRepository returns the image repository for a given runtime:
// DefaultGoImageConfig.Repository for the Go runtime, DefaultImageConfig.Repository
// otherwise.
func getRuntimeImageRepository(runtime v1alpha2.DeclarativeRuntime) string {
	if runtime == v1alpha2.DeclarativeRuntime_Go {
		return DefaultGoImageConfig.Repository
	}
	return DefaultImageConfig.Repository
}

func resolvePythonRuntimeImage(registry string, full, pinDigest bool) (string, error) {
	repo := DefaultImageConfig.Repository
	digest := PythonADKImageDigest
	imageLabel := "app"
	if full {
		digest = PythonADKFullImageDigest
		imageLabel = "app-full"
	}
	return resolveRuntimeImage(registry, repo, DefaultImageConfig.Tag, digest, imageLabel, full, pinDigest)
}

func resolveGoRuntimeImage(registry string, full, pinDigest bool) (string, error) {
	repo := getRuntimeImageRepository(v1alpha2.DeclarativeRuntime_Go)
	digest := GoADKImageDigest
	imageLabel := "golang-adk"
	if full {
		digest = GoADKFullImageDigest
		imageLabel = "golang-adk-full"
	}
	return resolveRuntimeImage(registry, repo, DefaultGoImageConfig.Tag, digest, imageLabel, full, pinDigest)
}

// resolveRuntimeImage builds the image reference for a declarative agent runtime.
//
// Regular agents get a tag reference (registry/repository:tag) so mirrored
// registries that do not preserve upstream manifest digests still resolve
// (https://github.com/kagent-dev/kagent/issues/2055). Full-variant images share
// the repository and are published under "<tag>-full" (see APP_FULL_IMAGE_TAG /
// GOLANG_ADK_FULL_IMAGE_TAG in the Makefile).
//
// Sandbox agents require pinDigest: Substrate ActorTemplate validation rejects
// image refs without a digest, so those use the link-time (or flag-overridden)
// runtime image digests.
func resolveRuntimeImage(registry, repository, tag, digest, imageLabel string, full, pinDigest bool) (string, error) {
	if !pinDigest {
		if full {
			tag += "-full"
		}
		return fmt.Sprintf("%s/%s:%s", registry, repository, tag), nil
	}
	if d := normalizeImageDigest(digest); d != "" {
		return fmt.Sprintf("%s/%s@%s", registry, repository, d), nil
	}
	return "", fmt.Errorf(
		"%s image digest is not set; rebuild the controller after pushing agent runtime images, or override it via --%s-image-digest",
		imageLabel, imageLabel,
	)
}

func resolveInlineDeployment(agent *v1alpha2.SandboxAgent, mdd *modelDeploymentData) (*resolvedDeployment, error) {
	specRef := agent.GetAgentSpec()
	spec := specRef.Declarative

	// Determine runtime (defaults to python when spec.declarative.runtime is unset).
	runtime := v1alpha2.EffectiveDeclarativeRuntime(agent.GetAgentSpec())

	// Resolve the runtime image registry.
	baseRegistry := DefaultImageConfig.Registry
	if runtime == v1alpha2.DeclarativeRuntime_Go {
		baseRegistry = DefaultGoImageConfig.Registry
	}

	// Per-agent spec overrides take precedence over all defaults.
	registry := baseRegistry
	if spec.ImageRegistry != "" {
		registry = spec.ImageRegistry
	}

	var image string
	full := needsSRTSettings(agent, specRef.Sandbox)
	// Substrate ActorTemplates reject tag refs, so runtime images are pinned.
	pinDigest := true
	switch runtime {
	case v1alpha2.DeclarativeRuntime_Go:
		var err error
		image, err = resolveGoRuntimeImage(registry, full, pinDigest)
		if err != nil {
			return nil, err
		}
	default:
		var err error
		image, err = resolvePythonRuntimeImage(registry, full, pinDigest)
		if err != nil {
			return nil, err
		}
	}

	dep := &resolvedDeployment{
		Image: image,
		Env:   append(slices.Clone(spec.Env), mdd.EnvVars...),
	}

	return dep, nil
}

func resolveByoDeployment(agent *v1alpha2.SandboxAgent) (*resolvedDeployment, error) {
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
