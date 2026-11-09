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

func TestE2EUnsupportedReferenceKinds(t *testing.T) {
	interactionTarget(t)
	kube := interactionKubeClient(t)

	t.Run("Given a model reference to a kind kagent does not resolve, when an Agent is created, assert that ResolvedRefs reports InvalidKind", func(t *testing.T) {
		agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{GenerateName: "unsupported-kinds-", Namespace: "kagent"}, Spec: v1alpha3.AgentSpec{
			HarnessRef: corev1.LocalObjectReference{Name: "kagent"},
			Template: v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{
				ModelConfig:  &v1alpha3.ModelConfigReference{Group: new("example.com"), Kind: new("Model"), Name: "default-model-config"},
				SystemPrompt: "Reply briefly.",
			}},
		}}
		require.NoError(t, kube.Create(t.Context(), agent))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), agent)) })

		// Wait for ResolvedRefs to describe this generation, then assert its outcome.
		var condition *metav1.Condition
		err := wait.PollUntilContextTimeout(t.Context(), time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
			if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
				return false, err
			}
			condition = meta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionResolvedRefs)
			return condition != nil && condition.ObservedGeneration == agent.Generation && condition.Status != metav1.ConditionUnknown, nil
		})
		require.NoError(t, err, "last conditions: %+v", agent.Status.Conditions)
		require.Equal(t, metav1.ConditionFalse, condition.Status, condition.Message)
		require.Equal(t, "InvalidKind", condition.Reason, condition.Message)
		require.Empty(t, agent.Status.DesiredRevision)
	})
}
