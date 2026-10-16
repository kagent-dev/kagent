package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

// fakeAgentReadClient serves Agents as the kagent API does and records the requests.
type fakeAgentReadClient struct {
	t           *testing.T
	agents      []apiv1alpha3.Agent
	next        string
	err         error
	listRequest *apiv1.ListAgentsRequest
	getRequest  *apiv1.GetAgentRequest
}

func (f *fakeAgentReadClient) encode(agent apiv1alpha3.Agent) *apiv1.Agent {
	resource, err := structuredobject.FromGo(&agent, apiv1alpha3.GroupVersion.String(), agentKind, 0)
	require.NoError(f.t, err)
	return &apiv1.Agent{Ref: &apiv1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}, Resource: resource}
}

func (f *fakeAgentReadClient) ListAgents(_ context.Context, request *apiv1.ListAgentsRequest) (*apiv1.ListAgentsResponse, error) {
	f.listRequest = request
	if f.err != nil {
		return nil, f.err
	}
	response := &apiv1.ListAgentsResponse{Page: &apiv1.PageResponse{NextPageToken: f.next}}
	for _, agent := range f.agents {
		response.Agents = append(response.Agents, f.encode(agent))
	}
	return response, nil
}

func (f *fakeAgentReadClient) GetAgent(_ context.Context, request *apiv1.GetAgentRequest) (*apiv1.GetAgentResponse, error) {
	f.getRequest = request
	if f.err != nil {
		return nil, f.err
	}
	return &apiv1.GetAgentResponse{Agent: f.encode(f.agents[0])}, nil
}

func readyAgent(name string) apiv1alpha3.Agent {
	agent := apiv1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kagent"}}
	agent.Status.Conditions = []metav1.Condition{{Type: apiv1alpha3.AgentConditionReady, Status: metav1.ConditionTrue}}
	return agent
}

func TestListAgentsPagesThroughTheAPI(t *testing.T) {
	client := &fakeAgentReadClient{t: t, next: "next-page", agents: []apiv1alpha3.Agent{
		readyAgent("ready-agent"),
		{ObjectMeta: metav1.ObjectMeta{Name: "unknown-agent", Namespace: "kagent"}},
	}}
	var output bytes.Buffer

	err := getAgents(context.Background(), client, &AgentGetCfg{
		Namespace: "kagent", PageSize: 2, PageToken: "previous-page",
	}, clioutput.FormatTable, &output)
	require.NoError(t, err)
	assert.Equal(t, "kagent", client.listRequest.GetNamespace())
	assert.Equal(t, int32(2), client.listRequest.GetPage().GetLimit())
	assert.Equal(t, "previous-page", client.listRequest.GetPage().GetPageToken())
	// READY comes from status, so it proves the API returns the whole object.
	assert.Regexp(t, `ready-agent\s+\S\s+True`, output.String())
	assert.Regexp(t, `unknown-agent\s+\S\s+UNKNOWN`, output.String())
	assert.Contains(t, output.String(), "Next page token: next-page")
}

func TestListAgentsJSONKeepsTheKubernetesListShape(t *testing.T) {
	client := &fakeAgentReadClient{t: t, next: "next-page", agents: []apiv1alpha3.Agent{readyAgent("ready-agent")}}
	var output bytes.Buffer

	err := getAgents(context.Background(), client, &AgentGetCfg{Namespace: "kagent"}, clioutput.FormatJSON, &output)
	require.NoError(t, err)
	assert.Equal(t, int32(agentMaxPageSize), client.listRequest.GetPage().GetLimit())
	var list apiv1alpha3.AgentList
	require.NoError(t, json.Unmarshal(output.Bytes(), &list))
	assert.Equal(t, "next-page", list.Continue)
	require.Len(t, list.Items, 1)
	assert.Equal(t, "ready-agent", list.Items[0].Name)
}

func TestGetAgentByName(t *testing.T) {
	client := &fakeAgentReadClient{t: t, agents: []apiv1alpha3.Agent{readyAgent("ready-agent")}}
	var output bytes.Buffer

	err := getAgents(context.Background(), client, &AgentGetCfg{Namespace: "kagent", Name: "ready-agent"}, clioutput.FormatJSON, &output)
	require.NoError(t, err)
	assert.True(t, proto.Equal(&apiv1.ResourceReference{Namespace: "kagent", Name: "ready-agent"}, client.getRequest.GetRef()))
	var agent apiv1alpha3.Agent
	require.NoError(t, json.Unmarshal(output.Bytes(), &agent))
	assert.Equal(t, "ready-agent", agent.Name)
}

// An API error is returned as-is, with no fallback to Kubernetes.
func TestGetAgentsReturnsAPIErrors(t *testing.T) {
	for _, cfg := range []*AgentGetCfg{{Namespace: "kagent"}, {Namespace: "kagent", Name: "hidden"}} {
		client := &fakeAgentReadClient{t: t, err: status.Error(codes.PermissionDenied, "not allowed")}

		err := getAgents(context.Background(), client, cfg, clioutput.FormatTable, &bytes.Buffer{})
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	}
}
