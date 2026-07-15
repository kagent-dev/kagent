package substrate

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ErrActorTemplateReconcilePending indicates ActorTemplate reconciliation started
// a multi-step recreate (e.g. golden-actor deletion) and callers should requeue.
var ErrActorTemplateReconcilePending = errors.New("actor template reconciliation pending")

func (p *Lifecycle) ensureActorTemplate(ctx context.Context, ah *v1alpha2.AgentHarness, wpKey types.NamespacedName) (types.NamespacedName, error) {
	key := types.NamespacedName{Namespace: ah.Namespace, Name: actorTemplateName(ah)}
	desired, err := p.buildActorTemplate(ctx, ah, wpKey)
	if err != nil {
		return types.NamespacedName{}, err
	}
	if err := reconcileActorTemplate(ctx, p.Client, p.AteClient, desired); err != nil {
		return types.NamespacedName{}, fmt.Errorf("reconcile ActorTemplate %s: %w", key, err)
	}
	return key, nil
}

// actorTemplateSpecEqual reports whether two ActorTemplate specs are semantically equal.
func actorTemplateSpecEqual(a, b atev1alpha1.ActorTemplateSpec) bool {
	return apiequality.Semantic.DeepEqual(a, b)
}

// reconcileActorTemplate applies the desired ActorTemplate with immutable-spec semantics:
//
//   - not found        -> create
//   - spec matches     -> patch labels/annotations/owner refs only (never the spec)
//   - spec drifts      -> delete the golden actor, delete the CR, recreate
//
// On spec drift it performs at most one mutating step per call. When more work
// remains it returns ErrActorTemplateReconcilePending so callers requeue.
func reconcileActorTemplate(ctx context.Context, c client.Client, ate *Client, desired *atev1alpha1.ActorTemplate) error {
	key := client.ObjectKeyFromObject(desired)

	existing := &atev1alpha1.ActorTemplate{}
	err := c.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil {
			return fmt.Errorf("create ActorTemplate %s: %w", key, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get ActorTemplate %s: %w", key, err)
	}

	// If the spec is semantically equal, update the labels and annotations and owner references only.
	if actorTemplateSpecEqual(existing.Spec, desired.Spec) {
		mergedLabels := mergeLabels(existing.Labels, desired.Labels)
		mergedAnnotations := mergeLabels(existing.Annotations, desired.Annotations)
		if maps.Equal(existing.Labels, mergedLabels) &&
			maps.Equal(existing.Annotations, mergedAnnotations) &&
			apiequality.Semantic.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
			return nil
		}
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Labels = mergedLabels
		existing.Annotations = mergedAnnotations
		existing.OwnerReferences = desired.OwnerReferences
		if err := c.Patch(ctx, existing, patch); err != nil {
			return fmt.Errorf("patch ActorTemplate %s metadata: %w", key, err)
		}
		return nil
	}

	// Delete the golden actor since it is an external ate-api resource
	if goldenID := strings.TrimSpace(existing.Status.GoldenActorID); goldenID != "" {
		done, derr := deleteGoldenActor(ctx, ate, goldenID)
		if derr != nil {
			return fmt.Errorf("delete golden actor %q before recreating ActorTemplate %s: %w", goldenID, key, derr)
		}
		if !done {
			return ErrActorTemplateReconcilePending
		}
	}
	if err := c.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete ActorTemplate %s for recreate: %w", key, err)
	}
	if err := c.Create(ctx, desired); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// The previous CR is still terminating; recreate on the next pass.
			return ErrActorTemplateReconcilePending
		}
		return fmt.Errorf("recreate ActorTemplate %s: %w", key, err)
	}
	return nil
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
			PauseImage:   p.Defaults.PauseImage,
			SandboxClass: atev1alpha1.SandboxClassGvisor,
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
			WorkerSelector: workerSelectorForPool(wpKey),
			SnapshotsConfig: atev1alpha1.SnapshotsConfig{
				Location: substrateSnapshotsLocation(ah),
				// Mirror substrate's CRD defaults so kagent's spec-drift check
				// (apiequality.Semantic.DeepEqual) treats them as equal to the
				// values the API server fills in on admission — otherwise kagent
				// re-creates the ActorTemplate every reconcile in a hot loop.
				OnPause:  atev1alpha1.SnapshotScopeFull,
				OnCommit: atev1alpha1.SnapshotScopeFull,
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
