package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AdvanceActorDelete deletes a harness actor via ate-api (one RPC step per call).
func (p *Provisioner) AdvanceActorDelete(ctx context.Context, actorID string) (bool, error) {
	if p == nil || p.Ate == nil || strings.TrimSpace(actorID) == "" {
		return true, nil
	}
	return p.Ate.AdvanceActorDelete(ctx, actorID)
}

// AdvanceDelete issues delete requests and observes substrate cleanup progress without blocking.
// Returns true when all kagent-managed Substrate resources for this harness are gone.
func (p *Provisioner) AdvanceDelete(ctx context.Context, ah *v1alpha2.AgentHarness) (bool, error) {
	if ah == nil || ah.Annotations == nil {
		return true, nil
	}
	if p.Client == nil {
		return true, nil
	}

	if ah.Annotations[AnnotationManagedActorTemplate] == "true" {
		tmplKey := types.NamespacedName{Namespace: ah.Namespace, Name: actorTemplateName(ah)}
		goldenID, err := p.goldenActorID(ctx, tmplKey)
		if err != nil {
			return false, err
		}
		if goldenID != "" {
			if p.Ate == nil {
				return false, fmt.Errorf("substrate ate-api client is required to delete golden actor %q", goldenID)
			}
			done, err := p.Ate.AdvanceActorDelete(ctx, goldenID)
			if err != nil {
				return false, fmt.Errorf("delete golden actor %q for ActorTemplate %s: %w", goldenID, tmplKey, err)
			}
			if !done {
				return false, nil
			}
		}
		var tmpl atev1alpha1.ActorTemplate
		if done, err := p.advanceDeleteCR(ctx, tmplKey, &tmpl); err != nil || !done {
			return false, err
		}
	}

	if ah.Annotations[AnnotationManagedWorkerPool] == "true" {
		wpKey := types.NamespacedName{Namespace: ah.Namespace, Name: workerPoolName(ah)}
		var wp atev1alpha1.WorkerPool
		if done, err := p.advanceDeleteCR(ctx, wpKey, &wp); err != nil || !done {
			return false, err
		}
		gone, err := p.workerPoolDeploymentGone(ctx, wpKey)
		if err != nil {
			return false, err
		}
		if !gone {
			return false, nil
		}
	}

	return true, nil
}

func (p *Provisioner) goldenActorID(ctx context.Context, tmplKey types.NamespacedName) (string, error) {
	var tmpl atev1alpha1.ActorTemplate
	if err := p.Client.Get(ctx, tmplKey, &tmpl); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("get ActorTemplate %s for golden actor cleanup: %w", tmplKey, err)
	}
	return strings.TrimSpace(tmpl.Status.GoldenActorID), nil
}

// advanceDeleteCR deletes obj when present; returns true when the object is gone.
func (p *Provisioner) advanceDeleteCR(ctx context.Context, key types.NamespacedName, obj client.Object) (bool, error) {
	if err := p.Client.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	if obj.GetDeletionTimestamp().IsZero() {
		if err := p.Client.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("delete %s: %w", key, err)
		}
		return false, nil
	}
	return false, nil
}

func workerPoolDeploymentName(wpName string) string {
	return wpName + "-deployment"
}

// workerPoolDeploymentGone reports whether the substrate WorkerPool deployment is absent or fully drained.
func (p *Provisioner) workerPoolDeploymentGone(ctx context.Context, wpKey types.NamespacedName) (bool, error) {
	deployKey := types.NamespacedName{Namespace: wpKey.Namespace, Name: workerPoolDeploymentName(wpKey.Name)}
	var deploy appsv1.Deployment
	err := p.Client.Get(ctx, deployKey, &deploy)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get WorkerPool deployment %s: %w", deployKey, err)
	}
	if !deploy.DeletionTimestamp.IsZero() {
		return false, nil
	}
	if deploy.Status.Replicas == 0 && deploy.Status.ReadyReplicas == 0 {
		return true, nil
	}
	return false, nil
}

// HarnessLabelKey labels substrate resources managed for an AgentHarness.
const HarnessLabelKey = "kagent.dev/agent-harness"

// HarnessNameFromLabels returns the AgentHarness name from provision labels.
func HarnessNameFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return strings.TrimSpace(labels[HarnessLabelKey])
}
