package substrate

import (
	"encoding/json"
	"slices"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
)

func TestActorTemplateSandboxClass(t *testing.T) {
	for _, tt := range []struct {
		name       string
		class      atev1alpha1.SandboxClass
		wantClass  ateapipb.SandboxClass
		wantConfig string
		wantError  string
	}{
		{name: "default", wantClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR, wantConfig: "gvisor-default"},
		{name: "gvisor", class: atev1alpha1.SandboxClassGvisor, wantClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR, wantConfig: "gvisor-default"},
		{name: "microvm", class: atev1alpha1.SandboxClassMicroVM, wantClass: ateapipb.SandboxClass_SANDBOX_CLASS_MICROVM, wantConfig: "microvm"},
		{name: "unsupported", class: "unsupported", wantError: `unsupported sandbox class "unsupported"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec := &translator.Revision{
				Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent",
				WorkerPoolName: "pool",
				AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{Streaming: new(true)},
					SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}},
					DefaultInputModes:   []string{"text"}, DefaultOutputModes: []string{"text"},
				},
			}
			// Exercise builder validation independently of Digest's validation.
			id, err := spec.Digest()
			require.NoError(t, err)
			spec.SandboxClass = tt.class
			template, err := ActorTemplateForRevision(spec, id)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				require.Nil(t, template)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantClass, template.GetSandboxConfig().GetSandboxClass())
			require.Equal(t, tt.wantConfig, template.GetSandboxConfig().GetConfigName())
			require.Equal(t, map[string]string{workerPoolLabelKey: "pool"}, template.GetWorkerSelector().GetMatchLabels())
		})
	}
}

func TestActorTemplateForRevision(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent",
		Image:          "agent.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Command:        []string{"/agent"},
		Args:           []string{"serve"},
		WorkerPoolName: "default", SnapshotLocation: "snapshots",
		ConfigJSON: []byte(`{"instruction":"help"}`), AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{Streaming: new(true)},
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"}},
		Environment: []corev1.EnvVar{{Name: "API_KEY", Value: translator.CredentialPlaceholder}},
	}
	revisionID, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	template, err := ActorTemplateForRevision(spec, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if template.GetMetadata().GetAtespace() != "agents" || template.GetMetadata().GetName() != "helper-kagent-"+revisionID.Short() {
		t.Fatalf("ActorTemplate = %+v", template)
	}
	container := template.GetContainers()[0]
	if !slices.Equal(container.Command, spec.Command) || !slices.Equal(container.Args, spec.Args) {
		t.Fatalf("container command/args = %v %v", container.Command, container.Args)
	}
	if template.GetSandboxConfig().GetSandboxClass() != ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR || template.GetSandboxConfig().GetConfigName() != "gvisor-default" || container.GetReadyz().GetHttpGet().GetPath() != "/readyz" || container.GetReadyz().GetHttpGet().GetPort() != 8081 || container.GetReadyz().GetTimeoutSeconds() != 30 {
		t.Fatalf("unexpected runtime contract: %+v", template)
	}
	if template.GetSnapshotsConfig().GetOnResume().GetFromData() != ateapipb.ResumeSource_RESUME_SOURCE_GOLDEN {
		t.Fatalf("unexpected snapshot resume default: %+v", template.GetSnapshotsConfig().GetOnResume())
	}
	if template.GetSnapshotsConfig().GetOnPause() != ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL {
		t.Fatalf("unexpected pause snapshot scope: %s", template.GetSnapshotsConfig().GetOnPause())
	}
	environment := map[string]*ateapipb.EnvVar{}
	for _, variable := range container.Env {
		environment[variable.Name] = variable
	}
	for _, name := range []string{"SSL_CERT_FILE", "AWS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE"} {
		if environment[name].Value != egressTrustMount+"/trust-bundle.pem" {
			t.Fatalf("missing gateway trust for %s", name)
		}
	}
	trust := template.Volumes[2].GetSystemInfo().GetDataSources()[0].GetTrustBundle()
	if trust.GetName() != "egress-mitm.ate.dev" || trust.GetPath() != "trust-bundle.pem" || container.VolumeMounts[1].GetMountPath() != egressTrustMount {
		t.Fatal("gateway trust bundle was not projected")
	}
	identity := template.Volumes[1].GetSystemInfo().GetDataSources()[0].GetActorMetadata().GetItems()
	if len(identity) != 3 || identity[0].GetField() != ateapipb.ActorMetadataField_ACTOR_METADATA_FIELD_NAME || identity[0].GetPath() != "name" || container.VolumeMounts[2].GetMountPath() != actorIdentityMount {
		t.Fatal("actor routing identity was not projected")
	}
	var rendered a2atype.AgentCard
	if err := json.Unmarshal([]byte(environment["KAGENT_AGENT_CARD_JSON"].Value), &rendered); err != nil {
		t.Fatal(err)
	}
	if rendered.Name != spec.AgentCard.Name || rendered.Description != "" || !rendered.Capabilities.Streaming || len(rendered.Skills) != 0 {
		t.Fatalf("runtime card = %#v", rendered)
	}
	if environment["KAGENT_CONFIG_JSON"].Value != string(spec.ConfigJSON) {
		t.Fatal("config was not embedded as a non-secret literal")
	}
}

func TestActorTemplateStampsTheRevisionOnTheResource(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent", WorkerPoolName: "default",
		AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{},
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}},
			DefaultInputModes:   []string{"text"}, DefaultOutputModes: []string{"text"}},
		Environment: []corev1.EnvVar{{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "gen_ai.agent.name=helper-kagent,service.version=forged"}},
	}
	revisionID, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	template, err := ActorTemplateForRevision(spec, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, variable := range template.GetContainers()[0].Env {
		if variable.Name == "OTEL_RESOURCE_ATTRIBUTES" {
			if want := "gen_ai.agent.name=helper-kagent,service.version=" + revisionID.Short(); variable.Value != want {
				t.Fatalf("resource attributes = %q, want %q", variable.Value, want)
			}
			if spec.Environment[0].Value != "gen_ai.agent.name=helper-kagent,service.version=forged" {
				t.Fatal("stamping the revision changed the compiled revision")
			}
			return
		}
	}
	t.Fatal("resource attributes missing from the actor")
}

func TestActorTemplateSpecEqualIgnoresServerFields(t *testing.T) {
	left := &ateapipb.ActorTemplate{
		Metadata:   &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "template"},
		Containers: []*ateapipb.Container{{Name: "agent", Image: "agent:v1"}},
	}
	right := proto.CloneOf(left)
	right.Metadata.Uid = "uid"
	right.Metadata.Version = 2
	right.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"}}}
	if !ActorTemplateSpecEqual(left, right) {
		t.Fatal("server-owned fields changed the immutable spec comparison")
	}
	right.Containers[0].Image = "agent:v2"
	if ActorTemplateSpecEqual(left, right) {
		t.Fatal("different container image was accepted")
	}
}
