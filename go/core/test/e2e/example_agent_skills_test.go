package e2e_test

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

const exampleAgentsTemplate = "../../../../helm/kagent/templates/example-agents.yaml"

// exampleAgentLoads names, for each example Agent in the chart, a question the
// mock model answers by loading one of the Agent's skills, and a tool that only
// that skill's body names.
var exampleAgentLoads = map[string]struct{ question, skill, tool string }{
	"cluster-ops": {"Which Helm releases are installed?", "helm-release-management", "helm_list_releases"},
	"networking":  {"Why do requests fail inside the Istio mesh?", "istio-traffic-debugging", "istio_proxy_status"},
	"security":    {"Is Kubescape healthy in this cluster?", "kubescape-security-posture", "kubescape_check_health"},
}

type exampleAgent struct {
	name, description string
	skills            []string
}

var (
	exampleAgentNamePattern        = regexp.MustCompile(`(?m)^  name: \{\{ printf "%s-([a-z-]+)" \$prefix`)
	exampleAgentDescriptionPattern = regexp.MustCompile(`(?m)^      description: "([^"]+)"$`)
	exampleAgentSkillsPattern      = regexp.MustCompile(`(?m)^          skills:\n((?:            - \S+(?:\n|\z))+)`)
)

// readExampleAgents reads each example Agent's name, description and skills
// from the chart template, so the test follows the chart.
func readExampleAgents(t *testing.T) []exampleAgent {
	t.Helper()
	data, err := os.ReadFile(exampleAgentsTemplate)
	require.NoError(t, err)
	var agents []exampleAgent
	for doc := range strings.SplitSeq(string(data), "\n---\n") {
		if !strings.Contains(doc, "\nkind: Agent\n") {
			continue
		}
		name := exampleAgentNamePattern.FindStringSubmatch(doc)
		description := exampleAgentDescriptionPattern.FindStringSubmatch(doc)
		skills := exampleAgentSkillsPattern.FindStringSubmatch(doc)
		require.True(t, name != nil && description != nil && skills != nil, "cannot read an example Agent in %s:\n%s", exampleAgentsTemplate, doc)
		agent := exampleAgent{name: name[1], description: description[1]}
		for line := range strings.SplitSeq(strings.TrimSpace(skills[1]), "\n") {
			agent.skills = append(agent.skills, strings.TrimPrefix(strings.TrimSpace(line), "- "))
		}
		agents = append(agents, agent)
	}
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agent.name)
	}
	want := make([]string, 0, len(exampleAgentLoads))
	for name := range exampleAgentLoads {
		want = append(want, name)
	}
	require.ElementsMatch(t, want, names, "every example Agent in %s needs an entry in exampleAgentLoads", exampleAgentsTemplate)
	return agents
}

// TestExampleAgentToolsSkills runs each of the chart's example Agent shapes on
// every harness with a mock model: the kagent-tools image as an Agent Plugin,
// the Agent's own skills, and the built-in prompt library. The chart's tool
// server binding is left out because the E2E install does not run kagent-tools.
func TestExampleAgentToolsSkills(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	image := kagentenv.E2EToolsSkillsImage.Get()
	if image == "" {
		t.Skip("KAGENT_E2E_TOOLS_SKILLS_IMAGE is not set; it needs a kagent-tools image with the opt/kagent-tools-skills folder")
	}
	agents := readExampleAgents(t)
	for _, agent := range agents {
		load := exampleAgentLoads[agent.name]
		require.Contains(t, agent.skills, load.skill, "%s must select the skill it loads", agent.name)
		var others []string
		for _, other := range agents {
			if other.name != agent.name {
				others = append(others, other.skills...)
			}
		}
		t.Run(agent.name, func(t *testing.T) {
			t.Parallel()
			forEachHarness(t, func(t *testing.T, harness testHarness) {
				t.Parallel()
				model := startExampleAgentSkillsMock(t, harness.skillsRoot(), load.question, load.skill, load.tool)
				kube := interactionKubeClient(t)
				config := harness.createModel(t, kube, reachableModelURL(t, model.URL), nil)
				template := &v1alpha3.AgentTemplate{
					ObjectMeta: metav1.ObjectMeta{GenerateName: "example-" + agent.name + "-", Namespace: "kagent", Labels: harness.labels()},
					Spec: v1alpha3.AgentTemplateSpec{
						ModelConfig: &v1alpha3.ObjectReference{Name: config.Name},
						Description: agent.description,
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
							Skills: agent.skills,
						}},
					},
				}
				// Ready requires the runtime to mount the image and install the skills.
				createAndWaitInteractionTemplate(t, harness, kube, template)
				fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
				_, _, task := fixture.send(t, load.question)
				require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
				require.Contains(t, taskText(task), "EXAMPLE_SKILL_LOADED")

				// Claude also sends requests without a body; only model turns count.
				requests := slices.DeleteFunc(model.Requests("", ""), func(r recordedModelRequest) bool { return len(r.Body) == 0 })
				require.GreaterOrEqual(t, len(requests), 2, "the model must be called again after loading the skill")
				first := string(requests[0].Body)
				for _, skill := range agent.skills {
					require.Contains(t, first, skill, "skill must be advertised to the model")
				}
				for _, skill := range others {
					require.NotContains(t, first, skill, "another example Agent's skill must not be advertised")
				}
				require.Contains(t, first, "## Safety Guidelines", "built-in prompts must be included")
				// Discovery exposes metadata only. The skill body names its MCP tools.
				require.NotContains(t, first, load.tool)
				loaded := false
				for _, request := range requests[1:] {
					loaded = loaded || strings.Contains(string(request.Body), load.tool)
				}
				require.True(t, loaded, "the model did not receive the %s skill body", load.skill)
			})
		})
	}
}

func startExampleAgentSkillsMock(t *testing.T, root, question, skill, tool string) *modelRecorder {
	t.Helper()
	fixture, err := interactionMocks.ReadFile("mocks/invoke_example_agent_skills.json")
	require.NoError(t, err)
	replacer := strings.NewReplacer("ROOT", root, "QUESTION", question, "SKILL_NAME", skill, "SKILL_TOOL", tool)
	var config mockllm.Config
	require.NoError(t, json.Unmarshal([]byte(replacer.Replace(string(fixture))), &config))
	return startModelRecorder(t, mockLLMProxy(t, startMockLLMConfig(t, config)), nil)
}
