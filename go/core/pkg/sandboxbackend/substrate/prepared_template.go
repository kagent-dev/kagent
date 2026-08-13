package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/preparation"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	PreparedAgentTemplateLabel = "kagent.dev/agent-template"
	PreparedHarnessLabel       = "kagent.dev/harness"
	PreparedRevisionLabel      = "kagent.dev/prepared-revision"
)

// EnsurePreparedTemplate materializes one immutable Kubernetes ActorTemplate revision.
func (p *Lifecycle) EnsurePreparedTemplate(ctx context.Context, bundle *preparation.Bundle, revision string) (*preparation.ActorTemplateRef, error) {
	if p == nil || p.Client == nil || bundle == nil {
		return nil, fmt.Errorf("substrate lifecycle, Kubernetes client, and preparation bundle are required")
	}
	if len(revision) < 12 {
		return nil, fmt.Errorf("prepared revision is invalid")
	}
	workerPool := &atev1alpha1.WorkerPool{}
	workerKey := types.NamespacedName{Namespace: bundle.Namespace, Name: bundle.WorkerPoolName}
	if err := p.Client.Get(ctx, workerKey, workerPool); err != nil {
		return nil, fmt.Errorf("get WorkerPool %s: %w", workerKey, err)
	}

	name := preparedActorTemplateName(bundle.AgentTemplateName, bundle.HarnessName, revision)
	environment := append([]corev1.EnvVar(nil), bundle.Environment...)
	environment = append(environment,
		corev1.EnvVar{Name: "KAGENT_CONFIG_JSON", Value: string(bundle.ConfigJSON)},
		corev1.EnvVar{Name: "KAGENT_AGENT_CARD_JSON", Value: string(bundle.AgentCardJSON)},
	)
	actorEnv := actorTemplateEnvFromPodEnv(environment)
	if len(actorEnv) > 32 {
		return nil, fmt.Errorf("prepared runtime has %d environment variables; Substrate supports at most 32", len(actorEnv))
	}

	template := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: bundle.Namespace,
			Name:      name,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kagent",
				PreparedAgentTemplateLabel:     bundle.AgentTemplateName,
				PreparedHarnessLabel:           bundle.HarnessName,
				PreparedRevisionLabel:          revision[:12],
			},
		},
		Spec: atev1alpha1.ActorTemplateSpec{
			PauseImage:   p.Defaults.PauseImage,
			SandboxClass: atev1alpha1.SandboxClassGvisor,
			Containers: []atev1alpha1.Container{{
				Name:  defaultKagentContainer,
				Image: bundle.Image,
				Env:   actorEnv,
				Readyz: &atev1alpha1.ContainerReadyz{HTTPGet: &atev1alpha1.HTTPGetAction{
					Path: "/readyz",
					Port: 8081,
				}},
				VolumeMounts: []atev1alpha1.VolumeMount{{Name: durableDataVolume, MountPath: durableDataMount}},
			}},
			WorkerSelector: workerSelectorForPool(workerKey),
			SnapshotsConfig: atev1alpha1.SnapshotsConfig{
				Location: bundle.SnapshotLocation,
				OnPause:  atev1alpha1.SnapshotScopeFull,
				OnCommit: atev1alpha1.SnapshotScopeData,
			},
			Volumes: []atev1alpha1.Volume{{
				Name:         durableDataVolume,
				VolumeSource: atev1alpha1.VolumeSource{DurableDir: &atev1alpha1.DurableDirVolumeSource{}},
			}},
		},
	}
	if err := createImmutableObject(ctx, p.Client, template); err != nil {
		return nil, err
	}
	return p.GetPreparedTemplate(ctx, template.Namespace, template.Name)
}

func (p *Lifecycle) GetPreparedTemplate(ctx context.Context, namespace, name string) (*preparation.ActorTemplateRef, error) {
	template := &atev1alpha1.ActorTemplate{}
	if err := p.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, template); err != nil {
		return nil, fmt.Errorf("get prepared ActorTemplate %s/%s: %w", namespace, name, err)
	}
	return actorTemplateRef(template), nil
}

func (p *Lifecycle) DeletePreparedTemplate(ctx context.Context, templateRef preparation.ActorTemplateRef) error {
	template := &atev1alpha1.ActorTemplate{}
	key := types.NamespacedName{Namespace: templateRef.Namespace, Name: templateRef.Name}
	if err := p.Client.Get(ctx, key, template); apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("get prepared ActorTemplate %s: %w", key, err)
	}
	if templateRef.UID != "" && string(template.UID) != templateRef.UID {
		return fmt.Errorf("prepared ActorTemplate %s UID changed", key)
	}
	if err := p.Client.Delete(ctx, template); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete prepared ActorTemplate %s: %w", key, err)
	}
	return nil
}

func preparedActorTemplateName(agentTemplate, harness, revision string) string {
	base := truncateDNS1123(agentTemplate + "-" + harness)
	base = truncateDNS1123To(base, 50)
	return base + "-" + revision[:12]
}

func createImmutableObject(ctx context.Context, kube client.Client, desired client.Object) error {
	existing := desired.DeepCopyObject().(client.Object)
	err := kube.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		if err := kube.Create(ctx, desired); err != nil {
			return fmt.Errorf("create %T %s: %w", desired, client.ObjectKeyFromObject(desired), err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get %T %s: %w", desired, client.ObjectKeyFromObject(desired), err)
	}
	switch current := existing.(type) {
	case *atev1alpha1.ActorTemplate:
		if !apiequality.Semantic.DeepEqual(current.Spec, desired.(*atev1alpha1.ActorTemplate).Spec) {
			return fmt.Errorf("immutable ActorTemplate %s differs from prepared revision", client.ObjectKeyFromObject(desired))
		}
	default:
		return fmt.Errorf("unsupported immutable object %T", desired)
	}
	return nil
}

func actorTemplateRef(template *atev1alpha1.ActorTemplate) *preparation.ActorTemplateRef {
	return &preparation.ActorTemplateRef{
		Namespace:      template.Namespace,
		Name:           template.Name,
		UID:            string(template.UID),
		Phase:          string(template.Status.Phase),
		GoldenSnapshot: strings.TrimSpace(template.Status.GoldenSnapshot),
	}
}
