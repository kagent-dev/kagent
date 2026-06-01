package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openclaw"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (p *Provisioner) ensureActorTemplate(ctx context.Context, ah *v1alpha2.AgentHarness, wpKey types.NamespacedName) (types.NamespacedName, error) {
	key := types.NamespacedName{Namespace: ah.Namespace, Name: actorTemplateName(ah)}
	workloadImage := strings.TrimSpace(ah.Spec.Substrate.WorkloadImage)
	if workloadImage == "" {
		workloadImage = strings.TrimSpace(p.Defaults.DefaultWorkloadImage)
	}
	if workloadImage == "" {
		workloadImage = openclaw.NemoclawSandboxBaseImage
	}
	startupScript, containerEnv, err := p.buildOpenClawActorStartup(ctx, ah)
	if err != nil {
		return types.NamespacedName{}, fmt.Errorf("build openclaw actor startup: %w", err)
	}

	desired := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels:    provisionLabels(ah),
		},
		Spec: atev1alpha1.ActorTemplateSpec{
			PauseImage: p.Defaults.PauseImage,
			Runsc:      defaultRunscConfig(p.Defaults),
			Containers: []atev1alpha1.Container{
				{
					Name:  defaultOpenClawContainer,
					Image: workloadImage,
					Ports: []corev1.ContainerPort{{ContainerPort: GatewayPort(ah)}},
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
		return types.NamespacedName{}, fmt.Errorf("set ActorTemplate owner ref: %w", err)
	}

	var existing atev1alpha1.ActorTemplate
	if err := p.Client.Get(ctx, key, &existing); apierrors.IsNotFound(err) {
		if err := p.Client.Create(ctx, desired); err != nil {
			return types.NamespacedName{}, fmt.Errorf("create ActorTemplate %s: %w", key, err)
		}
		return key, nil
	} else if err != nil {
		return types.NamespacedName{}, err
	}
	existing.Spec = desired.Spec
	if err := p.Client.Update(ctx, &existing); err != nil {
		return types.NamespacedName{}, fmt.Errorf("update ActorTemplate %s: %w", key, err)
	}
	return key, nil
}

func (p *Provisioner) actorTemplateReady(ctx context.Context, key types.NamespacedName) (bool, error) {
	var tmpl atev1alpha1.ActorTemplate
	if err := p.Client.Get(ctx, key, &tmpl); err != nil {
		return false, fmt.Errorf("get ActorTemplate %s: %w", key, err)
	}
	return tmpl.Status.Phase == atev1alpha1.PhaseReady, nil
}
