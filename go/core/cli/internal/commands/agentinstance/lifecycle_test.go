package agentinstance

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateAgentInstanceGeneratedRequestIDIsStable(t *testing.T) {
	cfg := &CreateCfg{Harness: "kagent", AgentTemplate: "smoke"}
	ensureRequestID(cfg)
	requestID := cfg.RequestID
	require.NoError(t, uuid.Validate(requestID))
	ensureRequestID(cfg)
	assert.Equal(t, requestID, cfg.RequestID)

	client := &lifecycleAgentInstanceClient{createInstance: testInstance()}
	require.NoError(t, create(t.Context(), client, "kagent", cfg, clioutput.FormatTable, &bytes.Buffer{}))
	assert.Equal(t, requestID, client.createRequest.GetRequestId())
}

func TestCreateAgentInstanceExplicitReplayIDAndOutput(t *testing.T) {
	tests := []struct {
		name   string
		format clioutput.Format
	}{
		{name: "table", format: clioutput.FormatTable},
		{name: "json", format: clioutput.FormatJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &lifecycleAgentInstanceClient{createInstance: testInstance()}
			cfg := &CreateCfg{
				Harness: "kagent", AgentTemplate: "smoke", RequestID: "replay-1",
			}
			var output bytes.Buffer

			require.NoError(t, create(t.Context(), client, "kagent", cfg, tt.format, &output))
			assert.Equal(t, &apiv1alpha1.CreateAgentInstanceRequest{
				Namespace: "kagent", Harness: "kagent", AgentTemplate: "smoke", RequestId: "replay-1",
			}, client.createRequest)
			assert.Contains(t, output.String(), testInstanceID)
			if tt.format == clioutput.FormatJSON {
				assert.True(t, json.Valid(output.Bytes()))
			} else {
				assert.Contains(t, output.String(), "READY")
			}
		})
	}
}

func TestDeleteAgentInstance(t *testing.T) {
	client := &lifecycleAgentInstanceClient{deleteInstance: &apiv1alpha1.AgentInstance{
		Id: testInstanceID, State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED,
	}}
	cfg := &DeleteCfg{InstanceID: testInstanceID}
	var output bytes.Buffer

	require.NoError(t, deleteAgentInstance(t.Context(), client, "kagent", cfg, clioutput.FormatTable, &output))
	assert.Equal(t, &apiv1alpha1.DeleteAgentInstanceRequest{
		Namespace: "kagent", AgentInstanceId: testInstanceID,
	}, client.deleteRequest)
	assert.Contains(t, output.String(), testInstanceID)
	assert.Contains(t, output.String(), "DELETED")
}

func TestDeleteAgentInstanceAborted(t *testing.T) {
	client := &lifecycleAgentInstanceClient{deleteErr: status.Error(codes.Aborted, "conflict")}
	cfg := &DeleteCfg{InstanceID: testInstanceID}

	err := deleteAgentInstance(t.Context(), client, "kagent", cfg, clioutput.FormatTable, &bytes.Buffer{})
	require.ErrorContains(t, err, "another lifecycle operation is in progress; retry after it completes")
	assert.Equal(t, codes.Aborted, status.Code(err))
}

type lifecycleAgentInstanceClient struct {
	createInstance *apiv1alpha1.AgentInstance
	deleteInstance *apiv1alpha1.AgentInstance
	createRequest  *apiv1alpha1.CreateAgentInstanceRequest
	deleteRequest  *apiv1alpha1.DeleteAgentInstanceRequest
	createErr      error
	deleteErr      error
}

func (c *lifecycleAgentInstanceClient) CreateAgentInstance(
	_ context.Context,
	request *apiv1alpha1.CreateAgentInstanceRequest,
) (*apiv1alpha1.CreateAgentInstanceResponse, error) {
	c.createRequest = request
	if c.createErr != nil {
		return nil, c.createErr
	}
	return &apiv1alpha1.CreateAgentInstanceResponse{AgentInstance: c.createInstance}, nil
}

func (c *lifecycleAgentInstanceClient) DeleteAgentInstance(
	_ context.Context,
	request *apiv1alpha1.DeleteAgentInstanceRequest,
) (*apiv1alpha1.DeleteAgentInstanceResponse, error) {
	c.deleteRequest = request
	if c.deleteErr != nil {
		return nil, c.deleteErr
	}
	return &apiv1alpha1.DeleteAgentInstanceResponse{AgentInstance: c.deleteInstance}, nil
}
