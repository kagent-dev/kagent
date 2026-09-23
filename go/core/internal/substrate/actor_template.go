package substrate

import (
	"encoding/json"
	"fmt"
	"strings"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
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

const egressTrustVolume = "egress-trust"
const egressTrustMount = "/run/kagent/egress"

var egressTrustEnvironment = map[string]struct{}{
	"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {}, "REQUESTS_CA_BUNDLE": {}, "AWS_CA_BUNDLE": {},
	"NODE_EXTRA_CA_CERTS": {}, "CURL_CA_BUNDLE": {}, "GIT_SSL_CAINFO": {},
}

// ActorTemplateForRevision constructs the immutable ate-api resource for a
// compiled revision. It performs no reads or writes, which makes it safe to use
// inside a KRT transformation.
func ActorTemplateForRevision(spec *translator.Revision, revisionID translator.RevisionID) (*ateapipb.ActorTemplate, error) {
	if revisionID.IsZero() {
		return nil, fmt.Errorf("runtime revision ID is required")
	}
	workerKey := types.NamespacedName{Namespace: spec.Namespace, Name: spec.WorkerPoolName}
	name := revisionActorTemplateName(spec.AgentTemplateName, spec.HarnessName, revisionID)
	// Config and SDK placeholders contain no Secret values. Render the typed
	// card only at this boundary.
	card, err := apia2a.FromProtoAgentCard(spec.AgentCard)
	if err != nil {
		return nil, fmt.Errorf("convert runtime Agent Card: %w", err)
	}
	cardJSON, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("render runtime Agent Card: %w", err)
	}
	environment := append([]corev1.EnvVar(nil), spec.Environment...)
	// The systemInfo trustBundle volume below projects the gateway CA. These
	// variables tell each TLS client to trust it; mounting the file alone does
	// not configure trust. The gateway intercepts HTTPS even without credentials.
	for _, variable := range environment {
		if _, owned := egressTrustEnvironment[variable.Name]; owned {
			return nil, fmt.Errorf("runtime environment %q conflicts with gateway trust", variable.Name)
		}
	}
	for _, name := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "AWS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO"} {
		environment = append(environment, corev1.EnvVar{Name: name, Value: egressTrustMount + "/trust-bundle.pem"})
	}
	environment = append(environment, corev1.EnvVar{Name: "SSL_CERT_DIR", Value: egressTrustMount})
	environment = append(environment,
		corev1.EnvVar{Name: "KAGENT_CONFIG_JSON", Value: string(spec.ConfigJSON)},
		corev1.EnvVar{Name: "KAGENT_AGENT_CARD_JSON", Value: string(cardJSON)},
	)
	actorEnv, err := actorTemplateEnvFromPodEnv(environment)
	if err != nil {
		return nil, err
	}
	if len(actorEnv) > 32 {
		return nil, fmt.Errorf("runtime revision has %d environment variables; Substrate supports at most 32", len(actorEnv))
	}
	sandboxConfig, err := sandboxConfigForClass(spec.SandboxClass)
	if err != nil {
		return nil, err
	}

	template := &ateapipb.ActorTemplate{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: spec.Namespace, Name: name},
		SandboxConfig: sandboxConfig,
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
			VolumeMounts: []*ateapipb.VolumeMount{{Name: durableDataVolume, MountPath: durableDataMount}, {Name: egressTrustVolume, MountPath: egressTrustMount}},
		}},
		WorkerSelector: workerSelectorForPool(workerKey),
		SnapshotsConfig: &ateapipb.SnapshotsConfig{
			StorageLocation: spec.SnapshotLocation,
			OnPause:         ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
			OnCommit:        ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA,
			OnResume:        &ateapipb.OnResumeConfig{FromData: ateapipb.ResumeSource_RESUME_SOURCE_GOLDEN},
		},
		Volumes: []*ateapipb.Volume{
			{Name: durableDataVolume, DurableDir: &ateapipb.DurableDirVolumeSource{}},
			{Name: egressTrustVolume, SystemInfo: &ateapipb.SystemInfoVolumeSource{DataSources: []*ateapipb.SystemInfoDataSource{
				{TrustBundle: &ateapipb.TrustBundleDataSource{Name: "egress-mitm.ate.dev", Path: "trust-bundle.pem"}},
			}}},
		},
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
	// Only non-secret literals and SDK placeholders may cross this boundary.
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

func sandboxConfigForClass(class atev1alpha1.SandboxClass) (*ateapipb.SandboxConfig, error) {
	switch class {
	case "", atev1alpha1.SandboxClassGvisor:
		return &ateapipb.SandboxConfig{
			SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR,
			ConfigName:   "gvisor-default",
		}, nil
	case atev1alpha1.SandboxClassMicroVM:
		return &ateapipb.SandboxConfig{
			SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_MICROVM,
			ConfigName:   "microvm",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported sandbox class %q", class)
	}
}
