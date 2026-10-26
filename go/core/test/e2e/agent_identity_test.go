package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestAgentIdentityLookupWithGatewayJWT(t *testing.T) {
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	harness := testHarness{name: "kagent", runtimeLabel: "kagent"}
	recorder := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json")), nil)
	model := harness.createModel(t, kube, reachableModelURL(t, recorder.URL), nil)
	base := model.DeepCopy()
	model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "", ""
	model.Spec.ActorJWT = &v1alpha3.ActorJWT{Audiences: []string{"model-e2e", "kagent-agent-info"}}
	require.NoError(t, kube.Patch(t.Context(), model, ctrlclient.MergeFrom(base)))
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "identity-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{
		ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, SystemPrompt: "Reply briefly.",
	}}
	createAndWaitInteractionTemplate(t, harness, kube, template)
	agent := &v1alpha3.Agent{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(template), agent))
	fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
	fixture.send(t, "What is 2+2?")
	recorder.mu.Lock()
	requests := append([]recordedModelRequest(nil), recorder.requests...)
	recorder.mu.Unlock()
	require.NotEmpty(t, requests)
	bearer := requests[0].Header.Get("Authorization")
	require.True(t, strings.HasPrefix(bearer, "Bearer "))
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", bearer))
	connection := newControllerConn(t, target)
	lookup := apiv1.NewAgentIdentityServiceClient(connection)
	response, err := lookup.ResolveAgentIdentity(ctx, &apiv1.ResolveAgentIdentityRequest{})
	require.NoError(t, err)
	require.Equal(t, agent.Namespace, response.Agent.Namespace)
	require.Equal(t, agent.Name, response.Agent.Name)
	require.Equal(t, string(agent.UID), response.Agent.Uid)
	require.Equal(t, agent.Labels, response.Agent.Labels)
	require.Equal(t, agent.Annotations, response.Agent.Annotations)
	session, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.NoError(t, err)
	require.Equal(t, session.Session.Id, response.SessionId)
	require.Equal(t, agent.Namespace, session.Session.GetAgent().Ref.Namespace)
	require.Equal(t, agent.Name, session.Session.GetAgent().Ref.Name)
	require.Equal(t, string(agent.UID), session.Session.GetAgent().Uid)
	require.Equal(t, session.Session.PreparedRevision, response.RuntimeRevision)
	require.NotEmpty(t, response.RuntimeRevision)
	_, err = apiv1.NewSessionServiceClient(connection).GetSession(ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

// A scheduled Actor resolves to the Agent that supplied its pinned revision.
func TestScheduledRunIdentityLookupWithGatewayJWT(t *testing.T) {
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	harness := testHarness{name: "kagent", runtimeLabel: "kagent"}
	recorder := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json")), nil)
	model := harness.createModel(t, kube, reachableModelURL(t, recorder.URL), nil)
	base := model.DeepCopy()
	model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "", ""
	model.Spec.ActorJWT = &v1alpha3.ActorJWT{Audiences: []string{"model-e2e", "kagent-agent-info"}}
	require.NoError(t, kube.Patch(t.Context(), model, ctrlclient.MergeFrom(base)))
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "scheduled-identity-", Namespace: "kagent", Labels: map[string]string{"team": "operations"}}, Spec: v1alpha3.AgentTemplateSpec{
		ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, SystemPrompt: "Reply briefly.",
	}}
	createAndWaitInteractionTemplate(t, harness, kube, template)
	agent := &v1alpha3.Agent{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(template), agent))

	schedule := &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{GenerateName: "identity-schedule-", Namespace: "kagent", Labels: map[string]string{"team": "support"}}, Spec: v1alpha3.ScheduledRunSpec{
		AgentRef: corev1.LocalObjectReference{Name: agent.Name},
		Schedule: "0 9 * * *", Prompt: "What is 2+2?", Paused: new(true),
	}}
	require.NoError(t, kube.Create(t.Context(), schedule))
	t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), schedule)) })
	conn := newControllerConn(t, target)
	ctx := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e")
	f := &scheduledFixture{ctx: ctx, kube: kube, schedule: schedule, schedules: apiv1.NewScheduledRunServiceClient(conn)}
	f.waitReconciled(t)
	execution := f.trigger(t)
	sessions := apiv1.NewSessionServiceClient(conn)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		require.NoError(t, deleteIdleSession(cleanup, sessions, execution.SessionId))
	})
	var bearer string
	require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(context.Context) (bool, error) {
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		if len(recorder.requests) == 0 {
			return false, nil
		}
		bearer = recorder.requests[0].Header.Get("Authorization")
		return true, nil
	}))
	lookupCtx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", bearer))
	response, err := apiv1.NewAgentIdentityServiceClient(conn).ResolveAgentIdentity(lookupCtx, &apiv1.ResolveAgentIdentityRequest{})
	require.NoError(t, err)
	require.Equal(t, agent.Namespace, response.Agent.Namespace)
	require.Equal(t, agent.Name, response.Agent.Name)
	require.Equal(t, string(agent.UID), response.Agent.Uid)
	require.Equal(t, agent.Labels, response.Agent.Labels)
	require.Equal(t, execution.SessionId, response.SessionId)
	require.Equal(t, execution.RuntimeRevision, response.RuntimeRevision)
}
