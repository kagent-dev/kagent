package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (p *Provisioner) ensureWorkerPool(ctx context.Context, ah *v1alpha2.AgentHarness) (types.NamespacedName, bool, error) {
	sub := ah.Spec.Substrate
	if sub.WorkerPoolRef != nil && strings.TrimSpace(sub.WorkerPoolRef.Name) != "" {
		key := types.NamespacedName{Namespace: ah.Namespace, Name: sub.WorkerPoolRef.Name}
		var wp atev1alpha1.WorkerPool
		if err := p.Client.Get(ctx, key, &wp); err != nil {
			return types.NamespacedName{}, false, fmt.Errorf("get WorkerPool %s: %w", key, err)
		}
		return key, false, nil
	}

	key := types.NamespacedName{Namespace: ah.Namespace, Name: workerPoolName(ah)}
	replicas := defaultWorkerPoolReplicas
	ateomImage := ""
	if sub.WorkerPool != nil {
		if sub.WorkerPool.Replicas > 0 {
			replicas = sub.WorkerPool.Replicas
		}
		ateomImage = strings.TrimSpace(sub.WorkerPool.AteomImage)
	}
	if ateomImage == "" {
		ateomImage = strings.TrimSpace(p.Defaults.DefaultAteomImage)
	}
	if ateomImage == "" {
		return types.NamespacedName{}, false, fmt.Errorf("ateom image is not configured (set controller substrate ateomImage or spec.substrate.workerPool.ateomImage)")
	}

	desired := &atev1alpha1.WorkerPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels:    provisionLabels(ah),
		},
		Spec: atev1alpha1.WorkerPoolSpec{
			Replicas:   replicas,
			AteomImage: ateomImage,
		},
	}
	if err := controllerutil.SetControllerReference(ah, desired, p.Client.Scheme()); err != nil {
		return types.NamespacedName{}, false, fmt.Errorf("set WorkerPool owner ref: %w", err)
	}

	var existing atev1alpha1.WorkerPool
	if err := p.Client.Get(ctx, key, &existing); apierrors.IsNotFound(err) {
		if err := p.Client.Create(ctx, desired); err != nil {
			return types.NamespacedName{}, false, fmt.Errorf("create WorkerPool %s: %w", key, err)
		}
		return key, true, nil
	} else if err != nil {
		return types.NamespacedName{}, false, err
	}
	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.AteomImage = desired.Spec.AteomImage
	if err := p.Client.Update(ctx, &existing); err != nil {
		return types.NamespacedName{}, false, fmt.Errorf("update WorkerPool %s: %w", key, err)
	}
	return key, true, nil
}
