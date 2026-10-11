package e2e_test

import (
	_ "embed"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
)

//go:embed mocks/invoke_go_adk_workspace.json
var goADKWorkspaceMocks []byte

// probeGoADKWorkspace runs the probe turn of invoke_go_adk_workspace.json in
// session and returns the turn's task and the probe's output as key=value
// lines.
func probeGoADKWorkspace(t *testing.T, agent *workspaceAgent, session *interactionFixture) (*a2atype.Task, map[string]string) {
	t.Helper()
	_, _, task := session.send(t, "Probe the Go ADK workspace.")
	output := agent.toolResults(t, session.sessionID)
	// What the turn reported, such as the workspace setup's notice, and what
	// the probe saw: the explanation of any assertion that fails below.
	t.Logf("task %s messages:\n%s\n%s\nprobe output:\n%s", task.ID, taskMessages(getTask(t, session, task.ID)), taskMessages(task), output)
	agent.requireReply(t, session, task, "GO_ADK_WORKSPACE_PROBED")
	probed := map[string]string{}
	for line := range strings.Lines(output) {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			probed[key] = value
		}
	}
	require.Equal(t, "done", probed["probe"], "probe output: %s", output)
	return task, probed
}

// A Declarative agent on the kagent Harness works in its Session's directory:
// before the first turn the Go ADK runtime clones every mirror with
// alternates, none checked out until a switch, and fetches the repository
// that moved since the sync while the other is never requested; bash runs in
// the directory, a Session without a volume works in /data/workspace, and a
// fetch the origin refuses is the turn's first message while the turn answers.
func TestGoADKSessionWorkspace(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	public := newWorkspaceFixtureOf(t, false)
	refusing := newWorkspaceFixture(t)
	agent := newBashWorkspaceAgent(t, goADKWorkspaceMocks, public.OriginURL, refusing.OriginURL)

	t.Run("fetches the changed repository", func(t *testing.T) {
		started := time.Now()
		_, probed := probeGoADKWorkspace(t, agent, agent.session(t, target, public))
		require.Equal(t, "/workspace", probed["pwd"], "bash runs in the session directory")
		require.Equal(t, "present", probed["notes"], "the setup's notes are in the directory")
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
		task, probed := probeGoADKWorkspace(t, agent, session)
		require.Equal(t, "/workspace", probed["pwd"])
		require.Equal(t, refusing.Heads[workspaceChanged], probed[workspaceChanged+" origin"], "a refused fetch leaves the sync's commit")
		messages := taskMessages(getTask(t, session, task.ID)) + "\n" + taskMessages(task)
		require.Contains(t, messages, workspaceChanged+": the origin refused the fetch", "the turn reports the refused fetch")
	})

	t.Run("without a volume", func(t *testing.T) {
		_, probed := probeGoADKWorkspace(t, agent, agent.session(t, target, nil))
		require.Equal(t, "/data/workspace", probed["pwd"])
		require.Empty(t, probed["e2e/alpha alternates"])
	})
}
