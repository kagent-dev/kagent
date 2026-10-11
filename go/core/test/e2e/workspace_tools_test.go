package e2e_test

import (
	_ "embed"
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed mocks/invoke_workspace_tools.json
var workspaceToolsMocks []byte

// newWorkspaceAgentWithoutSkills is a kagent Harness Agent with no skills,
// whose model reports a repository of the session directory with the bash
// tool, the probe of invoke_workspace_tools.json.
func newWorkspaceAgentWithoutSkills(t *testing.T) *workspaceAgent {
	t.Helper()
	kube := interactionKubeClient(t)
	var cfg mockllm.Config
	require.NoError(t, json.Unmarshal(workspaceToolsMocks, &cfg))
	model := startModelRecorder(t, startMockLLMConfig(t, cfg), nil)
	modelConfig := createInteractionModel(t, kube, reachableModelURL(t, model.URL), nil)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-tools-", Namespace: "kagent", Labels: goADKHarness.labels()},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: modelConfig.Name},
			SystemPrompt: "Work in the workspace with the bash tool.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, goADKHarness.name)
	return &workspaceAgent{template: template.Name, model: model}
}

// modelTools returns the tools the last of requests offered the model, their
// descriptions by name.
func modelTools(t *testing.T, requests []recordedModelRequest) map[string]string {
	t.Helper()
	require.NotEmpty(t, requests, "no model request")
	var body struct {
		Tools []struct {
			Function struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"function"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(requests[len(requests)-1].Body, &body))
	tools := map[string]string{}
	for _, tool := range body.Tools {
		tools[tool.Function.Name] = tool.Function.Description
	}
	return tools
}

// An agent without skills has bash and the file tools, told where they work:
// in a Session on a workspace, bash runs git status in a repository of the
// session directory and reports it; no skills tool is offered to the model.
func TestSessionWorkspaceToolsWithoutSkills(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	fixture := newWorkspaceFixture(t)
	agent := newWorkspaceAgentWithoutSkills(t)
	session := agent.session(t, target, fixture)

	_, _, task := session.send(t, "Report the repository.")
	agent.requireReply(t, session, task, "REPOSITORY_REPORTED")
	results := agent.toolResults(t, session.sessionID)
	require.Contains(t, results, "toplevel=/workspace/e2e/alpha", "git status ran in the repository of the session directory")
	require.Contains(t, results, "branch=## main", "git status reported the repository")

	tools := modelTools(t, agent.model.Requests(adk.AgentInstanceHeader, session.sessionID))
	require.ElementsMatch(t, []string{"bash", "read_file", "write_file", "edit_file", "ask_user"}, slices.Collect(maps.Keys(tools)), "the tools offered to the model")
	for _, name := range []string{"bash", "read_file", "write_file", "edit_file"} {
		require.Contains(t, tools[name], "<owner>/<repository>", "%s names the working directory rule", name)
		require.Contains(t, tools[name], "/data/workspace", "%s names the directory of a session without a workspace", name)
		require.NotContains(t, tools[name], "skills/", "%s names no skills", name)
	}
}
