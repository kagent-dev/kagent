package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
)

const codexSharedInstruction = "CODEX_SHARED_SPECIALIST: answer as the shared specialist."

// Validate the dynamic fixture against the pinned native CLI without requiring
// a cluster. The public Session path is exercised by TestSharedAgentInteraction.
func TestCodexSharedInteractionMock(t *testing.T) {
	executable, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("pinned Codex CLI is not installed")
	}
	version, err := exec.Command(executable, "--version").Output()
	require.NoError(t, err)
	if !strings.Contains(string(version), " "+codexconfig.PinnedCodexVersion) {
		t.Skip("installed Codex CLI differs from the pinned version")
	}
	model := startCodexSharedInteractionMock(t)
	directory := t.TempDir()
	home := filepath.Join(directory, "codex")
	require.NoError(t, os.MkdirAll(filepath.Join(home, "agents"), 0o700))
	role := filepath.Join(home, "agents", "specialist.toml")
	require.NoError(t, os.WriteFile(role, []byte("model = \"gpt-5.2-codex\"\ndeveloper_instructions = "+strconv.Quote(codexSharedInstruction)+"\n"), 0o600))
	baseURL := strings.NewReplacer("0.0.0.0", "127.0.0.1", "[::]", "127.0.0.1").Replace(model.URL) + "/v1"
	configuration := fmt.Sprintf(`model = "gpt-5.2-codex"
model_provider = "kagent-openai"
approval_policy = "never"
sandbox_mode = "danger-full-access"
[model_providers.kagent-openai]
name = "OpenAI"
wire_api = "responses"
env_key = "OPENAI_API_KEY"
base_url = %s
request_max_retries = 0
stream_max_retries = 0
[agents.specialist]
description = "Specialist"
config_file = %s
`, strconv.Quote(baseURL), strconv.Quote(role))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(configuration), 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "exec", "--skip-git-repo-check", "--json", "Ask the specialist")
	command.Dir = directory
	command.Env = append(os.Environ(), "CODEX_HOME="+home, "HOME="+directory, "OPENAI_API_KEY=mock-key", "NO_PROXY=*")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Logf("native model requests received: %d", len(model.Requests("", "")))
	}
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), `"tool":"wait"`)
	require.Contains(t, string(output), "Answer from the shared specialist.")
	var sawChild bool
	for _, recorded := range model.Requests("", "") {
		var request codexSharedRequest
		require.NoError(t, json.Unmarshal(recorded.Body, &request))
		sawChild = sawChild || strings.Contains(request.developerInstructions(), codexSharedInstruction)
	}
	require.True(t, sawChild, "fixture must answer a real child request")
}

func TestE2ECodexSharedSkills(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	image := publishSkillImage(t)
	model := startCodexSharedInteractionMock(t)
	root, _ := createCodexSharedTemplates(t, reachableModelURL(t, model.URL),
		[]v1alpha3.AgentTemplateSkill{{Name: "selected", Source: v1alpha3.ArtifactSource{OCI: image, Path: "plugin/skills/selected"}}},
		[]v1alpha3.AgentTemplateSkill{{Name: "standalone", Source: v1alpha3.ArtifactSource{OCI: image, Path: "standalone"}}})
	fixture := newInteractionFixtureForHarnessTemplate(t, target, codexE2EHarness, root)
	_, _, task := fixture.send(t, "Ask the specialist")
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
	require.Contains(t, taskText(task), "Answer from the shared specialist.")
	var sawRoot, sawChild bool
	for _, recorded := range model.Requests("", "") {
		var request codexSharedRequest
		require.NoError(t, json.Unmarshal(recorded.Body, &request))
		text := string(recorded.Body)
		if strings.Contains(request.developerInstructions(), codexSharedInstruction) {
			sawChild = true
			require.Contains(t, text, "Execute the standalone mounted skill.")
			require.NotContains(t, text, "Execute the selected mounted skill.")
		} else {
			sawRoot = true
			require.Contains(t, text, "Execute the standalone mounted skill.")
			require.Contains(t, text, "Execute the selected mounted skill.")
		}
	}
	require.True(t, sawRoot)
	require.True(t, sawChild, "the child must run with its own developer instructions")
}

