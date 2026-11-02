package grpcserver

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestAgentServiceGeneratedClient(t *testing.T) {
	client := apiv1.NewAgentServiceClient(newTemplateAndHarnessConnection(t))
	ctx := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "alice")
	ref := &apiv1.ResourceReference{Namespace: "team", Name: "reviewer"}
	agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: ref.Namespace, Name: ref.Name}, Spec: v1alpha3.AgentSpec{
		Template:   v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{SystemPrompt: "review code"}},
		HarnessRef: corev1.LocalObjectReference{Name: "runtime"},
	}}
	created, err := client.CreateAgent(ctx, &apiv1.CreateAgentRequest{Ref: ref, Resource: structured(t, agent, "Agent")})
	require.NoError(t, err)
	require.Equal(t, "Agent", created.Agent.Resource.Kind)
	listed, err := client.ListAgents(ctx, &apiv1.ListAgentsRequest{Namespace: "team"})
	require.NoError(t, err)
	require.Len(t, listed.Agents, 1)
	agent.Spec.Template = v1alpha3.AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: "shared-behavior"}}
	updated, err := client.UpdateAgent(ctx, &apiv1.UpdateAgentRequest{Ref: ref, Resource: structured(t, agent, "Agent")})
	require.NoError(t, err)
	decoded := &v1alpha3.Agent{}
	require.NoError(t, structuredobject.ToGo(updated.Agent.Resource, "Agent", decoded, DefaultMaxMessageSize))
	require.Nil(t, decoded.Spec.Template.Inline)
	require.Equal(t, "shared-behavior", decoded.Spec.Template.Ref.Name)
	require.Equal(t, "runtime", decoded.Spec.HarnessRef.Name)
	_, err = client.DeleteAgent(ctx, &apiv1.DeleteAgentRequest{Ref: ref})
	require.NoError(t, err)
	listed, err = client.ListAgents(ctx, &apiv1.ListAgentsRequest{Namespace: "team"})
	require.NoError(t, err)
	require.Empty(t, listed.Agents)
}

// An empty namespace lists every namespace, and the page
// token walks them in namespace-then-name order without server-side state.
func TestAgentServiceListPagesAcrossNamespaces(t *testing.T) {
	client := apiv1.NewAgentServiceClient(newTemplateAndHarnessConnection(t))
	ctx := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "alice")
	for _, ref := range []*apiv1.ResourceReference{{Namespace: "team-b", Name: "one"}, {Namespace: "team-a", Name: "two"}, {Namespace: "team-a", Name: "three"}} {
		agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: ref.Namespace, Name: ref.Name}, Spec: v1alpha3.AgentSpec{
			Template:   v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{SystemPrompt: "review code"}},
			HarnessRef: corev1.LocalObjectReference{Name: "runtime"},
		}}
		_, err := client.CreateAgent(ctx, &apiv1.CreateAgentRequest{Ref: ref, Resource: structured(t, agent, "Agent")})
		require.NoError(t, err)
	}

	var seen []string
	token := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 3, "paging never ended")
		listed, err := client.ListAgents(ctx, &apiv1.ListAgentsRequest{Page: &apiv1.PageRequest{Limit: 2, PageToken: token}})
		require.NoError(t, err)
		for _, agent := range listed.Agents {
			seen = append(seen, agent.Ref.Namespace+"/"+agent.Ref.Name)
		}
		if token = listed.GetPage().GetNextPageToken(); token == "" {
			break
		}
	}
	require.Equal(t, []string{"team-a/three", "team-a/two", "team-b/one"}, seen)
}
