package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
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
	if p == nil || p.Client == nil {
		return types.NamespacedName{}, fmt.Errorf("substrate lifecycle kubernetes client is required")
	}
	key := p.Defaults.DefaultWorkerPool
	if sub := ah.Spec.Substrate; sub != nil && sub.WorkerPoolRef != nil {
		if name := strings.TrimSpace(sub.WorkerPoolRef.Name); name != "" {
			key = types.NamespacedName{Namespace: ah.Namespace, Name: name}
		}
	}
	if key.Name == "" {
		return types.NamespacedName{}, fmt.Errorf("spec.substrate.workerPoolRef is required when no default substrate WorkerPool is configured")
	}
	if key.Namespace == "" {
		key.Namespace = ah.Namespace
	}

	var wp atev1alpha1.WorkerPool
	if err := p.Client.Get(ctx, key, &wp); err != nil {
		return types.NamespacedName{}, fmt.Errorf("get WorkerPool %s: %w", key, err)
	}
	return key, nil
}