func createCodexSharedTemplates(t *testing.T, modelURL string, rootSkills, childSkills []v1alpha3.AgentTemplateSkill) (string, string) {
	t.Helper()
	kube := interactionKubeClient(t)
	model := createCodexMockModel(t, kube, modelURL)
	child := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "codex-shared-child-", Namespace: "kagent"},
		Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, Description: "Shared specialist", SystemPrompt: codexSharedInstruction, Skills: childSkills},
	}
	createSharedTemplate(t, kube, child)
	root := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "codex-shared-root-", Namespace: "kagent", Labels: (testHarness{name: codexE2EHarness, runtimeLabel: "codex"}).labels()},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, SystemPrompt: "Delegate every request to the specialist.", Skills: rootSkills,
			Tools: []v1alpha3.ToolBinding{{SubAgent: &v1alpha3.SubAgentToolBinding{Name: "specialist", Description: "Handles specialist requests", TemplateRef: &corev1.LocalObjectReference{Name: child.Name}}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, root, codexE2EHarness)
	return root.Name, child.Name
}

type codexSharedRequest struct {
	Instructions string `json:"instructions"`
	Input        []struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		CallID  string          `json:"call_id"`
		Output  string          `json:"output"`
	} `json:"input"`
}

func (r codexSharedRequest) developerInstructions() string {
	result := r.Instructions
	for _, item := range r.Input {
		if item.Role == "developer" {
			result += string(item.Content)
		}
	}
	return result
}

func startCodexSharedInteractionMock(t *testing.T) *modelRecorder {
	t.Helper()
	return startModelRecorder(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request codexSharedRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var output json.RawMessage
		if strings.Contains(request.developerInstructions(), codexSharedInstruction) {
			output = json.RawMessage(`{"id":"msg_child","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Answer from the shared specialist.","annotations":[]}]}`)
		} else {
			var agentID string
			var completed bool
			for _, item := range request.Input {
				if item.Type != "function_call_output" {
					continue
				}
				if item.CallID == "call_spawn_shared" {
					var spawned struct {
						AgentID string `json:"agent_id"`
					}
					if err := json.Unmarshal([]byte(item.Output), &spawned); err != nil {
						http.Error(w, "spawn_agent did not return an agent ID: "+item.Output, http.StatusBadRequest)
						return
					}
					agentID = spawned.AgentID
				}
				completed = completed || item.CallID == "call_wait_shared" && strings.Contains(item.Output, "Answer from the shared specialist.")
			}
			switch {
			case completed:
				output = json.RawMessage(`{"id":"msg_root","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Answer from the shared specialist.","annotations":[]}]}`)
			case agentID != "":
				arguments, err := json.Marshal(struct {
					Targets []string `json:"targets"`
					Timeout int      `json:"timeout_ms"`
				}{[]string{agentID}, 10000})
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				encoded, err := json.Marshal(string(arguments))
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				output = json.RawMessage(fmt.Sprintf(`{"id":"fc_wait","type":"function_call","status":"completed","call_id":"call_wait_shared","namespace":"multi_agent_v1","name":"wait_agent","arguments":%s}`, encoded))
			default:
				output = json.RawMessage(`{"id":"fc_spawn","type":"function_call","status":"completed","call_id":"call_spawn_shared","namespace":"multi_agent_v1","name":"spawn_agent","arguments":"{\"agent_type\":\"specialist\",\"message\":\"Return your specialist answer.\"}"}`)
			}
		}
		writeCodexSharedResponse(w, output)
	}), nil)
}

func writeCodexSharedResponse(w http.ResponseWriter, output json.RawMessage) {
	// The same completed-item SSE sequence is consumed by the pinned Codex CLI.
	w.Header().Set("Content-Type", "text/event-stream")
	responseID := "resp_" + uuid.NewString()
	for _, event := range []struct{ name, payload string }{
		{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_shared","object":"response","created_at":1677652290,"model":"gpt-5.2-codex","status":"in_progress","output":[],"parallel_tool_calls":false,"tools":[],"usage":{"input_tokens":10,"output_tokens":10,"total_tokens":20}}}`},
		{"response.output_item.added", fmt.Sprintf(`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":%s}`, strings.Replace(string(output), `"status":"completed"`, `"status":"in_progress"`, 1))},
		{"response.output_item.done", fmt.Sprintf(`{"type":"response.output_item.done","sequence_number":2,"output_index":0,"item":%s}`, output)},
		{"response.completed", fmt.Sprintf(`{"type":"response.completed","sequence_number":3,"response":{"id":"resp_shared","object":"response","created_at":1677652290,"model":"gpt-5.2-codex","status":"completed","output":[%s],"parallel_tool_calls":false,"tools":[],"usage":{"input_tokens":10,"output_tokens":10,"total_tokens":20}}}`, output)},
	} {
		if _, err := io.WriteString(w, "event: "+event.name+"\ndata: "+strings.ReplaceAll(event.payload, "resp_shared", responseID)+"\n\n"); err != nil {
			return
		}
	}
}
