package substrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
)

// Ensure creates or updates Substrate CRs and waits for ActorTemplate Ready.
func (p *Provisioner) Ensure(ctx context.Context, ah *v1alpha2.AgentHarness) (EnsureResult, error) {
	if ah == nil || ah.Spec.Substrate == nil {
		return EnsureResult{}, fmt.Errorf("spec.substrate is required")
	}
	if err := validateSubstrateProvisionSpec(ah); err != nil {
		return EnsureResult{}, err
	}

	if ah.Spec.Substrate.ActorTemplateRef != nil && strings.TrimSpace(ah.Spec.Substrate.ActorTemplateRef.Name) != "" {
		return p.ensureAdoptedActorTemplate(ctx, ah)
	}

	wpKey, managedWP, err := p.ensureWorkerPool(ctx, ah)
	if err != nil {
		return EnsureResult{}, err
	}

	tmplKey, err := p.ensureActorTemplate(ctx, ah, wpKey)
	if err != nil {
		return EnsureResult{}, err
	}

	ready, err := p.actorTemplateReady(ctx, tmplKey)
	if err != nil {
		return EnsureResult{}, err
	}

	return EnsureResult{
		WorkerPoolRef:        wpKey,
		ActorTemplateRef:     tmplKey,
		ActorTemplateReady:   ready,
		ManagedWorkerPool:    managedWP,
		ManagedActorTemplate: true,
	}, nil
}

func (p *Provisioner) ensureAdoptedActorTemplate(ctx context.Context, ah *v1alpha2.AgentHarness) (EnsureResult, error) {
	ref := ah.Spec.Substrate.ActorTemplateRef
	ns := ref.Namespace
	if ns == "" {
		ns = ah.Namespace
	}
	tmplKey := types.NamespacedName{Namespace: ns, Name: ref.Name}
	ready, err := p.actorTemplateReady(ctx, tmplKey)
	if err != nil {
		return EnsureResult{}, err
	}
	return EnsureResult{
		ActorTemplateRef:     tmplKey,
		ActorTemplateReady:   ready,
		ManagedActorTemplate: false,
	}, nil
}
