package controller

import (
	"context"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

func (r *SubstrateAgentHarnessController) enqueueAgentHarnessForSubstrateResource(ctx context.Context, obj client.Object) []reconcile.Request {
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

func (r *SubstrateAgentHarnessController) substrateWatches(b *builder.Builder) *builder.Builder {
	if r == nil {
		return b
	}
	return b.
		Watches(
			&atev1alpha1.ActorTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAgentHarnessForSubstrateResource),
		)
}
