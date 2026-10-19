package substrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/image"
)

// SandboxPolicy is operator-owned. Its resolved values are pinned in each revision.
type SandboxPolicy struct {
	GuestImage string
	CPU        string
	Memory     string
}

type sandboxInputs struct {
	Kind      string
	Namespace string
	Name      string
	UID       string
	Spec      v1alpha3.SandboxTemplateSpec
	Class     atev1alpha1.SandboxClass
	Policy    SandboxPolicy
}

// SandboxActorTemplate compiles standalone inputs without an Agent Card or Harness.
// The image's entrypoint is replaced by the pinned, platform-managed guest.
func SandboxActorTemplate(template *v1alpha3.SandboxTemplate, class atev1alpha1.SandboxClass, policy SandboxPolicy) (*ateapipb.ActorTemplate, string, json.RawMessage, error) {
	if !image.IsDigestPinned(policy.GuestImage) || !image.IsDigestPinned(template.Spec.Workload.Image) {
		return nil, "", nil, fmt.Errorf("sandbox workload and guest images must be pinned by sha256 digest")
	}
	resources, err := actorResources(policy.CPU, policy.Memory)
	if err != nil {
		return nil, "", nil, err
	}
	switch class {
	case "", atev1alpha1.SandboxClassGvisor, atev1alpha1.SandboxClassMicroVM:
	default:
		return nil, "", nil, fmt.Errorf("unsupported sandbox class %q", class)
	}
	if class == "" {
		class = atev1alpha1.SandboxClassGvisor
	}
	var environment []*ateapipb.EnvVar
	for _, variable := range template.Spec.Env {
		_, trust := egressTrustEnvironment[variable.Name]
		if trust || strings.HasPrefix(variable.Name, "KAGENT_") || strings.HasPrefix(variable.Name, "ATE_") {
			return nil, "", nil, fmt.Errorf("sandbox environment %q is reserved", variable.Name)
		}
		environment = append(environment, &ateapipb.EnvVar{Name: variable.Name, Value: variable.Value})
	}
	for _, name := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "AWS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO"} {
		environment = append(environment, &ateapipb.EnvVar{Name: name, Value: egressTrustMount + "/trust-bundle.pem"})
	}
	if len(environment) > 32 {
		return nil, "", nil, fmt.Errorf("sandbox exceeds Substrate's 32 environment variables")
	}
	inputs := sandboxInputs{Kind: "sandbox", Namespace: template.Namespace, Name: template.Name, UID: string(template.UID), Spec: template.Spec, Class: class, Policy: policy}
	snapshot, err := json.Marshal(inputs)
	if err != nil {
		return nil, "", nil, fmt.Errorf("encode sandbox inputs: %w", err)
	}
	result := &ateapipb.ActorTemplate{
		Metadata:       &ateapipb.ResourceMetadata{Atespace: template.Namespace},
		WorkerSelector: workerSelectorForPool(types.NamespacedName{Namespace: template.Namespace, Name: template.Spec.Substrate.WorkerPoolRef.Name}),
		Resources:      resources,
		Containers: []*ateapipb.Container{{
			Name: "sandbox", Image: template.Spec.Workload.Image,
			Command:      []string{"/run/kagent/guest/usr/local/bin/kagent-sandbox-guest"},
			Args:         []string{"--listen=:80", "--workspace=/data/workspace", "--log-dir=/data/guest-logs"},
			Env:          environment,
			WakeupProbe:  &ateapipb.WakeupProbe{HttpGet: &ateapipb.HttpGetAction{Path: "/readyz", Port: 80}, TimeoutSeconds: 30},
			VolumeMounts: []*ateapipb.VolumeMount{{Name: durableDataVolume, MountPath: durableDataMount}, {Name: "guest", MountPath: "/run/kagent/guest"}, {Name: egressTrustVolume, MountPath: egressTrustMount}},
		}},
		Volumes: []*ateapipb.Volume{
			{Name: durableDataVolume, DurableDir: &ateapipb.DurableDirVolumeSource{}},
			{Name: "guest", Image: &ateapipb.ImageVolumeSource{Reference: policy.GuestImage}},
			{Name: egressTrustVolume, SystemInfo: &ateapipb.SystemInfoVolumeSource{DataSources: []*ateapipb.SystemInfoDataSource{{TrustBundle: &ateapipb.TrustBundleDataSource{Names: []string{"egress-mitm.ate.dev"}, Path: "trust-bundle.pem"}}}}},
		},
		SnapshotConfig: &ateapipb.SnapshotConfig{
			StorageLocation:   template.Spec.Substrate.SnapshotPolicy.Location,
			PreferredFidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES,
			// Pin the server default so immutable comparison survives creation.
			GoldenSnapshotConfig: &ateapipb.GoldenSnapshotConfig{Mode: ateapipb.GoldenSnapshotMode_GOLDEN_SNAPSHOT_MODE_ENABLED},
		},
	}
	// Hash the compiled template and source identity. Live policy references
	// remain in provenance but do not affect immutable runtime contents.
	compiled, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil {
		return nil, "", nil, fmt.Errorf("encode sandbox template: %w", err)
	}
	identity, err := json.Marshal(struct {
		Name     string
		UID      string
		Class    atev1alpha1.SandboxClass
		Template []byte
	}{template.Name, string(template.UID), class, compiled})
	if err != nil {
		return nil, "", nil, fmt.Errorf("encode sandbox revision: %w", err)
	}
	digest := sha256.Sum256(identity)
	revision := hex.EncodeToString(digest[:])
	result.Metadata.Name = "sandbox-" + revision[:40]
	return result, revision, snapshot, nil
}
