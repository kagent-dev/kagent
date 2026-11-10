package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestHarnessResources(t *testing.T) {
	interactionTarget(t)
	t.Run("Given an Agent ready on a Harness with a CPU limit, when the Harness drops the limit, assert that the Agent becomes ready on a new revision", func(t *testing.T) {
		kube := interactionKubeClient(t)
		source := &v1alpha3.Harness{}
		require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "kagent"}, source))
		model := createInteractionModel(t, kube, startInteractionMock(t), nil)
		harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{GenerateName: "harness-resources-", Namespace: source.Namespace}, Spec: *source.Spec.DeepCopy()}
		harness.Spec.Resources = &v1alpha3.HarnessResources{CPU: new(resource.MustParse("500m"))}
		require.NoError(t, kube.Create(t.Context(), harness))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), harness)) })
		agent := &v1alpha3.Agent{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "harness-resources-", Namespace: source.Namespace},
			Spec: v1alpha3.AgentSpec{
				HarnessRef: corev1.LocalObjectReference{Name: harness.Name},
				Template:   v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, SystemPrompt: "Be helpful."}},
			},
		}
		require.NoError(t, kube.Create(t.Context(), agent))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), agent)) })
		waitForAgentRevision(t, kube, agent)
		sized := agent.Status.LatestSuccessfulRevision

		require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(harness), harness))
		harness.Spec.Resources = nil
		require.NoError(t, kube.Update(t.Context(), harness))

		// The Agent's generation is unchanged, so readiness alone could still
		// describe the CPU-limited revision.
		err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
				return false, err
			}
			return meta.IsStatusConditionTrue(agent.Status.Conditions, v1alpha3.AgentConditionReady) &&
				agent.Status.DesiredRevision != sized &&
				agent.Status.LatestSuccessfulRevision == agent.Status.DesiredRevision, nil
		})
		require.NoError(t, err, "last status: %+v", agent.Status)
	})
}
