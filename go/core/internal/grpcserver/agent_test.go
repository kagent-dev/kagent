package grpcserver

import (
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAgentServiceGeneratedClient(t *testing.T) {
	client := apiv1alpha1.NewAgentServiceClient(newTemplateAndHarnessConnection(t))
	ctx := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "alice")
	ref := &apiv1alpha1.ResourceReference{Namespace: "team", Name: "reviewer"}
	agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: ref.Namespace, Name: ref.Name}, Spec: v1alpha3.AgentSpec{
		Template: &v1alpha3.AgentTemplateSpec{SystemPrompt: "review code"},
		Harness:  testHarness("team", "runtime", "pool").Spec.DeepCopy(),
	}}
	created, err := client.CreateAgent(ctx, &apiv1alpha1.CreateAgentRequest{Ref: ref, Resource: structured(t, agent, "Agent")})
	require.NoError(t, err)
	require.Equal(t, "Agent", created.Agent.Resource.Kind)
	listed, err := client.ListAgents(ctx, &apiv1alpha1.ListAgentsRequest{Namespace: "team"})
	require.NoError(t, err)
	require.Len(t, listed.Agents, 1)
	agent.Spec.Template = nil
	agent.Spec.TemplateRef = &corev1.LocalObjectReference{Name: "shared-behavior"}
	updated, err := client.UpdateAgent(ctx, &apiv1alpha1.UpdateAgentRequest{Ref: ref, Resource: structured(t, agent, "Agent")})
	require.NoError(t, err)
	decoded := &v1alpha3.Agent{}
	require.NoError(t, structuredobject.ToGo(updated.Agent.Resource, "Agent", decoded, DefaultMaxMessageSize))
	require.Nil(t, decoded.Spec.Template)
	require.Equal(t, "shared-behavior", decoded.Spec.TemplateRef.Name)
	require.NotNil(t, decoded.Spec.Harness)
	_, err = client.DeleteAgent(ctx, &apiv1alpha1.DeleteAgentRequest{Ref: ref})
	require.NoError(t, err)
	listed, err = client.ListAgents(ctx, &apiv1alpha1.ListAgentsRequest{Namespace: "team"})
	require.NoError(t, err)
	require.Empty(t, listed.Agents)
}
