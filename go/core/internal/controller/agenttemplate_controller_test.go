package controller

import (
	"context"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type preparedStore struct {
	attachment database.PreparedAttachment
	revision   database.PreparedRevision
}

func (s *preparedStore) UpsertPreparedAttachment(_ context.Context, value database.PreparedAttachment) error {
	s.attachment = value
	return nil
}
func (s *preparedStore) UpsertPreparedRevision(_ context.Context, value database.PreparedRevision) error {
	s.revision = value
	return nil
}
func (s *preparedStore) MarkPreparedRevisionSuccessful(_ context.Context, value database.PreparedAttachment) error {
	s.attachment = value
	return nil
}
func (*preparedStore) RetireAgentTemplateAttachments(context.Context, string, string) error {
	return nil
}
func (*preparedStore) RetireHarnessAttachment(context.Context, string, string, string) error {
	return nil
}
func (*preparedStore) RetireOtherHarnessAttachments(context.Context, string, string, []string) error {
	return nil
}
func (*preparedStore) ListUnreferencedPreparedRevisions(context.Context) ([]database.PreparedRevisionRef, error) {
	return nil, nil
}
func (*preparedStore) DeleteUnreferencedPreparedRevision(context.Context, string) error { return nil }

func TestAgentTemplateControllerKeepsLastSuccessUntilActorTemplateReady(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, v1alpha3.AddToScheme, atev1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "helper", Namespace: "agents", UID: "template-uid", Labels: map[string]string{"team": "agents"}},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: v1alpha3.AgentTemplateLocalReference{Name: "model"}, SystemPrompt: "help",
			Harnesses: v1alpha3.HarnessAttachments{Include: []v1alpha3.AgentTemplateLocalReference{{Name: "kagent"}}},
		},
	}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "agents", UID: "harness-uid"},
		Spec: v1alpha3.HarnessSpec{
			Kagent:   &v1alpha3.KagentHarness{},
			Workload: v1alpha3.HarnessWorkload{Image: "agent.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Substrate: v1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef:  corev1.LocalObjectReference{Name: "default"},
				SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
			},
			AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{MatchLabels: map[string]string{"team": "agents"}}},
		},
	}
	model := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "agents"}, Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5"}}
	workerPool := &atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "agents"}}
	kube := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&v1alpha3.AgentTemplate{}, &atev1alpha1.ActorTemplate{}).
		WithObjects(template, harness, model, workerPool).Build()
	translator := agenttranslator.NewAdkApiTranslator(kube, types.NamespacedName{Name: model.Name, Namespace: model.Namespace}, nil, "", nil)
	lifecycle := &substrate.Lifecycle{Client: kube, Defaults: substrate.LifecycleDefaults{PauseImage: "pause.example/image@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	store := &preparedStore{}
	controller := &AgentTemplateController{Client: kube, Translator: translator, Lifecycle: lifecycle, Store: store}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(template)}
	result, err := controller.Reconcile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter == 0 || store.attachment.DesiredRevision == "" {
		t.Fatalf("pending reconcile = %+v, attachment = %+v", result, store.attachment)
	}
	current := &v1alpha3.AgentTemplate{}
	if err := kube.Get(context.Background(), request.NamespacedName, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Preparations[0].LatestSuccessfulRevision != "" {
		t.Fatal("pending revision replaced latest successful revision")
	}
	backing := &atev1alpha1.ActorTemplate{}
	if err := kube.Get(context.Background(), types.NamespacedName{Namespace: "agents", Name: store.revision.BackingResource.Name}, backing); err != nil {
		t.Fatal(err)
	}
	backing.Status.Phase = atev1alpha1.PhaseReady
	backing.Status.GoldenSnapshot = "golden"
	if err := kube.Status().Update(context.Background(), backing); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := kube.Get(context.Background(), request.NamespacedName, current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Preparations[0].LatestSuccessfulRevision != store.attachment.DesiredRevision {
		t.Fatalf("preparation status = %+v", current.Status.Preparations[0])
	}
}
