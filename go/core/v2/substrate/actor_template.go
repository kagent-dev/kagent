package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/v2/translator"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	workerPoolLabelKey   = "kagent.dev/worker-pool"
	defaultContainerName = "kagent"
	durableDataVolume    = "data"
	durableDataMount     = "/data"
)

type Lifecycle struct {
	Client     client.Client
	PauseImage string
}

type ActorTemplateRef struct {
	Namespace      string
	Name           string
	UID            string
	Phase          string
	GoldenSnapshot string
}

const (
	RevisionAgentTemplateLabel = "kagent.dev/agent-template"
	RevisionHarnessLabel       = "kagent.dev/harness"
	RevisionLabel              = "kagent.dev/revision"
)

// EnsureActorTemplate materializes one immutable Kubernetes ActorTemplate revision.
func (p *Lifecycle) EnsureActorTemplate(ctx context.Context, spec *translator.Revision, revisionID string) (*ActorTemplateRef, error) {
	if p == nil || p.Client == nil || spec == nil {
		return nil, fmt.Errorf("substrate lifecycle, Kubernetes client, and runtime revision spec are required")
	}
	if len(revisionID) < 12 {
		return nil, fmt.Errorf("runtime revision is invalid")
	}
	workerPool := &atev1alpha1.WorkerPool{}
	workerKey := types.NamespacedName{Namespace: spec.Namespace, Name: spec.WorkerPoolName}
	if err := p.Client.Get(ctx, workerKey, workerPool); err != nil {
		return nil, fmt.Errorf("get WorkerPool %s: %w", workerKey, err)
	}

	name := revisionActorTemplateName(spec.AgentTemplateName, spec.HarnessName, revisionID)
	environment := append([]corev1.EnvVar(nil), spec.Environment...)
	environment = append(environment,
		corev1.EnvVar{Name: "KAGENT_CONFIG_JSON", Value: string(spec.ConfigJSON)},
		corev1.EnvVar{Name: "KAGENT_AGENT_CARD_JSON", Value: string(spec.AgentCardJSON)},
	)
	actorEnv := actorTemplateEnvFromPodEnv(environment)
	if len(actorEnv) > 32 {
		return nil, fmt.Errorf("runtime revision has %d environment variables; Substrate supports at most 32", len(actorEnv))
	}

	template := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: spec.Namespace,
			Name:      name,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kagent",
				RevisionAgentTemplateLabel:     spec.AgentTemplateName,
				RevisionHarnessLabel:           spec.HarnessName,
				RevisionLabel:                  revisionID[:12],
			},
		},
		Spec: atev1alpha1.ActorTemplateSpec{
			PauseImage:   p.PauseImage,
			SandboxClass: atev1alpha1.SandboxClassGvisor,
			Containers: []atev1alpha1.Container{{
				Name:  defaultContainerName,
				Image: spec.Image,
				Env:   actorEnv,
				Readyz: &atev1alpha1.ContainerReadyz{HTTPGet: &atev1alpha1.HTTPGetAction{
					Path: "/readyz",
					Port: 8081,
				}},
				VolumeMounts: []atev1alpha1.VolumeMount{{Name: durableDataVolume, MountPath: durableDataMount}},
			}},
			WorkerSelector: workerSelectorForPool(workerKey),
			SnapshotsConfig: atev1alpha1.SnapshotsConfig{
				Location: spec.SnapshotLocation,
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
	return p.GetActorTemplate(ctx, template.Namespace, template.Name)
}

func (p *Lifecycle) GetActorTemplate(ctx context.Context, namespace, name string) (*ActorTemplateRef, error) {
	template := &atev1alpha1.ActorTemplate{}
	if err := p.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, template); err != nil {
		return nil, fmt.Errorf("get runtime ActorTemplate %s/%s: %w", namespace, name, err)
	}
	return actorTemplateRef(template), nil
}

func (p *Lifecycle) DeleteActorTemplate(ctx context.Context, templateRef ActorTemplateRef) error {
	template := &atev1alpha1.ActorTemplate{}
	key := types.NamespacedName{Namespace: templateRef.Namespace, Name: templateRef.Name}
	if err := p.Client.Get(ctx, key, template); apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("get runtime ActorTemplate %s: %w", key, err)
	}
	if templateRef.UID != "" && string(template.UID) != templateRef.UID {
		return fmt.Errorf("runtime ActorTemplate %s UID changed", key)
	}
	if err := p.Client.Delete(ctx, template); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete runtime ActorTemplate %s: %w", key, err)
	}
	return nil
}

func revisionActorTemplateName(agentTemplate, harness, revision string) string {
	base := truncateDNS1123(agentTemplate + "-" + harness)
	base = truncateDNS1123To(base, 50)
	return base + "-" + revision[:12]
}

func workerSelectorForPool(pool types.NamespacedName) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{workerPoolLabelKey: pool.Name}}
}

func truncateDNS1123(value string) string {
	return truncateDNS1123To(value, 63)
}

func truncateDNS1123To(value string, limit int) string {
	value = strings.ToLower(strings.ReplaceAll(value, "_", "-"))
	if len(value) > limit {
		value = strings.TrimRight(value[:limit], "-")
	}
	return value
}

func actorTemplateEnvFromPodEnv(environment []corev1.EnvVar) []atev1alpha1.EnvVar {
	result := make([]atev1alpha1.EnvVar, 0, len(environment))
	seen := make(map[string]struct{}, len(environment))
	for _, value := range environment {
		if value.Name == "" {
			continue
		}
		var converted atev1alpha1.EnvVar
		switch {
		case value.ValueFrom == nil:
			converted = atev1alpha1.EnvVar{Name: value.Name, Value: &value.Value}
		case value.ValueFrom.SecretKeyRef != nil:
			ref := value.ValueFrom.SecretKeyRef
			converted = atev1alpha1.EnvVar{Name: value.Name, ValueFrom: &atev1alpha1.EnvVarSource{
				SecretKeyRef: &atev1alpha1.SecretKeySelector{Name: ref.Name, Key: ref.Key, Optional: ref.Optional},
			}}
		default:
			continue
		}
		if _, exists := seen[value.Name]; exists {
			continue
		}
		seen[value.Name] = struct{}{}
		result = append(result, converted)
	}
	return result
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
			return fmt.Errorf("immutable ActorTemplate %s differs from runtime revision", client.ObjectKeyFromObject(desired))
		}
	default:
		return fmt.Errorf("unsupported immutable object %T", desired)
	}
	return nil
}

func actorTemplateRef(template *atev1alpha1.ActorTemplate) *ActorTemplateRef {
	return &ActorTemplateRef{
		Namespace:      template.Namespace,
		Name:           template.Name,
		UID:            string(template.UID),
		Phase:          string(template.Status.Phase),
		GoldenSnapshot: strings.TrimSpace(template.Status.GoldenSnapshot),
	}
}
