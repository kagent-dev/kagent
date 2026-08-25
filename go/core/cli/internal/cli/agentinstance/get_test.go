package agentinstance

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/cli/output"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const testInstanceID = "707f3f49-fdc4-40c5-93c7-472d37c8d355"

func TestValidateGetCfg(t *testing.T) {
	tests := []struct {
		name    string
		config  GetCfg
		wantErr string
	}{
		{name: "list"},
		{name: "list page", config: GetCfg{PageSize: 100, PageToken: "next"}},
		{name: "get", config: GetCfg{InstanceID: testInstanceID}},
		{name: "invalid ID", config: GetCfg{InstanceID: "not-an-id"}, wantErr: "invalid AgentInstance ID"},
		{name: "negative page size", config: GetCfg{PageSize: -1}, wantErr: "page size"},
		{name: "large page size", config: GetCfg{PageSize: 101}, wantErr: "page size"},
		{name: "pagination with get", config: GetCfg{InstanceID: testInstanceID, PageSize: 1}, wantErr: "pagination flags"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGetCfg(&tt.config)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGetAgentInstanceTableUsesFullID(t *testing.T) {
	client := &fakeAgentInstanceClient{instance: testInstance(), nextPageToken: "next-page"}
	cfg := &GetCfg{Config: &config.Config{Namespace: "kagent"}}
	var output bytes.Buffer

	require.NoError(t, get(t.Context(), client, cfg, clioutput.FormatTable, &output))
	assert.Equal(t, &apiv1alpha1.ListAgentInstancesRequest{
		Namespace: "kagent", Page: &apiv1alpha1.PageRequest{},
	}, client.listRequest)
	assert.Contains(t, output.String(), testInstanceID)
	assert.Contains(t, output.String(), "smoke")
	assert.Contains(t, output.String(), "READY")
	assert.Contains(t, output.String(), "Next page token: next-page")
}

func TestGetOneAgentInstanceJSON(t *testing.T) {
	client := &fakeAgentInstanceClient{instance: testInstance()}
	cfg := &GetCfg{Config: &config.Config{Namespace: "kagent"}, InstanceID: testInstanceID}
	var output bytes.Buffer

	require.NoError(t, get(t.Context(), client, cfg, clioutput.FormatJSON, &output))
	assert.Equal(t, testInstanceID, client.getRequest.GetAgentInstanceId())
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), testInstanceID)
}

func TestListAgentInstancesJSONPreservesNextPageToken(t *testing.T) {
	client := &fakeAgentInstanceClient{instance: testInstance(), nextPageToken: "next-page"}
	cfg := &GetCfg{
		Config: &config.Config{Namespace: "kagent"}, PageSize: 1, PageToken: "current-page",
	}
	var output bytes.Buffer

	require.NoError(t, get(t.Context(), client, cfg, clioutput.FormatJSON, &output))
	assert.Equal(t, int32(1), client.listRequest.GetPage().GetLimit())
	assert.Equal(t, "current-page", client.listRequest.GetPage().GetPageToken())
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), `"nextPageToken":"next-page"`)
}

type fakeAgentInstanceClient struct {
	instance      *apiv1alpha1.AgentInstance
	nextPageToken string
	getRequest    *apiv1alpha1.GetAgentInstanceRequest
	listRequest   *apiv1alpha1.ListAgentInstancesRequest
}

func (c *fakeAgentInstanceClient) GetAgentInstance(
	_ context.Context,
	request *apiv1alpha1.GetAgentInstanceRequest,
) (*apiv1alpha1.GetAgentInstanceResponse, error) {
	c.getRequest = request
	return &apiv1alpha1.GetAgentInstanceResponse{AgentInstance: c.instance}, nil
}

func (c *fakeAgentInstanceClient) ListAgentInstances(
	_ context.Context,
	request *apiv1alpha1.ListAgentInstancesRequest,
) (*apiv1alpha1.ListAgentInstancesResponse, error) {
	c.listRequest = request
	return &apiv1alpha1.ListAgentInstancesResponse{
		AgentInstances: []*apiv1alpha1.AgentInstance{c.instance},
		Page:           &apiv1alpha1.PageResponse{NextPageToken: c.nextPageToken},
	}, nil
}

func testInstance() *apiv1alpha1.AgentInstance {
	return &apiv1alpha1.AgentInstance{
		Id: testInstanceID, Namespace: "kagent", Creator: "e2e",
		Harness:       &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: "kagent"},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: "smoke"},
		State:         apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
		CreatedAt:     timestamppb.New(time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)),
	}
}
