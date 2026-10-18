package e2e_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed mocks/invoke_claude_workspace.json
var claudeWorkspaceMocks []byte

const claudeWorkspacePrompt = "Probe the claude workspace."

// claudeWorkspaceAgent is a claude Harness Agent whose model runs the probe of
// invoke_claude_workspace.json with Bash, and whose egress reaches the
// workspace fixtures' origins.
type claudeWorkspaceAgent struct {
	template string
	model    *modelRecorder
}

func newClaudeWorkspaceAgent(t *testing.T, origins ...string) *claudeWorkspaceAgent {
	t.Helper()
	kube := interactionKubeClient(t)
	var cfg mockllm.Config
	require.NoError(t, json.Unmarshal(claudeWorkspaceMocks, &cfg))
	model := startModelRecorder(t, startMockLLMConfig(t, cfg), nil)
	modelConfig := createClaudeMockModel(t, kube, reachableServerURL(t, model.URL, ""))
	template := createClaudeTemplate(t, kube, modelConfig.Name, "Claude workspace fixture")
	allowEgress(t, kube, template, origins...)
	return &claudeWorkspaceAgent{template: template, model: model}
}

// allowEgress lets the Agent's sandbox reach origins and waits for the Agent
// to be ready at that generation.
func allowEgress(t *testing.T, kube ctrlclient.Client, name string, origins ...string) {
	t.Helper()
	agent := &v1alpha3.Agent{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: name}, agent))
	before := agent.DeepCopy()
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		require.NoError(t, err)
		agent.Spec.Egress = append(agent.Spec.Egress, parsed.Scheme+"://"+parsed.Host)
	}
	require.NoError(t, kube.Patch(t.Context(), agent, ctrlclient.MergeFrom(before)))
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, agentReadyTimeout, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
			return false, err
		}
		for _, condition := range agent.Status.Conditions {
			if condition.Type == v1alpha3.AgentConditionReady && condition.ObservedGeneration == agent.Generation {
				return condition.Status == metav1.ConditionTrue, nil
			}
		}
		return false, nil
	})
	require.NoError(t, err, "Agent %s ready with its egress; conditions: %+v", name, agent.Status.Conditions)
}

// session creates a Session of the agent, on fixture's volume when there is
// one.
func (a *claudeWorkspaceAgent) session(t *testing.T, target string, fixture *workspaceFixture) *interactionFixture {
	t.Helper()
	if fixture == nil {
		return newInteractionFixtureWith(t, target, a.template, nil)
	}
	session := newInteractionFixtureWith(t, target, a.template, fixture.admit(fixture.workspaceGrant(t)))
	session.retryWindow = workspaceSendRetryWindow
	return session
}

// probe runs the probe turn in session and returns its answer and the probe's
// output as key=value lines.
func (a *claudeWorkspaceAgent) probe(t *testing.T, session *interactionFixture) (*a2atype.Task, map[string]string) {
	t.Helper()
	before := len(a.model.Requests("", ""))
	_, _, task := session.send(t, claudeWorkspacePrompt)
	output := claudeToolResults(t, a.model.Requests("", "")[before:])
	// What the turn reported, such as the workspace setup's notice, and what
	// the probe saw: the explanation of any assertion that fails below.
	t.Logf("task %s messages:\n%s\n%s\nprobe output:\n%s", task.ID, taskMessages(getTask(t, session, task.ID)), taskMessages(task), output)
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "task %q", taskText(task))
	require.Contains(t, taskText(task), "CLAUDE_WORKSPACE_PROBED")
	probed := map[string]string{}
	for line := range strings.Lines(output) {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			probed[key] = value
		}
	}
	require.Equal(t, "done", probed["probe"], "probe output: %s", output)
	return task, probed
}

