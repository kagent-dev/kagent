package substrate

import (
	"context"
	"fmt"
	"strings"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

const workerPoolDrainTimeout = 3 * time.Minute

// Delete removes kagent-managed Substrate CRs after the harness actor has been removed.
// Order: golden snapshot actor (from ActorTemplate status), ActorTemplate, WorkerPool.
func (p *Provisioner) Delete(ctx context.Context, ah *v1alpha2.AgentHarness) error {
	if ah == nil || ah.Annotations == nil {
		return nil
	}
	if ah.Annotations[annotationManagedActorTemplate] == "true" {
		key := types.NamespacedName{Namespace: ah.Namespace, Name: actorTemplateName(ah)}
		if err := p.deleteGoldenActor(ctx, key); err != nil {
			return err
		}
		var tmpl atev1alpha1.ActorTemplate
		if err := p.Client.Get(ctx, key, &tmpl); err == nil {
			if err := p.Client.Delete(ctx, &tmpl); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete ActorTemplate %s: %w", key, err)
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	if ah.Annotations[annotationManagedWorkerPool] == "true" {
		key := types.NamespacedName{Namespace: ah.Namespace, Name: workerPoolName(ah)}
		var wp atev1alpha1.WorkerPool
		if err := p.Client.Get(ctx, key, &wp); err == nil {
			if err := p.Client.Delete(ctx, &wp); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete WorkerPool %s: %w", key, err)
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
		if err := p.waitForWorkerPoolDeploymentGone(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provisioner) deleteGoldenActor(ctx context.Context, tmplKey types.NamespacedName) error {
	if p.Ate == nil || p.Client == nil {
		return nil
	}
	var tmpl atev1alpha1.ActorTemplate
	if err := p.Client.Get(ctx, tmplKey, &tmpl); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get ActorTemplate %s for golden actor cleanup: %w", tmplKey, err)
	}
	goldenID := strings.TrimSpace(tmpl.Status.GoldenActorID)
	if goldenID == "" {
		return nil
	}
	if err := p.Ate.deleteActorSequenced(ctx, goldenID); err != nil {
		return fmt.Errorf("delete golden actor %q for ActorTemplate %s: %w", goldenID, tmplKey, err)
	}
	return nil
}

func workerPoolDeploymentName(wpName string) string {
	return wpName + "-deployment"
}

func (p *Provisioner) waitForWorkerPoolDeploymentGone(ctx context.Context, wpKey types.NamespacedName) error {
	if p.Client == nil {
		return nil
	}
	deployKey := types.NamespacedName{Namespace: wpKey.Namespace, Name: workerPoolDeploymentName(wpKey.Name)}
	deadline := time.Now().Add(workerPoolDrainTimeout)
	for time.Now().Before(deadline) {
		var deploy appsv1.Deployment
		err := p.Client.Get(ctx, deployKey, &deploy)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get WorkerPool deployment %s: %w", deployKey, err)
		}
		if deploy.DeletionTimestamp != nil {
			if err := sleepOrDone(ctx, actorDeletePollInterval); err != nil {
				return err
			}
			continue
		}
		if deploy.Status.Replicas == 0 && deploy.Status.ReadyReplicas == 0 {
			return nil
		}
		if err := sleepOrDone(ctx, actorDeletePollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("timeout waiting for WorkerPool deployment %s to drain", deployKey)
}
