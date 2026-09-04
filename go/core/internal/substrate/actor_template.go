package substrate

import (
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	workerPoolLabelKey   = "kagent.dev/worker-pool"
	defaultContainerName = "kagent"
	durableDataVolume    = "data"
	durableDataMount     = "/data"
)

// ActorTemplateForRevision constructs the immutable ate-api resource for a
// compiled revision. It performs no reads or writes, which makes it safe to use
// inside a KRT transformation.
func ActorTemplateForRevision(spec *translator.Revision, revisionID translator.RevisionID) (*ateapipb.ActorTemplate, error) {
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

	template := &ateapipb.ActorTemplate{
		Metadata: &ateapipb.ResourceMetadata{Atespace: spec.Namespace, Name: name},
		// The v2 API intentionally has one default sandbox policy for now.
		SandboxConfig: &ateapipb.SandboxConfig{
			SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR,
			ConfigName:   "gvisor-default",
		},
		Containers: []*ateapipb.Container{{
			Name:    defaultContainerName,
			Image:   spec.Image,
			Command: append([]string(nil), spec.Command...),
			Args:    append([]string(nil), spec.Args...),
			Env:     actorEnv,
			Readyz: &ateapipb.ContainerReadyz{HttpGet: &ateapipb.HTTPGetAction{
				Path: "/readyz",
				Port: 8081,
			}, TimeoutSeconds: 30},
			VolumeMounts: []*ateapipb.VolumeMount{{Name: durableDataVolume, MountPath: durableDataMount}},
		}},
		WorkerSelector: workerSelectorForPool(workerKey),
		SnapshotsConfig: &ateapipb.SnapshotsConfig{
			StorageLocation: spec.SnapshotLocation,
			OnPause:         ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
			OnCommit:        ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA,
			OnResume:        &ateapipb.OnResumeConfig{FromData: ateapipb.ResumeSource_RESUME_SOURCE_GOLDEN},
		},
		Volumes: []*ateapipb.Volume{{Name: durableDataVolume, DurableDir: &ateapipb.DurableDirVolumeSource{}}},
	}
	return template, nil
}

// ActorTemplateSpecEqual compares the client-owned immutable fields of two
// templates, excluding server-owned metadata and golden-snapshot status.
func ActorTemplateSpecEqual(left, right *ateapipb.ActorTemplate) bool {
	return proto.Equal(actorTemplateSpec(left), actorTemplateSpec(right))
}

func actorTemplateSpec(template *ateapipb.ActorTemplate) *ateapipb.ActorTemplate {
	if template == nil {
		return nil
	}
	return &ateapipb.ActorTemplate{
		Metadata: &ateapipb.ResourceMetadata{
			Atespace: template.GetMetadata().GetAtespace(),
			Name:     template.GetMetadata().GetName(),
		},
		WorkerSelector:  template.GetWorkerSelector(),
		Containers:      template.GetContainers(),
		Volumes:         template.GetVolumes(),
		SnapshotsConfig: template.GetSnapshotsConfig(),
		SandboxConfig:   template.GetSandboxConfig(),
		Resources:       template.GetResources(),
	}
}

func revisionActorTemplateName(agentTemplate, harness string, revision translator.RevisionID) string {
	// Twelve digest characters keep names readable while the full digest remains
	// the database identity and immutable-content check.
	base := truncateDNS1123(agentTemplate + "-" + harness)
	base = truncateDNS1123To(base, 50)
	return base + "-" + revision.Short()
}

func workerSelectorForPool(pool types.NamespacedName) *ateapipb.Selector {
	return &ateapipb.Selector{MatchLabels: map[string]string{workerPoolLabelKey: pool.Name}}
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

func actorTemplateEnvFromPodEnv(environment []corev1.EnvVar) ([]*ateapipb.EnvVar, error) {
	// Substrate ActorTemplates accept only literal values. The compiler resolves
	// Secret references before revisions reach this boundary.
	result := make([]*ateapipb.EnvVar, 0, len(environment))
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
		result = append(result, &ateapipb.EnvVar{Name: value.Name, Value: value.Value})
	}
	return result, nil
}
