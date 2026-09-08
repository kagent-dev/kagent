package substrate

import (
	"encoding/json"
	"slices"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
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
		Environment: []corev1.EnvVar{{Name: "API_KEY", Value: "secret"}},
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
	if template.GetSnapshotsConfig().GetOnResume().GetFromData() != ateapipb.ResumeSource_RESUME_SOURCE_COLD_BOOT {
		t.Fatalf("unexpected snapshot resume default: %+v", template.GetSnapshotsConfig().GetOnResume())
	}
	environment := map[string]*ateapipb.EnvVar{}
	for _, variable := range container.Env {
		environment[variable.Name] = variable
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

func TestActorTemplateSpecEqualIgnoresServerFields(t *testing.T) {
	left := &ateapipb.ActorTemplate{
		Metadata:   &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "template"},
		Containers: []*ateapipb.Container{{Name: "agent", Image: "agent:v1"}},
	}
	right := proto.CloneOf(left)
	right.Metadata.Uid = "uid"
	right.Metadata.Version = 2
	right.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/golden"}}}
	if !ActorTemplateSpecEqual(left, right) {
		t.Fatal("server-owned fields changed the immutable spec comparison")
	}
	right.Containers[0].Image = "agent:v2"
	if ActorTemplateSpecEqual(left, right) {
		t.Fatal("different container image was accepted")
	}
}
