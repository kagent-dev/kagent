package substrate

import (
	"slices"
	"testing"

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
		ConfigJSON: []byte(`{"instruction":"help"}`), AgentCardJSON: []byte(`{"name":"helper"}`),
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
	right.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenSnapshot: &ateapipb.ObjectRef{Name: "golden"}}}
	if !ActorTemplateSpecEqual(left, right) {
		t.Fatal("server-owned fields changed the immutable spec comparison")
	}
	right.Containers[0].Image = "agent:v2"
	if ActorTemplateSpecEqual(left, right) {
		t.Fatal("different container image was accepted")
	}
}
