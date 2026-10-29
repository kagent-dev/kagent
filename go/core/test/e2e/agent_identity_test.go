package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	require.Equal(t, session.Session.PreparedRevision, response.RuntimeRevision)
	require.NotEmpty(t, response.RuntimeRevision)
	_, err = apiv1.NewSessionServiceClient(connection).GetSession(ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}
