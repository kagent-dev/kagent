package substrate

import (
	"context"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/preparation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsurePreparedTemplateCreatesOnlyActorTemplate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := atev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	workerPool := &atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "agents"}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(workerPool).Build()
	lifecycle := &Lifecycle{Client: kube, Defaults: LifecycleDefaults{PauseImage: "pause.example/image@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	bundle := &preparation.Bundle{
		Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent",
		Image:          "agent.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkerPoolName: "default", SnapshotLocation: "snapshots",
		ConfigJSON: []byte(`{"instruction":"help"}`), AgentCardJSON: []byte(`{"name":"helper"}`),
		Environment: []corev1.EnvVar{{Name: "API_KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "credentials"}, Key: "key",
		}}}},
	}
	revision := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	backing, err := lifecycle.EnsurePreparedTemplate(context.Background(), bundle, revision)
	if err != nil {
		t.Fatal(err)
	}
	if backing.Kind != "ActorTemplate" || backing.Name != "helper-kagent-0123456789ab" {
		t.Fatalf("backing = %+v", backing)
	}
	template := &atev1alpha1.ActorTemplate{}
	if err := kube.Get(context.Background(), client.ObjectKey{Namespace: "agents", Name: backing.Name}, template); err != nil {
		t.Fatal(err)
	}
	container := template.Spec.Containers[0]
	if template.Spec.SandboxClass != atev1alpha1.SandboxClassGvisor || container.Readyz.HTTPGet.Path != "/readyz" || container.Readyz.HTTPGet.Port != 8081 {
		t.Fatalf("unexpected runtime contract: %+v", template.Spec)
	}
	environment := map[string]atev1alpha1.EnvVar{}
	for _, variable := range container.Env {
		environment[variable.Name] = variable
	}
	if environment["KAGENT_CONFIG_JSON"].Value == nil || *environment["KAGENT_CONFIG_JSON"].Value != string(bundle.ConfigJSON) {
		t.Fatal("config was not embedded as a non-secret literal")
	}
	if environment["API_KEY"].ValueFrom.SecretKeyRef.Name != "credentials" {
		t.Fatal("credential SecretKeyRef was not preserved")
	}
	secrets := &corev1.SecretList{}
	if err := kube.List(context.Background(), secrets, client.InNamespace("agents")); err != nil {
		t.Fatal(err)
	}
	if len(secrets.Items) != 0 {
		t.Fatalf("created %d config Secrets", len(secrets.Items))
	}
}