// claudeToolResults returns the text of every tool result in the Anthropic
// Messages requests, the last of which carries the whole conversation.
func claudeToolResults(t *testing.T, requests []recordedModelRequest) string {
	t.Helper()
	var results []string
	for _, request := range requests {
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if json.Unmarshal(request.Body, &body) != nil {
			continue
		}
		for _, message := range body.Messages {
			var blocks []struct {
				Type    string          `json:"type"`
				Content json.RawMessage `json:"content"`
			}
			if json.Unmarshal(message.Content, &blocks) != nil {
				continue
			}
			for _, block := range blocks {
				if block.Type == "tool_result" {
					results = append(results, blockText(block.Content))
				}
			}
		}
	}
	if len(results) == 0 {
		return ""
	}
	return results[len(results)-1]
}

// blockText is a tool result's content: a string or a list of text blocks.
func blockText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(content, &blocks)
	var parts []string
	for _, block := range blocks {
		parts = append(parts, block.Text)
	}
	return strings.Join(parts, "\n")
}

// taskMessages is the text of every message of task: its history and its
// status message.
func taskMessages(task *a2atype.Task) string {
	var parts []string
	messages := append([]*a2atype.Message(nil), task.History...)
	if task.Status.Message != nil {
		messages = append(messages, task.Status.Message)
	}
	for _, message := range messages {
		for _, part := range message.Parts {
			parts = append(parts, part.Text())
		}
	}
	return strings.Join(parts, "\n")
}

// A claude Session on a workspace works in its session directory: a clone
// with alternates of every mirror, none checked out until a switch, the
// repository that moved since the sync fetched and the other never requested;
// a Session without a volume works in /data/workspace; a fetch the origin
// refuses is the turn's first message, and the turn answers.
func TestClaudeSessionWorkspace(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	public := newWorkspaceFixtureOf(t, false)
	refusing := newWorkspaceFixture(t)
	agent := newClaudeWorkspaceAgent(t, public.OriginURL, refusing.OriginURL)

	t.Run("fetches the changed repository", func(t *testing.T) {
		started := time.Now()
		_, probed := agent.probe(t, agent.session(t, target, public))
		require.Equal(t, "/workspace", probed["pwd"])
		for _, repository := range workspaceRepositories {
			require.Contains(t, probed[repository+" alternates"], "/mirrors/"+repository+".git/objects", "%s borrows the mirror's objects", repository)
			require.Equal(t, "0", probed[repository+" tree"], "%s is not checked out", repository)
		}
		require.Equal(t, public.Heads["e2e/alpha"], probed["e2e/alpha origin"], "the unchanged repository stays at the sync")
		require.Equal(t, public.Origin.head(t, workspaceChanged+".git"), probed[workspaceChanged+" origin"], "the changed repository has the origin's commit")
		require.NotEqual(t, public.Heads[workspaceChanged], probed[workspaceChanged+" origin"])
		require.Contains(t, probed["switched"], "CHANGED.md", "git switch checks the fetched default branch out")
		require.Empty(t, receivedSince(public.Origin.received("e2e/alpha.git"), started), "the origin logged a request for the unchanged repository")
		require.NotEmpty(t, receivedSince(public.Origin.received(workspaceChanged+".git"), started), "the changed repository was fetched")
	})

	t.Run("reports a refused fetch", func(t *testing.T) {
		session := agent.session(t, target, refusing)
		task, probed := agent.probe(t, session)
		require.Equal(t, "/workspace", probed["pwd"])
		require.Equal(t, refusing.Heads[workspaceChanged], probed[workspaceChanged+" origin"], "a refused fetch leaves the sync's commit")
		messages := taskMessages(getTask(t, session, task.ID)) + "\n" + taskMessages(task)
		require.Contains(t, messages, workspaceChanged+": the origin refused the fetch", "the turn reports the refused fetch")
	})

	t.Run("without a volume", func(t *testing.T) {
		_, probed := agent.probe(t, agent.session(t, target, nil))
		require.Equal(t, "/data/workspace", probed["pwd"])
		require.Empty(t, probed["e2e/alpha alternates"])
	})
}
