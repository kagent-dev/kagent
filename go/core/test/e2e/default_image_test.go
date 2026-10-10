package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
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
		for _, inline := range []bool{false, true} {
			t.Run(fmt.Sprintf("inline=%t", inline), func(t *testing.T) {
				harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{GenerateName: "default-image-", Namespace: source.Namespace}, Spec: *source.Spec.DeepCopy()}
				harness.Spec.Workload.Image = nil
				agent := &v1alpha3.Agent{
					ObjectMeta: metav1.ObjectMeta{GenerateName: "default-image-", Namespace: source.Namespace},
					Spec:       v1alpha3.AgentSpec{Template: &v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "Be helpful."}},
				}
				if inline {
					agent.Spec.Harness = &harness.Spec
				} else {
					require.NoError(t, kube.Create(t.Context(), harness))
					t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), harness)) })
					agent.Spec.HarnessRef = &corev1.LocalObjectReference{Name: harness.Name}
				}
				require.NoError(t, kube.Create(t.Context(), agent))
				t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), agent)) })
				err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
					if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
						return false, err
					}
					condition := meta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionReady)
					return condition != nil && condition.ObservedGeneration == agent.Generation && condition.Status == metav1.ConditionTrue, nil
				})
				require.NoError(t, err, "last conditions: %+v", agent.Status.Conditions)
				require.Equal(t, source.Spec.Workload.Image, agent.Status.WorkloadImage)
				require.NotEmpty(t, agent.Status.DesiredRevision)
				require.Equal(t, agent.Status.DesiredRevision, agent.Status.LatestSuccessfulRevision)
				if inline {
					require.Nil(t, agent.Spec.Harness.Workload.Image)
				} else {
					require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(harness), harness))
					require.Nil(t, harness.Spec.Workload.Image)
				}
			})
		}
	})
}
