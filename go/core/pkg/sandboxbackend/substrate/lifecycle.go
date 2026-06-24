package substrate

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
)

// EnsureGeneratedTemplate creates or updates the generated ActorTemplate and reports whether it is Ready.
func (p *Lifecycle) EnsureGeneratedTemplate(ctx context.Context, ah *v1alpha2.AgentHarness) (LifecycleState, error) {
	if ah == nil || ah.Spec.Substrate == nil {
		return LifecycleState{}, fmt.Errorf("spec.substrate is required")
	}

	wpKey, err := p.resolveWorkerPoolRef(ctx, ah)
	if err != nil {
		return LifecycleState{}, err
	}

	tmplKey, err := p.ensureActorTemplate(ctx, ah, wpKey)
	if err != nil {
		return LifecycleState{}, err
	}

	ready, err := p.actorTemplateReady(ctx, tmplKey)
	if err != nil {
		return LifecycleState{}, err
	}

	return LifecycleState{
		ActorTemplateReady: ready,
	}, nil
}

func (p *Lifecycle) resolveWorkerPoolRef(ctx context.Context, ah *v1alpha2.AgentHarness) (types.NamespacedName, error) {
	var explicit *v1alpha2.TypedLocalReference
	if sub := ah.Spec.Substrate; sub != nil {
		explicit = sub.WorkerPoolRef
	}
	return p.resolveWorkerPoolRefFor(ctx, ah.Namespace, explicit)
}
