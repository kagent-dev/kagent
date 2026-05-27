package controller

import (
	"context"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

func (r *AgentHarnessController) enqueueAgentHarnessForSubstrateResource(ctx context.Context, obj client.Object) []reconcile.Request {
	harnessName := substrate.HarnessNameFromLabels(obj.GetLabels())
	if harnessName == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      harnessName,
		},
	}}
}

func (r *AgentHarnessController) enqueueAgentHarnessForWorkerPoolDeployment(ctx context.Context, obj client.Object) []reconcile.Request {
	deploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		return nil
	}
	harnessName := substrate.HarnessNameFromLabels(deploy.GetLabels())
	if harnessName == "" {
		harnessName = r.harnessNameFromWorkerPoolDeployment(ctx, deploy)
	}
	if harnessName == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: deploy.Namespace,
			Name:      harnessName,
		},
	}}
}

// harnessNameFromWorkerPoolDeployment resolves the harness via the owning WorkerPool's labels.
// Substrate names deployments "{workerPool}-deployment" and does not copy harness labels onto them.
func (r *AgentHarnessController) harnessNameFromWorkerPoolDeployment(ctx context.Context, deploy *appsv1.Deployment) string {
	if r == nil || r.Client == nil || deploy == nil {
		return ""
	}
	for _, ref := range deploy.GetOwnerReferences() {
		if ref.Kind != "WorkerPool" || ref.Controller == nil || !*ref.Controller {
			continue
		}
		if !strings.Contains(ref.APIVersion, "ate.dev") {
			continue
		}
		var wp atev1alpha1.WorkerPool
		key := types.NamespacedName{Namespace: deploy.Namespace, Name: ref.Name}
		if err := r.Client.Get(ctx, key, &wp); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return ""
		}
		if name := substrate.HarnessNameFromLabels(wp.GetLabels()); name != "" {
			return name
		}
	}
	return ""
}

func (r *AgentHarnessController) substrateWatches(b *builder.Builder) *builder.Builder {
	if r == nil || r.SubstrateProvisioner == nil {
		return b
	}
	return b.
		Watches(
			&atev1alpha1.WorkerPool{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAgentHarnessForSubstrateResource),
		).
		Watches(
			&atev1alpha1.ActorTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAgentHarnessForSubstrateResource),
		).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAgentHarnessForWorkerPoolDeployment),
		)
}
