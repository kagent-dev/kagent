package e2e_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/adk"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed mocks/invoke_workspace_session.json
var workspaceSessionMocks []byte

// The Event and its annotations a deleted Session on a volume leaves for the
// volume's owner.
const (
	directoryReleasedReason    = "SessionDirectoryReleased"
	directorySessionAnnotation = "kagent.dev/session-id"
	directorySubPathAnnotation = "kagent.dev/sub-path"
)

// workspaceAgent is a kagent Harness Agent with the bash tool and ask_user,
// whose model runs the shell commands of invoke_workspace_session.json in a
// Session's sandbox.
type workspaceAgent struct {
	template string
	model    *modelRecorder
}

func newWorkspaceAgent(t *testing.T) *workspaceAgent {
	t.Helper()
	kube := interactionKubeClient(t)
	// A skill is what gives the kagent Harness its bash tool.
	repositories := newGitFixture(t, "")
	server := repositories.serveThroughEgress(t)
	var cfg mockllm.Config
	require.NoError(t, json.Unmarshal(workspaceSessionMocks, &cfg))
	model := startModelRecorder(t, startMockLLMConfig(t, cfg), nil)
	modelConfig := createInteractionModel(t, kube, reachableModelURL(t, model.URL), nil)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-", Namespace: "kagent", Labels: goADKHarness.labels()},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: modelConfig.Name},
			SystemPrompt: "Work in the workspace with the bash tool.",
			Skills: []v1alpha3.AgentTemplateSkill{{Name: gitFixtureSkill, Source: v1alpha3.ArtifactSource{
				Git:  &v1alpha3.GitArtifact{URL: server + "/" + gitSkillRepository, Commit: repositories.commit},
				Path: "skills/" + gitFixtureSkill,
			}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, goADKHarness.name)
	return &workspaceAgent{template: template.Name, model: model}
}

// session creates a Session of the agent on the fixture's volume, admitted
// with a grant of the lane's issuer.
func (a *workspaceAgent) session(t *testing.T, target string, fixture *workspaceFixture) *interactionFixture {
	t.Helper()
	token := fixture.workspaceGrant(t)
	session := newInteractionFixtureWith(t, target, a.template, func(request *apiv1alpha1.CreateSessionRequest) {
		request.VolumeSource = &apiv1alpha1.SessionVolumeSource{
			Volume: &apiv1alpha1.SessionVolume{CsiDriver: fixture.Volume.Driver, VolumeHandle: fixture.Volume.Handle},
			Mounts: []*apiv1alpha1.SessionVolumeMount{
				{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
				{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
			},
		}
		request.AdmissionToken = token
	})
	session.ctx = metadata.AppendToOutgoingContext(session.ctx, strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)
	return session
}

// toolResults are the tool results of the session's last model request, which
// carries the whole conversation.
func (a *workspaceAgent) toolResults(t *testing.T, sessionID string) string {
	t.Helper()
	requests := a.model.Requests(adk.AgentInstanceHeader, sessionID)
	require.NotEmpty(t, requests, "no model request of session %s", sessionID)
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(requests[len(requests)-1].Body, &body))
	var results []string
	for _, message := range body.Messages {
		if message.Role == "tool" {
			var text string
			if json.Unmarshal(message.Content, &text) != nil {
				text = string(message.Content)
			}
			results = append(results, text)
		}
	}
	return strings.Join(results, "\n")
}

func (a *workspaceAgent) requireReply(t *testing.T, session *interactionFixture, task *a2atype.Task, want string) {
	t.Helper()
	require.True(t, task.Status.State == a2atype.TaskStateCompleted && strings.Contains(taskText(task), want),
		"task %s with %q, want %q; tool results: %s", task.Status.State, taskText(task), want, a.toolResults(t, session.sessionID))
}

// requireDirectoryReleased waits for the Event a deleted session on a volume
// leaves, naming its own directory.
func requireDirectoryReleased(t *testing.T, kube ctrlclient.Client, sessionID string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var events corev1.EventList
		if !assert.NoError(c, kube.List(t.Context(), &events, ctrlclient.InNamespace("kagent"))) {
			return
		}
		for _, event := range events.Items {
			if event.Reason == directoryReleasedReason && event.Annotations[directorySessionAnnotation] == sessionID {
				assert.Equal(c, "sessions/"+sessionID, event.Annotations[directorySubPathAnnotation])
				return
			}
		}
		assert.Fail(c, "no "+directoryReleasedReason+" Event for session "+sessionID)
	}, time.Minute, time.Second)
}

func deleteWorkspaceSession(t *testing.T, session *interactionFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", workspaceGrantSubject), 2*time.Minute)
	defer cancel()
	require.NoError(t, deleteIdleSession(ctx, session.sessions, session.sessionID))
}

