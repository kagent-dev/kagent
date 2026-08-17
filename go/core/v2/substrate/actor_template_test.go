package substrate

import (
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/v2/translator"
	corev1 "k8s.io/api/core/v1"
)

func TestActorTemplateForRevision(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent",
		Image:          "agent.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkerPoolName: "default", SnapshotLocation: "snapshots",
		ConfigJSON: []byte(`{"instruction":"help"}`), AgentCardJSON: []byte(`{"name":"helper"}`),
		Environment: []corev1.EnvVar{{Name: "API_KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "credentials"}, Key: "key",
		}}}},
	}
	revisionID, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	template, err := ActorTemplateForRevision(spec, revisionID, "pause.example/image@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	if template.Name != "helper-kagent-"+revisionID.Short() {
		t.Fatalf("ActorTemplate = %+v", template)
	}
	container := template.Spec.Containers[0]
	if template.Spec.SandboxClass != atev1alpha1.SandboxClassGvisor || container.Readyz.HTTPGet.Path != "/readyz" || container.Readyz.HTTPGet.Port != 8081 || container.Readyz.TimeoutSeconds != 30 {
		t.Fatalf("unexpected runtime contract: %+v", template.Spec)
	}
	if template.Spec.SnapshotsConfig.OnResume.FromData != atev1alpha1.ResumeSourceColdBoot {
		t.Fatalf("unexpected snapshot resume default: %+v", template.Spec.SnapshotsConfig.OnResume)
	}
	environment := map[string]atev1alpha1.EnvVar{}
	for _, variable := range container.Env {
		environment[variable.Name] = variable
	}
	if environment["KAGENT_CONFIG_JSON"].Value == nil || *environment["KAGENT_CONFIG_JSON"].Value != string(spec.ConfigJSON) {
		t.Fatal("config was not embedded as a non-secret literal")
	}
	if environment["API_KEY"].ValueFrom.SecretKeyRef.Name != "credentials" {
		t.Fatal("credential SecretKeyRef was not preserved")
	}
}
