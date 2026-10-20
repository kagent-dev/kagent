package e2e_test

import (
	"encoding/json"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestSessionAskUserDisabled(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		if harness.name != "kagent" {
			t.Skip("askUser: false is supported only by the kagent harness")
		}
		target := interactionTarget(t)
		kube := interactionKubeClient(t)
		recorder := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json")), nil)
		model := harness.createModel(t, kube, reachableModelURL(t, recorder.URL), map[string]string{"X-Kagent-E2E-Model": "ask-user-disabled"})
		template := &v1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "ask-user-disabled-", Namespace: "kagent", Labels: harness.labels()},
			Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "Reply briefly.", AskUser: new(false),
			},
		}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
		require.Contains(t, taskText(task), "The answer is 4.")
		requests := recorder.Requests("X-Kagent-E2E-Model", "ask-user-disabled")
		require.NotEmpty(t, requests, "the agent must reach the model")
		for _, recorded := range requests {
			var request struct {
				Tools []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tools"`
			}
			require.NoError(t, json.Unmarshal(recorded.Body, &request))
			var names []string
			for _, tool := range request.Tools {
				names = append(names, tool.Function.Name)
			}
			require.NotContains(t, names, "ask_user")
			require.Contains(t, names, "read_file", "other built-in tools remain available")
		}
	})
}
