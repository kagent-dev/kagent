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
func ActorTemplateForRevision(spec *translator.Revision, revisionID translator.RevisionID) (*atev1alpha1.ActorTemplate, error) {
	if revisionID.IsZero() {
		return nil, fmt.Errorf("runtime revision ID is required")
	}
	workerKey := types.NamespacedName{Namespace: spec.Namespace, Name: spec.WorkerPoolName}
	name := revisionActorTemplateName(spec.AgentTemplateName, spec.HarnessName, revisionID)
	// Config is passed inline because Substrate ActorTemplates support only
	// literal environment variables. The revision digest already covers both
	// JSON documents.
	environment := append([]corev1.EnvVar(nil), spec.Environment...)
	environment = append(environment,
		corev1.EnvVar{Name: "KAGENT_CONFIG_JSON", Value: string(spec.ConfigJSON)},
		corev1.EnvVar{Name: "KAGENT_AGENT_CARD_JSON", Value: string(spec.AgentCardJSON)},
	)
	actorEnv, err := actorTemplateEnvFromPodEnv(environment)
	if err != nil {
		return nil, err
	}
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
				RevisionLabel:                  revisionID.Short(),
			},
		},
		Spec: atev1alpha1.ActorTemplateSpec{
			// The v2 API intentionally has one default sandbox policy for now.
			SandboxClass: atev1alpha1.SandboxClassGvisor,
			Containers: []atev1alpha1.Container{{
				Name:  defaultContainerName,
				Image: spec.Image,
				Env:   actorEnv,
				Readyz: &atev1alpha1.ContainerReadyz{HTTPGet: &atev1alpha1.HTTPGetAction{
					Path: "/readyz",
					Port: 8081,
				}, TimeoutSeconds: 30},
				VolumeMounts: []atev1alpha1.VolumeMount{{Name: durableDataVolume, MountPath: durableDataMount}},
			}},
			WorkerSelector: workerSelectorForPool(workerKey),
			SnapshotsConfig: atev1alpha1.SnapshotsConfig{
				Location: spec.SnapshotLocation,
				OnPause:  atev1alpha1.SnapshotScopeFull,
				OnCommit: atev1alpha1.SnapshotScopeData,
				OnResume: atev1alpha1.OnResumeConfig{FromData: atev1alpha1.ResumeSourceColdBoot},
			},
			Volumes: []atev1alpha1.Volume{{
				Name:         durableDataVolume,
				VolumeSource: atev1alpha1.VolumeSource{DurableDir: &atev1alpha1.DurableDirVolumeSource{}},
			}},
		},
	}
	return template, nil
}

func revisionActorTemplateName(agentTemplate, harness string, revision translator.RevisionID) string {
	// Twelve digest characters keep names readable while the full digest remains
	// the database identity and immutable-content check.
	base := truncateDNS1123(agentTemplate + "-" + harness)
	base = truncateDNS1123To(base, 50)
	return base + "-" + revision.Short()
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

func actorTemplateEnvFromPodEnv(environment []corev1.EnvVar) ([]atev1alpha1.EnvVar, error) {
	// Substrate ActorTemplates accept only literal values. The compiler resolves
	// Secret references before revisions reach this boundary.
	result := make([]atev1alpha1.EnvVar, 0, len(environment))
	seen := make(map[string]struct{}, len(environment))
	for _, value := range environment {
		if value.Name == "" {
			continue
		}
		if value.ValueFrom != nil {
			return nil, fmt.Errorf("runtime environment variable %q is not resolved to a literal value", value.Name)
		}
		if _, exists := seen[value.Name]; exists {
			continue
		}
		seen[value.Name] = struct{}{}
		result = append(result, atev1alpha1.EnvVar{Name: value.Name, Value: value.Value})
	}
	return result, nil
}
