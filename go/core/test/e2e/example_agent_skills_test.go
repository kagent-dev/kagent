package e2e_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

// exampleAgentSkills are the kagent-tools skills that the chart's cluster-ops
// example Agent (helm/kagent/templates/example-agents.yaml) selects.
var exampleAgentSkills = []string{
	"k8s-troubleshooting",
	"helm-release-management",
	"argo-rollouts-progressive-delivery",
	"prometheus-observability",
}

// exampleAgentOtherSkills are in the same image but belong to the other
// example Agents.
var exampleAgentOtherSkills = []string{
	"istio-traffic-debugging",
	"cilium-network-debugging",
	"kubescape-security-posture",
}

// TestExampleAgentToolsSkills runs the chart's cluster-ops example Agent
// behavior on every harness with a mock model: the kagent-tools image as an
// Agent Plugin, four of its skills, and the built-in prompt library. The chart's tool server
// binding is left out because the E2E install does not run kagent-tools.
func TestExampleAgentToolsSkills(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	image := kagentenv.E2EToolsSkillsImage.Get()
	if image == "" {
		t.Skip("KAGENT_E2E_TOOLS_SKILLS_IMAGE is not set; it needs a kagent-tools image with the opt/kagent-tools-skills folder")
	}
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		model := startExampleAgentSkillsMock(t, harness.skillsRoot())
		kube := interactionKubeClient(t)
		config := harness.createModel(t, kube, reachableModelURL(t, model.URL), nil)
		template := &v1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "example-skills-", Namespace: "kagent", Labels: harness.labels()},
			Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig: &corev1.LocalObjectReference{Name: config.Name},
				Description: "Kubernetes workload, Helm release, Argo Rollouts, and Prometheus metrics assistant.",
				PromptTemplate: &v1alpha3.AgentTemplatePromptTemplateSpec{
					DataSources: []v1alpha3.AgentTemplatePromptSource{{Name: "kagent-builtin-prompts", Alias: "builtin"}},
				},
				SystemPrompt: "Answer questions about the Kubernetes cluster where kagent runs. When a\n" +
					"question matches one of your skills, load that skill and follow it. Each\n" +
					"skill names the kagent-tools MCP tools it uses.\n\n" +
					"{{include \"builtin/skills-usage\"}}\n\n" +
					"{{include \"builtin/kubernetes-context\"}}\n\n" +
					"{{include \"builtin/tool-usage-best-practices\"}}\n\n" +
					"{{include \"builtin/safety-guardrails\"}}\n",
				Plugins: []v1alpha3.PluginBundle{{
					Source: v1alpha3.ArtifactSource{OCI: image, Path: "opt/kagent-tools-skills"},
					Skills: exampleAgentSkills,
				}},
			},
		}
		// Ready requires the runtime to mount the image and install the skills.
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		_, _, task := fixture.send(t, "Which Helm releases are installed?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
		require.Contains(t, taskText(task), "EXAMPLE_SKILL_LOADED")

		// Claude also sends requests without a body; only model turns count.
		requests := slices.DeleteFunc(model.Requests("", ""), func(r recordedModelRequest) bool { return len(r.Body) == 0 })
		require.GreaterOrEqual(t, len(requests), 2, "the model must be called again after loading the skill")
		first := string(requests[0].Body)
		for _, skill := range exampleAgentSkills {
			require.Contains(t, first, skill, "skill must be advertised to the model")
		}
		for _, skill := range exampleAgentOtherSkills {
			require.NotContains(t, first, skill, "unselected skill must not be advertised")
		}
		require.Contains(t, first, "## Safety Guidelines", "built-in prompts must be included")
		// Discovery exposes metadata only. The skill body names its MCP tools.
		require.NotContains(t, first, "helm_list_releases")
		loaded := false
		for _, request := range requests[1:] {
			loaded = loaded || strings.Contains(string(request.Body), "helm_list_releases")
		}
		require.True(t, loaded, "the model did not receive the helm-release-management skill body")
	})
}

func startExampleAgentSkillsMock(t *testing.T, root string) *modelRecorder {
	t.Helper()
	fixture, err := interactionMocks.ReadFile("mocks/invoke_example_agent_skills.json")
	require.NoError(t, err)
	var config mockllm.Config
	require.NoError(t, json.Unmarshal([]byte(strings.ReplaceAll(string(fixture), "ROOT", root)), &config))
	return startModelRecorder(t, mockLLMProxy(t, startMockLLMConfig(t, config)), nil)
}
