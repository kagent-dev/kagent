package e2e_test

import (
	"context"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAgentInlineAndReferencedConfiguration(t *testing.T) {
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		for _, inlineTemplate := range []bool{false, true} {
			for _, inlineHarness := range []bool{false, true} {
				name := "template-ref"
				if inlineTemplate {
					name = "template-inline"
				}
				name += "/harness-ref"
				if inlineHarness {
					name = name[:len(name)-3] + "inline"
				}
				t.Run(name, func(t *testing.T) {
					target := interactionTarget(t)
					kube := interactionKubeClient(t)
					agentName := createInteractionTemplate(t, harness, startInteractionMock(t))
					key := ctrlclient.ObjectKey{Namespace: "kagent", Name: agentName}
					agent := &v1alpha3.Agent{}
					require.NoError(t, kube.Get(t.Context(), key, agent))
					if inlineTemplate {
						template := &v1alpha3.AgentTemplate{}
						require.NoError(t, kube.Get(t.Context(), key, template))
						agent.Spec.Template = template.Spec.DeepCopy()
						agent.Spec.TemplateRef = nil
					}
					if inlineHarness {
						runtime := &v1alpha3.Harness{}
						require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: harness.name}, runtime))
						agent.Spec.Harness = runtime.Spec.DeepCopy()
						agent.Spec.HarnessRef = nil
					}
					require.NoError(t, kube.Update(t.Context(), agent))
					generation := agent.Generation
					require.NoError(t, wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
						if err := kube.Get(ctx, key, agent); err != nil {
							return false, err
						}
						for _, condition := range agent.Status.Conditions {
							if condition.Type == v1alpha3.AgentConditionReady && condition.ObservedGeneration == generation {
								return condition.Status == metav1.ConditionTrue, nil
							}
						}
						return false, nil
					}))
					fixture := newInteractionFixtureForTemplate(t, harness, target, agentName)
					_, _, task := fixture.send(t, "What is 2+2?")
					require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
					require.Contains(t, taskText(task), "The answer is 4.")
				})
			}
		}
	})
}
