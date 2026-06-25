package substrate

import (
	"context"
	"fmt"
	"maps"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (p *Lifecycle) ensureActorTemplate(ctx context.Context, ah *v1alpha2.AgentHarness, wpKey types.NamespacedName) (types.NamespacedName, error) {
	key := types.NamespacedName{Namespace: ah.Namespace, Name: actorTemplateName(ah)}
	desired, err := p.buildActorTemplate(ctx, ah, wpKey)
	if err != nil {
		return types.NamespacedName{}, err
	}

	existing := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, p.Client, existing, func() error {
		existing.Labels = mergeLabels(existing.Labels, desired.Labels)
		existing.OwnerReferences = desired.OwnerReferences
		existing.Spec = desired.Spec
		return nil
	}); err != nil {
		return types.NamespacedName{}, fmt.Errorf("reconcile ActorTemplate %s: %w", key, err)
	}
	return key, nil
}

func (p *Lifecycle) buildActorTemplate(ctx context.Context, ah *v1alpha2.AgentHarness, wpKey types.NamespacedName) (*atev1alpha1.ActorTemplate, error) {
	key := types.NamespacedName{Namespace: ah.Namespace, Name: actorTemplateName(ah)}

	var (
		startupScript  string
		containerEnv   []atev1alpha1.EnvVar
		defaultImageFn func(acpSandboxImageConfig) (string, error)
		containerName  string
		err            error
	)
	// clawBackend selects the OpenClaw startup path; the cluster-wide
	// DefaultWorkloadImage only applies to claw backends (it points at the
	// openclaw sandbox image), other backends fall back to their own image.
	clawBackend := false
	switch ah.Spec.Backend {
	case v1alpha2.AgentHarnessBackendOpenClaw:
		clawBackend = true
		defaultImageFn = acpSandboxOpenClawImage
		containerName = defaultOpenClawContainer
		startupScript, containerEnv, err = p.buildOpenClawActorStartup(ctx, ah)
		if err != nil {
			return nil, fmt.Errorf("build openclaw actor startup: %w", err)
		}
	default:
		spec, ok := acpAgentSpecs[ah.Spec.Backend]
		if !ok {
			return nil, fmt.Errorf("substrate runtime does not support backend %q", ah.Spec.Backend)
		}
		defaultImageFn = spec.DefaultImage
		containerName = string(ah.Spec.Backend)
		startupScript, containerEnv, err = p.buildAcpAgentActorStartup(ctx, ah, spec)
		if err != nil {
			return nil, fmt.Errorf("build %s actor startup: %w", ah.Spec.Backend, err)
		}
	}

	workloadImage := strings.TrimSpace(ah.Spec.Substrate.WorkloadImage)
	if workloadImage == "" && clawBackend {
		workloadImage = strings.TrimSpace(p.Defaults.DefaultWorkloadImage)
	}
	if workloadImage == "" {
		// Fall back to the backend's built-in default, which is always
		// digest-pinned (or errors if the link-time digest is missing).
		workloadImage, err = defaultImageFn(p.acpSandboxImageConfig())
		if err != nil {
			return nil, err
		}
	} else {
		workloadImage, err = pinImageRef(workloadImage)
		if err != nil {
			return nil, err
		}
	}

	desired := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels:    lifecycleLabels(ah),
		},
		Spec: atev1alpha1.ActorTemplateSpec{
			PauseImage: p.Defaults.PauseImage,
			Runsc:      defaultRunscConfig(p.Defaults),
			Containers: []atev1alpha1.Container{
				{
					Name:  containerName,
					Image: workloadImage,
					Command: []string{
						"/bin/sh",
						"-c",
						startupScript,
					},
					Env: containerEnv,
				},
			},
			WorkerPoolRef: corev1.ObjectReference{
				Name:      wpKey.Name,
				Namespace: wpKey.Namespace,
			},
			SnapshotsConfig: atev1alpha1.SnapshotsConfig{
				Location: substrateSnapshotsLocation(ah),
			},
		},
	}
	if err := controllerutil.SetControllerReference(ah, desired, p.Client.Scheme()); err != nil {
		return nil, fmt.Errorf("set ActorTemplate owner ref: %w", err)
	}
	return desired, nil
}

func mergeLabels(existing, desired map[string]string) map[string]string {
	if len(existing) == 0 && len(desired) == 0 {
		return nil
	}
	merged := make(map[string]string, len(existing)+len(desired))
	maps.Copy(merged, existing)
	maps.Copy(merged, desired)
	return merged
}

// ActorTemplateReady reports whether the ActorTemplate golden snapshot is ready.
func (p *Lifecycle) ActorTemplateReady(ctx context.Context, key types.NamespacedName) (bool, error) {
	return p.actorTemplateReady(ctx, key)
}

func (p *Lifecycle) actorTemplateReady(ctx context.Context, key types.NamespacedName) (bool, error) {
	var tmpl atev1alpha1.ActorTemplate
	if err := p.Client.Get(ctx, key, &tmpl); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get ActorTemplate %s: %w", key, err)
	}
	return tmpl.Status.Phase == atev1alpha1.PhaseReady, nil
}
