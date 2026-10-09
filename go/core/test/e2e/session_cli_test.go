package e2e_test

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed mocks/invoke_cli_versions.json
var sessionCLIMocks embed.FS

// A coding agent works in its clone with git and the GitHub CLI, so both run
// through the model's shell tool in a Session's sandbox on every Harness that
// gives the model one. The kagent Harness registers its bash tool only with a
// skill, so the template carries the git fixture's skill.
func TestSessionShellRunsGitAndGitHubCLI(t *testing.T) {
	t.Parallel()
	for _, harness := range []struct {
		testHarness
		shellTool string
	}{
		{testHarness{name: "kagent", runtimeLabel: "kagent"}, "bash"},
		{testHarness{name: claudeE2EHarness, runtimeLabel: "claude"}, "Bash"},
	} {
		t.Run(harness.runtimeLabel, func(t *testing.T) {
			t.Parallel()
			target := interactionTarget(t)
			kube := interactionKubeClient(t)
			credential := base64.StdEncoding.EncodeToString([]byte("x-access-token:cli-e2e-token"))
			secret := createCredentialSecret(t, kube, credential)
			repositories := newGitFixture(t, "Basic "+credential)
			server := repositories.serveThroughEgress(t)
			modelURL := reachableModelURL(t, startMockLLMServer(t, sessionCLIMocks, "mocks/invoke_cli_versions.json"))
			model := harness.createModel(t, kube, modelURL, nil)
			template := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "session-cli-", Namespace: "kagent", Labels: harness.labels()},
				Spec: v1alpha3.AgentTemplateSpec{
					ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
					SystemPrompt: "Run the commands you are asked to run.",
					Skills: []v1alpha3.AgentTemplateSkill{{Name: gitFixtureSkill, Source: v1alpha3.ArtifactSource{
						Git: &v1alpha3.GitArtifact{
							URL: server + "/" + gitSkillRepository, Commit: repositories.commit,
							CredentialRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"},
						},
						Path: "skills/" + gitFixtureSkill,
					}}},
				},
			}
			createAndWaitInteractionTemplate(t, harness.testHarness, kube, template)
			fixture := newInteractionFixtureForTemplate(t, harness.testHarness, target, template.Name)

			streamed := sendStreaming(t, fixture, "Print the CLI versions.")
			require.Equal(t, a2atype.TaskStateCompleted, streamed.state, "task text: %q", streamed.text)
			require.Contains(t, streamed.text, "CLI_VERSIONS_DONE")
			output := toolResponseText(t, getTask(t, fixture, streamed.taskID), harness.shellTool)
			require.Contains(t, output, "gh version")
			require.Contains(t, output, "git version")
		})
	}
}

// toolResponseText returns the serialized function_response of the task's one
// call of toolName, which carries the command's output.
func toolResponseText(t *testing.T, task *a2atype.Task, toolName string) string {
	t.Helper()
	var responses []string
	for _, part := range taskParts(task) {
		if partType, _ := part.Metadata[apia2a.PartTypeMetadataKey].(string); partType != "function_response" {
			continue
		}
		data, ok := part.Data().(map[string]any)
		if !ok || data["name"] != toolName {
			continue
		}
		encoded, err := json.Marshal(data)
		require.NoError(t, err)
		responses = append(responses, string(encoded))
	}
	require.Len(t, responses, 1, "function_response parts of %s in task %s", toolName, task.ID)
	return responses[0]
}
