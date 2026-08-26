package substrate

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrActorTemplateReconcilePending indicates an immutable-template recreate is still in progress.
var ErrActorTemplateReconcilePending = errors.New("actor template reconciliation pending")

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
