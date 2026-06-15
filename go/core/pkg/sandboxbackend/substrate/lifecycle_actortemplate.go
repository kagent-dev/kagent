package substrate

import (
	"context"
	"fmt"
	"maps"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openclaw"
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
	workloadImage := strings.TrimSpace(ah.Spec.Substrate.WorkloadImage)
	if workloadImage == "" {
		workloadImage = strings.TrimSpace(p.Defaults.DefaultWorkloadImage)
	}
	if workloadImage == "" {
		workloadImage = openclaw.NemoclawSandboxBaseImage
	} else {
		var err error
		workloadImage, err = pinImageRef(workloadImage)
		if err != nil {
			return nil, err
		}
	}
	startupScript, containerEnv, err := p.buildOpenClawActorStartup(ctx, ah)
	if err != nil {
		return nil, fmt.Errorf("build openclaw actor startup: %w", err)
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
					Name:  defaultOpenClawContainer,
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
