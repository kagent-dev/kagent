package substrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
)

// SandboxPolicy is operator-owned. Its resolved values are pinned in each revision.
type SandboxPolicy struct {
	GuestImage string
	CPU        string
	Memory     string
}

type sandboxInputs struct {
	Kind      string
	Version   int
	Namespace string
	Name      string
	UID       string
	Spec      v1alpha3.SandboxTemplateSpec
	Class     atev1alpha1.SandboxClass
	Policy    SandboxPolicy
}

var pinnedSandboxImage = regexp.MustCompile(`^[^\s@]+@sha256:[a-f0-9]{64}$`)

// SandboxActorTemplate compiles standalone inputs without an Agent Card or Harness.
// The image's entrypoint is replaced by the pinned, platform-managed guest.
func SandboxActorTemplate(template *v1alpha3.SandboxTemplate, class atev1alpha1.SandboxClass, policy SandboxPolicy) (*ateapipb.ActorTemplate, string, json.RawMessage, error) {
	if !pinnedSandboxImage.MatchString(policy.GuestImage) || !pinnedSandboxImage.MatchString(template.Spec.Workload.Image) {
		return nil, "", nil, fmt.Errorf("sandbox workload and guest images must be pinned by sha256 digest")
	}
	for name, value := range map[string]string{"cpu": policy.CPU, "memory": policy.Memory} {
		q, err := resource.ParseQuantity(value)
		if err != nil || q.Sign() <= 0 || (name == "cpu" && q.Cmp(resource.MustParse("1000")) >= 0) {
			return nil, "", nil, fmt.Errorf("invalid sandbox %s limit %q", name, value)
		}
	}
	config, err := sandboxConfigForClass(class)
	if err != nil {
		return nil, "", nil, err
	}
	if class == "" {
		class = atev1alpha1.SandboxClassGvisor
	}
	var environment []*ateapipb.EnvVar
	for _, variable := range template.Spec.Env {
		if variable.CredentialRef != nil {
			return nil, "", nil, fmt.Errorf("sandbox environment %q cannot inject a credential", variable.Name)
		}
		_, trust := egressTrustEnvironment[variable.Name]
		if trust || strings.HasPrefix(variable.Name, "KAGENT_") || strings.HasPrefix(variable.Name, "ATE_") {
			return nil, "", nil, fmt.Errorf("sandbox environment %q is reserved", variable.Name)
		}
		if variable.Value == nil {
			return nil, "", nil, fmt.Errorf("sandbox environment %q requires a literal value", variable.Name)
		}
		environment = append(environment, &ateapipb.EnvVar{Name: variable.Name, Value: *variable.Value})
	}
	for _, name := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "AWS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO"} {
		environment = append(environment, &ateapipb.EnvVar{Name: name, Value: egressTrustMount + "/trust-bundle.pem"})
	}
	if len(environment) > 32 {
		return nil, "", nil, fmt.Errorf("sandbox exceeds Substrate's 32 environment variables")
	}
	inputs := sandboxInputs{Kind: "sandbox", Version: 1, Namespace: template.Namespace, Name: template.Name, UID: string(template.UID), Spec: template.Spec, Class: class, Policy: policy}
	snapshot, err := json.Marshal(inputs)
	if err != nil {
		return nil, "", nil, fmt.Errorf("encode sandbox inputs: %w", err)
	}
	digest := sha256.Sum256(snapshot)
	revision := hex.EncodeToString(digest[:])
	result := &ateapipb.ActorTemplate{
		Metadata:       &ateapipb.ResourceMetadata{Atespace: template.Namespace, Name: "sandbox-" + revision[:40]},
		SandboxConfig:  config,
		WorkerSelector: workerSelectorForPool(types.NamespacedName{Namespace: template.Namespace, Name: template.Spec.Substrate.WorkerPoolRef.Name}),
		Resources:      &ateapipb.Resources{Limits: []*ateapipb.Limits{{Name: "cpu", Quantity: policy.CPU}, {Name: "memory", Quantity: policy.Memory}}},
		Containers: []*ateapipb.Container{{
			Name: "sandbox", Image: template.Spec.Workload.Image,
			Command:      []string{"/run/kagent/guest/usr/local/bin/kagent-sandbox-guest"},
			Args:         []string{"--listen=:80", "--workspace=/data/workspace", "--log-dir=/data/guest-logs"},
			Env:          environment,
			Readyz:       &ateapipb.ContainerReadyz{HttpGet: &ateapipb.HTTPGetAction{Path: "/readyz", Port: 80}, TimeoutSeconds: 30},
			VolumeMounts: []*ateapipb.VolumeMount{{Name: durableDataVolume, MountPath: durableDataMount}, {Name: "guest", MountPath: "/run/kagent/guest"}, {Name: egressTrustVolume, MountPath: egressTrustMount}},
		}},
		Volumes: []*ateapipb.Volume{
			{Name: durableDataVolume, DurableDir: &ateapipb.DurableDirVolumeSource{}},
			{Name: "guest", Image: &ateapipb.ImageVolumeSource{Reference: policy.GuestImage}},
			{Name: egressTrustVolume, SystemInfo: &ateapipb.SystemInfoVolumeSource{DataSources: []*ateapipb.SystemInfoDataSource{{TrustBundle: &ateapipb.TrustBundleDataSource{Name: "egress-mitm.ate.dev", Path: "trust-bundle.pem"}}}}},
		},
		SnapshotsConfig: &ateapipb.SnapshotsConfig{
			StorageLocation: template.Spec.Substrate.SnapshotPolicy.Location,
			OnPause:         ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
			OnCommit:        ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA,
			OnResume:        &ateapipb.OnResumeConfig{FromData: ateapipb.ResumeSource_RESUME_SOURCE_GOLDEN},
		},
	}
	return result, revision, snapshot, nil
}