// Ten Sessions on one workspace volume each start in their own empty directory
// at /workspace with the mirrors read-only at /mirrors, write only their own
// directory, and fail to write the mirrors; deleting them leaves their
// directories and the volume to the volume's owner, told by an Event each.
func TestSessionWorkspaceDirectory(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	fixture := newWorkspaceFixture(t)
	agent := newWorkspaceAgent(t)

	const sessionsOnOneVolume = 10
	ids := make([]string, sessionsOnOneVolume)
	t.Run("sessions", func(t *testing.T) {
		for i := range ids {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				session := agent.session(t, target, fixture)
				_, _, task := session.send(t, "Probe the workspace.")
				agent.requireReply(t, session, task, "WORKSPACE_PROBED")
				results := agent.toolResults(t, session.sessionID)
				for _, want := range []string{"entries=0", "mirrors=present", "mirrors=read-only", "own=written"} {
					require.Contains(t, results, want, "session %s", session.sessionID)
				}
				ids[i] = session.sessionID
				deleteWorkspaceSession(t, session)
				requireDirectoryReleased(t, kube, session.sessionID)
			})
		}
	})
	require.NotContains(t, ids, "", "every session probed its workspace")

	// The volume holds each session's directory with its own file only, and
	// no write reached the mirrors.
	script := `set -eu
test "$(ls /volume/sessions | wc -l | tr -d ' ')" = ` + fmt.Sprint(sessionsOnOneVolume) + `
for id in ` + strings.Join(ids, " ") + `; do
  test "$(ls -A "/volume/sessions/$id")" = OWN
done
test ! -e /volume/mirrors/written
` + workspaceMirrorHeadsScript("/volume/mirrors")
	report := runWorkspaceJob(t, kube, fixture.Claim, script, []workspaceMount{{Path: "/volume", ReadOnly: true}}, nil)
	require.Equal(t, fixture.Heads, parseWorkspaceHeads(t, report), "the mirrors are unchanged")
	requireWorkspaceVolumeBound(t, kube, fixture)
}

// A turn that ends in a question pauses its Session; the answer resumes it
// with a file written before the pause intact; deleting the Session leaves the
// directory and the volume in place and the Event for the volume's owner.
func TestSessionWorkspaceSurvivesAQuestion(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	fixture := newWorkspaceFixture(t)
	agent := newWorkspaceAgent(t)
	session := agent.session(t, target, fixture)

	_, _, waiting := session.send(t, "Write a note, then ask.")
	require.Equal(t, a2atype.TaskStateInputRequired, waiting.Status.State, "tool results: %s", agent.toolResults(t, session.sessionID))
	question := adka2a.GetAskUserRequest(waiting.Status.Message)
	require.NotNil(t, question, "INPUT_REQUIRED task has no ask_user request")
	waitForActorState(t, session, ateapipb.ActorState_ACTOR_STATE_PAUSED, 2*time.Minute)

	answer := adka2a.AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Keep the note")), &apia2a.AskUserResponse{
		Type: adka2a.HITLTypeAskUserResponse, ID: question.ID,
		Answers: []apia2a.AskUserAnswer{{Answer: []string{"Keep the note"}}},
	})
	answer.TaskID, answer.ContextID = waiting.ID, waiting.ContextID
	response, err := a2agrpc.NewGRPCTransportFromClient(session.client).SendMessage(session.ctx, nil, &a2atype.SendMessageRequest{Tenant: session.tenant, Message: answer})
	require.NoError(t, err, "resume with the answer")
	completed, ok := response.(*a2atype.Task)
	require.True(t, ok, "resumed response = %T", response)
	agent.requireReply(t, session, completed, "NOTE_READ")
	require.Contains(t, agent.toolResults(t, session.sessionID), "note=before-pause", "the note written before the pause")

	deleteWorkspaceSession(t, session)
	requireDirectoryReleased(t, kube, session.sessionID)
	report := runWorkspaceJob(t, kube, fixture.Claim,
		`printf '%s\n' "$(cat "/volume/sessions/`+session.sessionID+`/NOTE")" >> /dev/termination-log`,
		[]workspaceMount{{Path: "/volume", ReadOnly: true}}, nil)
	require.Equal(t, "before-pause", strings.TrimSpace(report), "the deleted session's directory stays for the volume's owner")
	requireWorkspaceVolumeBound(t, kube, fixture)
}

// requireWorkspaceVolumeBound fails unless the fixture's claim is still bound
// to its volume: no Session deleted or detached it.
func requireWorkspaceVolumeBound(t *testing.T, kube ctrlclient.Client, fixture *workspaceFixture) {
	t.Helper()
	claim := &corev1.PersistentVolumeClaim{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(fixture.Claim), claim))
	require.Equal(t, corev1.ClaimBound, claim.Status.Phase)
	volume := &corev1.PersistentVolume{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Name: claim.Spec.VolumeName}, volume))
	require.Equal(t, fixture.Volume.Handle, volume.Spec.CSI.VolumeHandle)
}
