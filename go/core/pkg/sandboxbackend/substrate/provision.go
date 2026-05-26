package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	AnnotationManagedWorkerPool    = "kagent.dev/substrate-managed-workerpool"
	AnnotationManagedActorTemplate = "kagent.dev/substrate-managed-actortemplate"

	annotationManagedWorkerPool    = AnnotationManagedWorkerPool
	annotationManagedActorTemplate = AnnotationManagedActorTemplate

	defaultWorkerPoolReplicas = int32(1)
	defaultSnapshotsBucket    = "ate-snapshots"
	defaultOpenClawContainer  = "openclaw"
)

// ProvisionDefaults are cluster-wide defaults for auto-provisioned Substrate CRs.
type ProvisionDefaults struct {
	PauseImage           string
	RunscAMD64URL        string
	RunscAMD64SHA256     string
	RunscARM64URL        string
	RunscARM64SHA256     string
	DefaultAteomImage    string
	DefaultWorkloadImage string
}

// ateActorDeleter removes actors from ate-api during harness teardown.
type ateActorDeleter interface {
	deleteActorSequenced(ctx context.Context, actorID string) error
}

// Provisioner ensures WorkerPool and ActorTemplate exist for a substrate AgentHarness.
type Provisioner struct {
	Client   client.Client
	Defaults ProvisionDefaults
	// Ate deletes harness and golden snapshot actors before Substrate CRs are removed.
	Ate ateActorDeleter
}

// EnsureResult describes provisioned Substrate resources.
type EnsureResult struct {
	WorkerPoolRef        types.NamespacedName
	ActorTemplateRef     types.NamespacedName
	ActorTemplateReady   bool
	ManagedWorkerPool    bool
	ManagedActorTemplate bool
}

// Ensure creates or updates Substrate CRs and waits for ActorTemplate Ready.
func (p *Provisioner) Ensure(ctx context.Context, ah *v1alpha2.AgentHarness) (EnsureResult, error) {
	if ah == nil || ah.Spec.Substrate == nil {
		return EnsureResult{}, fmt.Errorf("spec.substrate is required")
	}
	if err := validateSubstrateProvisionSpec(ah); err != nil {
		return EnsureResult{}, err
	}

	// Legacy / advanced: user supplied an existing template.
	if ah.Spec.Substrate.ActorTemplateRef != nil && strings.TrimSpace(ah.Spec.Substrate.ActorTemplateRef.Name) != "" {
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

	_ = managedWP
	return EnsureResult{
		WorkerPoolRef:        wpKey,
		ActorTemplateRef:     tmplKey,
		ActorTemplateReady:   ready,
		ManagedWorkerPool:    managedWP,
		ManagedActorTemplate: true,
	}, nil
}

func validateSubstrateProvisionSpec(ah *v1alpha2.AgentHarness) error {
	sub := ah.Spec.Substrate
	if err := ValidateGatewayTokenSpec(sub); err != nil {
		return err
	}
	if sub.ActorTemplateRef != nil && strings.TrimSpace(sub.ActorTemplateRef.Name) != "" {
		return nil
	}
	loc := substrateSnapshotsLocation(ah)
	if !strings.HasPrefix(loc, "gs://") {
		return fmt.Errorf("spec.substrate.snapshotsConfig.location must be a gs:// URI (Substrate snapshots are GCS-only today)")
	}
	if sub.WorkerPoolRef != nil && strings.TrimSpace(sub.WorkerPoolRef.Name) != "" && sub.WorkerPool != nil {
		return fmt.Errorf("spec.substrate.workerPoolRef and workerPool are mutually exclusive")
	}
	return nil
}

func (p *Provisioner) ensureWorkerPool(ctx context.Context, ah *v1alpha2.AgentHarness) (types.NamespacedName, bool, error) {
	sub := ah.Spec.Substrate
	if sub.WorkerPoolRef != nil && strings.TrimSpace(sub.WorkerPoolRef.Name) != "" {
		ns := sub.WorkerPoolRef.Namespace
		if ns == "" {
			ns = ah.Namespace
		}
		key := types.NamespacedName{Namespace: ns, Name: sub.WorkerPoolRef.Name}
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

func (p *Provisioner) ensureActorTemplate(ctx context.Context, ah *v1alpha2.AgentHarness, wpKey types.NamespacedName) (types.NamespacedName, error) {
	key := types.NamespacedName{Namespace: ah.Namespace, Name: actorTemplateName(ah)}
	workloadImage := strings.TrimSpace(ah.Spec.Substrate.WorkloadImage)
	if workloadImage == "" {
		workloadImage = strings.TrimSpace(p.Defaults.DefaultWorkloadImage)
	}
	if workloadImage == "" {
		workloadImage = openshell.NemoclawSandboxBaseImage
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
					Ports: []corev1.ContainerPort{{ContainerPort: 80}},
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

func defaultRunscConfig(d ProvisionDefaults) atev1alpha1.RunscConfig {
	return atev1alpha1.RunscConfig{
		AMD64: &atev1alpha1.RunscPlatformConfig{
			URL:        d.RunscAMD64URL,
			SHA256Hash: d.RunscAMD64SHA256,
		},
		ARM64: &atev1alpha1.RunscPlatformConfig{
			URL:        d.RunscARM64URL,
			SHA256Hash: d.RunscARM64SHA256,
		},
	}
}

func substrateSnapshotsLocation(ah *v1alpha2.AgentHarness) string {
	if ah == nil {
		return defaultSubstrateSnapshotsLocation("", "")
	}
	if sub := ah.Spec.Substrate; sub != nil && sub.SnapshotsConfig != nil {
		if loc := strings.TrimSpace(sub.SnapshotsConfig.Location); loc != "" {
			return loc
		}
	}
	return defaultSubstrateSnapshotsLocation(ah.Namespace, ah.Name)
}

func defaultSubstrateSnapshotsLocation(namespace, name string) string {
	return fmt.Sprintf("gs://%s/%s/%s", defaultSnapshotsBucket, namespace, name)
}

func provisionLabels(ah *v1alpha2.AgentHarness) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "kagent",
		"kagent.dev/agent-harness":     ah.Name,
	}
}

func workerPoolName(ah *v1alpha2.AgentHarness) string {
	return truncateDNS1123(ah.Name + "-wp")
}

func actorTemplateName(ah *v1alpha2.AgentHarness) string {
	return truncateDNS1123(ah.Name)
}

func truncateDNS1123(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "_", "-"))
	if len(s) > 63 {
		s = strings.TrimRight(s[:63], "-")
	}
	return s
}
