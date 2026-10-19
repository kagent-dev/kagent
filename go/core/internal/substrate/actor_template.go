package substrate

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const (
	workerPoolNameLabelKey      = "kagent.dev/worker-pool-name"
	workerPoolNamespaceLabelKey = "kagent.dev/worker-pool-namespace"
	defaultContainerName        = "kagent"
	durableDataVolume           = "data"
	durableDataMount            = "/data"
	// Matches EnvVar.value's maxLength in Substrate's ateapi.proto.
	maxEnvironmentValueRunes = 32768
)

const egressTrustVolume = "egress-trust"
const egressTrustMount = "/run/kagent/egress"

const actorIdentityVolume = "actor-identity"
const actorIdentityMount = "/run/kagent/identity"

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
	name := revisionActorTemplateName(spec.AgentName, revisionID)
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
	environment := withServiceVersion(append([]corev1.EnvVar(nil), spec.Environment...), revisionID.Short())
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
	resources, err := actorResources(spec.CPU, spec.Memory)
	if err != nil {
		return nil, err
	}

	template := &ateapipb.ActorTemplate{
		Metadata:  &ateapipb.ResourceMetadata{Atespace: spec.Namespace, Name: name},
		Resources: resources,
		Containers: []*ateapipb.Container{{
			Name:    defaultContainerName,
			Image:   spec.Image,
			Command: append([]string(nil), spec.Command...),
			Args:    append([]string(nil), spec.Args...),
			Env:     actorEnv,
			WakeupProbe: &ateapipb.WakeupProbe{HttpGet: &ateapipb.HttpGetAction{
				Path: "/readyz",
				Port: 8081,
			}, TimeoutSeconds: 30},
			VolumeMounts: []*ateapipb.VolumeMount{
				{Name: durableDataVolume, MountPath: durableDataMount},
				{Name: egressTrustVolume, MountPath: egressTrustMount},
				{Name: actorIdentityVolume, MountPath: actorIdentityMount},
			},
		}},
		WorkerSelector: workerSelectorForPool(workerKey),
		SnapshotConfig: &ateapipb.SnapshotConfig{
			StorageLocation:   spec.SnapshotLocation,
			PreferredFidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES,
			// Pin the server default so immutable comparison survives creation.
			GoldenSnapshotConfig: &ateapipb.GoldenSnapshotConfig{Mode: ateapipb.GoldenSnapshotMode_GOLDEN_SNAPSHOT_MODE_ENABLED},
		},
		Volumes: []*ateapipb.Volume{
			{Name: durableDataVolume, DurableDir: &ateapipb.DurableDirVolumeSource{}},
			// Substrate regenerates this projection on Run and Restore. A fork
			// therefore routes storage calls as its own actor, never its source.
			{Name: actorIdentityVolume, SystemInfo: &ateapipb.SystemInfoVolumeSource{DataSources: []*ateapipb.SystemInfoDataSource{
				{ActorMetadata: &ateapipb.ActorMetadataDataSource{Items: []*ateapipb.ActorMetadataItem{
					{Field: ateapipb.ActorMetadataField_ACTOR_METADATA_FIELD_NAME, Path: "name"},
					{Field: ateapipb.ActorMetadataField_ACTOR_METADATA_FIELD_ATESPACE, Path: "atespace"},
					{Field: ateapipb.ActorMetadataField_ACTOR_METADATA_FIELD_UID, Path: "uid"},
				}}},
			}}},
			{Name: egressTrustVolume, SystemInfo: &ateapipb.SystemInfoVolumeSource{DataSources: []*ateapipb.SystemInfoDataSource{
				{TrustBundle: &ateapipb.TrustBundleDataSource{Names: []string{"egress-mitm.ate.dev"}, Path: "trust-bundle.pem"}},
			}}},
		},
	}
	for i, image := range spec.SkillImages {
		name := fmt.Sprintf("skill-%d", i)
		template.Volumes = append(template.Volumes, &ateapipb.Volume{
			Name: name, Image: &ateapipb.ImageVolumeSource{Reference: string(image)},
		})
		template.Containers[0].VolumeMounts = append(template.Containers[0].VolumeMounts, &ateapipb.VolumeMount{
			Name: name, MountPath: image.MountPath(),
		})
	}
	if len(template.Volumes) > 32 || len(template.Containers[0].VolumeMounts) > 32 {
		return nil, fmt.Errorf("runtime revision has %d volumes and %d container mounts; Substrate supports at most 32 volumes and 32 mounts per container", len(template.Volumes), len(template.Containers[0].VolumeMounts))
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
		WorkerSelector: template.GetWorkerSelector(),
		Containers:     template.GetContainers(),
		Volumes:        template.GetVolumes(),
		SnapshotConfig: template.GetSnapshotConfig(),
		Resources:      template.GetResources(),
	}
}

// withServiceVersion stamps the revision on the runtime resource. The revision
// digests the environment, so the compiler cannot render it.
func withServiceVersion(environment []corev1.EnvVar, version string) []corev1.EnvVar {
	for index, variable := range environment {
		if variable.Name == env.OTELResourceAttributes.Name() {
			if variable.ValueFrom == nil {
				environment[index].Value = tracing.MergeResourceAttributes(variable.Value, []attribute.KeyValue{semconv.ServiceVersion(version)})
			}
			return environment
		}
	}
	return append(environment, corev1.EnvVar{Name: env.OTELResourceAttributes.Name(), Value: tracing.MergeResourceAttributes("", []attribute.KeyValue{semconv.ServiceVersion(version)})})
}

func revisionActorTemplateName(agentName string, revision translator.RevisionID) string {
	// Twelve digest characters keep names readable while the full digest remains
	// the database identity and immutable-content check.
	base := truncateDNS1123(agentName)
	base = truncateDNS1123To(base, 50)
	return base + "-" + revision.Short()
}

func workerSelectorForPool(pool types.NamespacedName) *ateapipb.Selector {
	// Substrate matches worker labels across the entire fleet and never checks
	// them against the pool's namespace. Placement therefore stays within the
	// referenced pool's namespace only while every pool's labels name its own.
	return &ateapipb.Selector{MatchLabels: map[string]string{
		workerPoolNameLabelKey:      pool.Name,
		workerPoolNamespaceLabelKey: pool.Namespace,
	}}
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
		if size := utf8.RuneCountInString(value.Value); size > maxEnvironmentValueRunes {
			return nil, fmt.Errorf("environment variable %q is %d characters; Substrate supports at most %d", value.Name, size, maxEnvironmentValueRunes)
		}
		seen[value.Name] = struct{}{}
		result = append(result, &ateapipb.EnvVar{Name: value.Name, Value: value.Value})
	}
	return result, nil
}

// actorResources enforces the required limits at the Substrate boundary.
func actorResources(cpu, memory string) (*ateapipb.Resources, error) {
	for _, limit := range []struct{ name, value string }{{"cpu", cpu}, {"memory", memory}} {
		q, err := resource.ParseQuantity(limit.value)
		if err != nil || q.Sign() <= 0 || (limit.name == "cpu" && q.Cmp(resource.MustParse("1000")) >= 0) {
			return nil, fmt.Errorf("invalid runtime %s limit %q", limit.name, limit.value)
		}
	}
	return &ateapipb.Resources{Limits: []*ateapipb.Limits{{Name: "cpu", Quantity: cpu}, {Name: "memory", Quantity: memory}}}, nil
}
