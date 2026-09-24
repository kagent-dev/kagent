package substrate

import (
	"encoding/json"
	"slices"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
)

func TestActorTemplateForRevision(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent",
		Image:          "agent.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Command:        []string{"/agent"},
		Args:           []string{"serve"},
		WorkerPoolName: "default", SnapshotLocation: "snapshots",
		ConfigJSON: []byte(`{"instruction":"help"}`), AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{Streaming: new(true)},
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"}},
		Environment:        []corev1.EnvVar{{Name: "API_KEY", Value: translator.CredentialPlaceholder}},
		EgressDestinations: []string{"github.com", "api.anthropic.com"},
		Credentials:        []egress.Credential{{Hostname: "api.anthropic.com", Header: "x-api-key", URI: "ate-secret://kubernetes.io/agents/anthropic/key"}},
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
	trust := template.Volumes[1].GetSystemInfo().GetDataSources()[0].GetTrustBundle()
	if trust.GetName() != "egress-mitm.ate.dev" || trust.GetPath() != "trust-bundle.pem" || container.VolumeMounts[1].GetMountPath() != egressTrustMount {
		t.Fatal("gateway trust bundle was not projected")
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
	wantPolicy, err := ActorEgressPolicy("agents", spec.EgressDestinations, spec.Credentials)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&ateapipb.EgressPolicy{Rules: template.GetDefaultEgressPolicy().GetRules()}, &ateapipb.EgressPolicy{Rules: wantPolicy.GetRules()}) {
		t.Fatalf("template default egress policy = %v, want %v", template.GetDefaultEgressPolicy(), wantPolicy)
	}
	if injected := template.GetDefaultEgressPolicy().GetRules()[0].GetHostnames().GetEffects().GetInjectStaticHeaders(); len(injected) != 1 || injected[0].GetHeader() != "x-api-key" {
		t.Fatalf("credential injection missing from the template policy: %v", template.GetDefaultEgressPolicy())
	}
}

func TestActorTemplateForRevisionDeniesAllEgressWithoutDestinations(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent",
		Image:          "agent.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkerPoolName: "default", SnapshotLocation: "snapshots",
		ConfigJSON: []byte(`{}`), AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{},
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"}},
	}
	revisionID, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	template, err := ActorTemplateForRevision(spec, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if template.GetDefaultEgressPolicy() == nil || len(template.GetDefaultEgressPolicy().GetRules()) != 0 {
		t.Fatalf("a revision without destinations must carry an empty (deny-all) policy, got %v", template.GetDefaultEgressPolicy())
	}
	invalid := *spec
	invalid.EgressDestinations = []string{"https://github.com"}
	if _, err := ActorTemplateForRevision(&invalid, revisionID); err == nil {
		t.Fatal("an invalid destination must fail template compilation")
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
	right = proto.CloneOf(left)
	right.DefaultEgressPolicy = &ateapipb.EgressPolicyTemplate{Rules: []*ateapipb.EgressRule{{Hostnames: &ateapipb.HostnameRule{Patterns: []string{"github.com"}}}}}
	if ActorTemplateSpecEqual(left, right) {
		t.Fatal("different default egress policy was accepted")
	}
}
