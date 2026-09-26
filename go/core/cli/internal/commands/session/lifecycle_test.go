package session

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

func TestCreateSessionGeneratedRequestIDIsStable(t *testing.T) {
	cfg := &CreateCfg{Agent: "smoke"}
	ensureRequestID(cfg)
	requestID := cfg.RequestID
	require.NoError(t, uuid.Validate(requestID))
	ensureRequestID(cfg)
	assert.Equal(t, requestID, cfg.RequestID)

	client := &lifecycleSessionClient{createSession: testSession()}
	require.NoError(t, create(t.Context(), client, "kagent", cfg, clioutput.FormatTable, &bytes.Buffer{}))
	assert.Equal(t, requestID, client.createRequest.GetRequestId())
}

func TestCreateSessionExplicitReplayIDAndOutput(t *testing.T) {
	tests := []struct {
		name   string
		format clioutput.Format
	}{
		{name: "table", format: clioutput.FormatTable},
		{name: "json", format: clioutput.FormatJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &lifecycleSessionClient{createSession: testSession()}
			cfg := &CreateCfg{
				Agent: "smoke", RequestID: "replay-1",
			}
			var output bytes.Buffer

			require.NoError(t, create(t.Context(), client, "kagent", cfg, tt.format, &output))
			assert.Equal(t, &apiv1alpha1.CreateSessionRequest{
				Agent: &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: "smoke"}, RequestId: "replay-1",
			}, client.createRequest)
			assert.Contains(t, output.String(), testSessionID)
			if tt.format == clioutput.FormatJSON {
				assert.True(t, json.Valid(output.Bytes()))
			} else {
				assert.Contains(t, output.String(), "READY")
			}
		})
	}
}

func TestDeleteSession(t *testing.T) {
	client := &lifecycleSessionClient{deleteSession: &apiv1alpha1.Session{
		Id: testSessionID, State: apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED,
	}}
	cfg := &DeleteCfg{SessionID: testSessionID}
	var output bytes.Buffer

	require.NoError(t, deleteSession(t.Context(), client, cfg, clioutput.FormatTable, &output))
	assert.Equal(t, &apiv1alpha1.DeleteSessionRequest{
		SessionId: testSessionID,
	}, client.deleteRequest)
	assert.Contains(t, output.String(), testSessionID)
	assert.Contains(t, output.String(), "DELETED")
}

func TestDeleteSessionAborted(t *testing.T) {
	client := &lifecycleSessionClient{deleteErr: status.Error(codes.Aborted, "conflict")}
	cfg := &DeleteCfg{SessionID: testSessionID}

	err := deleteSession(t.Context(), client, cfg, clioutput.FormatTable, &bytes.Buffer{})
	require.ErrorContains(t, err, "lifecycle work is active or pending; inspect the Session and retry its pending operation")
	assert.Equal(t, codes.Aborted, status.Code(err))
}

type lifecycleSessionClient struct {
	createSession *apiv1alpha1.Session
	deleteSession *apiv1alpha1.Session
	createRequest *apiv1alpha1.CreateSessionRequest
	deleteRequest *apiv1alpha1.DeleteSessionRequest
	createErr     error
	deleteErr     error
}

func (c *lifecycleSessionClient) CreateSession(
	_ context.Context,
	request *apiv1alpha1.CreateSessionRequest,
) (*apiv1alpha1.CreateSessionResponse, error) {
	c.createRequest = request
	if c.createErr != nil {
		return nil, c.createErr
	}
	return &apiv1alpha1.CreateSessionResponse{Session: c.createSession}, nil
}

func (c *lifecycleSessionClient) DeleteSession(
	_ context.Context,
	request *apiv1alpha1.DeleteSessionRequest,
) (*apiv1alpha1.DeleteSessionResponse, error) {
	c.deleteRequest = request
	if c.deleteErr != nil {
		return nil, c.deleteErr
	}
	return &apiv1alpha1.DeleteSessionResponse{Session: c.deleteSession}, nil
}
