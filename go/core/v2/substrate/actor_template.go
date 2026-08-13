package substrate

import (
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/v2/translator"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	workerPoolLabelKey   = "kagent.dev/worker-pool"
	defaultContainerName = "kagent"
	durableDataVolume    = "data"
	durableDataMount     = "/data"
)

const (
	// Revision labels connect the temporary Kubernetes ActorTemplate back to the
	// public kagent resources and let controller watches find their owner.
	RevisionAgentTemplateLabel = "kagent.dev/agent-template"
	RevisionHarnessLabel       = "kagent.dev/harness"
	RevisionLabel              = "kagent.dev/revision"
)

// ActorTemplateForRevision constructs the immutable Kubernetes object for a
// compiled revision. It performs no reads or writes, which makes it safe to use
// inside a KRT transformation.
func ActorTemplateForRevision(spec *translator.Revision, revisionID, pauseImage string) (*atev1alpha1.ActorTemplate, error) {
	if len(revisionID) < 12 {
		return nil, fmt.Errorf("runtime revision is invalid")
	}
	workerKey := types.NamespacedName{Namespace: spec.Namespace, Name: spec.WorkerPoolName}
	name := revisionActorTemplateName(spec.AgentTemplateName, spec.HarnessName, revisionID)
	// Config is passed inline because Substrate ActorTemplates support literal
	// and Secret-backed environment variables but not generated ConfigMaps or
	// Secrets. The revision digest already covers both JSON documents.
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
			// The v2 API intentionally has one default sandbox policy for now.
			PauseImage:   pauseImage,
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
	return template, nil
}

func revisionActorTemplateName(agentTemplate, harness, revision string) string {
	// Twelve digest characters keep names readable while the full digest remains
	// the database identity and immutable-content check.
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
	// The Substrate type supports only literal values and Secret keys. Other Pod
	// env sources are skipped because they cannot be represented faithfully.
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
