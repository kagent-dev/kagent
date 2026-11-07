package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestBuiltinHarnessDefaultImages(t *testing.T) {
	interactionTarget(t)
	forEachHarness(t, func(t *testing.T, runtime testHarness) {
		if runtime.runtimeLabel == "byo-adk" {
			t.Skip("BYO requires an explicit image; default images apply only to built-in runtimes")
		}
		kube := interactionKubeClient(t)
		source := &v1alpha3.Harness{}
		require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: runtime.name}, source))
		require.NotNil(t, source.Spec.Workload.Image, "the override fixture identifies the image built for this test")
		model := runtime.createModel(t, kube, startInteractionMock(t), nil)
		harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{GenerateName: "default-image-", Namespace: source.Namespace}, Spec: *source.Spec.DeepCopy()}
		harness.Spec.Workload.Image = nil
		agent := &v1alpha3.Agent{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "default-image-", Namespace: source.Namespace},
			Spec:       v1alpha3.AgentSpec{Template: v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, SystemPrompt: "Be helpful."}}},
		}
		require.NoError(t, kube.Create(t.Context(), harness))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), harness)) })
		agent.Spec.HarnessRef = corev1.LocalObjectReference{Name: harness.Name}
		require.NoError(t, kube.Create(t.Context(), agent))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), agent)) })
		waitForAgentRevision(t, kube, agent)
		require.Equal(t, source.Spec.Workload.Image, agent.Status.WorkloadImage)
		require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(harness), harness))
		require.Nil(t, harness.Spec.Workload.Image)
	})
}

// The default ModelConfig and Harnesses come from `kagent install`, which the
// suite uses to create the cluster.
const installedDefaultModelConfig = "default-model-config"

func TestInstalledDefaultHarnesses(t *testing.T) {
	interactionTarget(t)
	scenarios := []struct {
		name    string
		harness string
		// model returns the ModelConfig the Agent references.
		model   func(*testing.T, ctrlclient.Client) string
		hasKind func(v1alpha3.HarnessSpec) bool
	}{
		{
			name:    "Given the installed kagent-default Harness, when an Agent references it with the installed default model, assert that the Agent becomes ready",
			harness: "kagent-default",
			model:   func(*testing.T, ctrlclient.Client) string { return installedDefaultModelConfig },
			hasKind: func(spec v1alpha3.HarnessSpec) bool { return spec.Kagent != nil },
		},
		{
			name:    "Given the installed codex-default Harness, when an Agent references it with the installed default OpenAI model, assert that the Agent becomes ready",
			harness: "codex-default",
			model: func(t *testing.T, kube ctrlclient.Client) string {
				model := &v1alpha3.ModelConfig{}
				require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: installedDefaultModelConfig}, model))
				if model.Spec.Provider != v1alpha3.ModelProviderOpenAI {
					t.Skipf("the installed default model uses %s; Codex accepts only OpenAI", model.Spec.Provider)
				}
				return model.Name
			},
			hasKind: func(spec v1alpha3.HarnessSpec) bool { return spec.Codex != nil },
		},
		{
			name:    "Given the installed claude-default Harness, when an Agent references it with an Anthropic model, assert that the Agent becomes ready",
			harness: "claude-default",
			model: func(t *testing.T, kube ctrlclient.Client) string {
				return testHarness{name: claudeE2EHarness}.createModel(t, kube, startInteractionMock(t), nil).Name
			},
			hasKind: func(spec v1alpha3.HarnessSpec) bool { return spec.Claude != nil },
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			kube := interactionKubeClient(t)
			harness := &v1alpha3.Harness{}
			require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: scenario.harness}, harness))
			require.True(t, scenario.hasKind(harness.Spec), "unexpected runtime in %+v", harness.Spec)
			require.Nil(t, harness.Spec.Workload.Image, "default Harnesses use the release's built-in image")
			require.Equal(t, "kagent-default", harness.Spec.Substrate.WorkerPoolRef.Name)
			require.Equal(t, "s3://ate-snapshots/kagent/", harness.Spec.Substrate.SnapshotPolicy.Location)

			agent := &v1alpha3.Agent{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "default-harness-", Namespace: harness.Namespace},
				Spec: v1alpha3.AgentSpec{
					HarnessRef: corev1.LocalObjectReference{Name: harness.Name},
					Template:   v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: scenario.model(t, kube)}, SystemPrompt: "Be helpful."}},
				},
			}
			require.NoError(t, kube.Create(t.Context(), agent))
			t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), agent)) })
			waitForAgentRevision(t, kube, agent)
		})
	}
}

// waitForAgentRevision waits until the controller has prepared the Agent's
// current generation, leaving the latest status in agent.
func waitForAgentRevision(t *testing.T, kube ctrlclient.Client, agent *v1alpha3.Agent) {
	t.Helper()
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
			return false, err
		}
		condition := meta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionReady)
		return condition != nil && condition.ObservedGeneration == agent.Generation && condition.Status == metav1.ConditionTrue, nil
	})
	require.NoError(t, err, "last conditions: %+v", agent.Status.Conditions)
	require.NotEmpty(t, agent.Status.DesiredRevision)
	require.Equal(t, agent.Status.DesiredRevision, agent.Status.LatestSuccessfulRevision)
}
