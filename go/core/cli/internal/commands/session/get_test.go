package session

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const testSessionID = "707f3f49-fdc4-40c5-93c7-472d37c8d355"

func TestValidateGetCfg(t *testing.T) {
	tests := []struct {
		name    string
		config  GetCfg
		wantErr string
	}{
		{name: "list"},
		{name: "list page", config: GetCfg{PageSize: 100, PageToken: "next"}},
		{name: "get", config: GetCfg{SessionID: testSessionID}},
		{name: "invalid ID", config: GetCfg{SessionID: "not-an-id"}, wantErr: "invalid Session ID"},
		{name: "negative page size", config: GetCfg{PageSize: -1}, wantErr: "page size"},
		{name: "large page size", config: GetCfg{PageSize: 101}, wantErr: "page size"},
		{name: "pagination with get", config: GetCfg{SessionID: testSessionID, PageSize: 1}, wantErr: "pagination flags"},
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

func TestGetSessionTableUsesFullID(t *testing.T) {
	client := &fakeSessionClient{session: testSession(), nextPageToken: "next-page"}
	cfg := &GetCfg{}
	var output bytes.Buffer

	require.NoError(t, get(t.Context(), client, cfg, clioutput.FormatTable, &output))
	assert.Equal(t, &apiv1alpha1.ListSessionsRequest{
		Page: &apiv1alpha1.PageRequest{},
	}, client.listRequest)
	assert.Contains(t, output.String(), testSessionID)
	assert.Contains(t, output.String(), "smoke")
	assert.Contains(t, output.String(), "READY")
	assert.Contains(t, output.String(), "Next page token: next-page")
}

func TestGetOneSessionJSON(t *testing.T) {
	client := &fakeSessionClient{session: testSession()}
	cfg := &GetCfg{SessionID: testSessionID}
	var output bytes.Buffer

	require.NoError(t, get(t.Context(), client, cfg, clioutput.FormatJSON, &output))
	assert.Equal(t, testSessionID, client.getRequest.GetSessionId())
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), testSessionID)
}

func TestListSessionsJSONPreservesNextPageToken(t *testing.T) {
	client := &fakeSessionClient{session: testSession(), nextPageToken: "next-page"}
	cfg := &GetCfg{
		PageSize: 1, PageToken: "current-page",
	}
	var output bytes.Buffer

	require.NoError(t, get(t.Context(), client, cfg, clioutput.FormatJSON, &output))
	assert.Equal(t, int32(1), client.listRequest.GetPage().GetLimit())
	assert.Equal(t, "current-page", client.listRequest.GetPage().GetPageToken())
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), `"nextPageToken":"next-page"`)
}

type fakeSessionClient struct {
	session       *apiv1alpha1.Session
	nextPageToken string
	getRequest    *apiv1alpha1.GetSessionRequest
	listRequest   *apiv1alpha1.ListSessionsRequest
}

func (c *fakeSessionClient) GetSession(
	_ context.Context,
	request *apiv1alpha1.GetSessionRequest,
) (*apiv1alpha1.GetSessionResponse, error) {
	c.getRequest = request
	return &apiv1alpha1.GetSessionResponse{Session: c.session}, nil
}

func (c *fakeSessionClient) ListSessions(
	_ context.Context,
	request *apiv1alpha1.ListSessionsRequest,
) (*apiv1alpha1.ListSessionsResponse, error) {
	c.listRequest = request
	return &apiv1alpha1.ListSessionsResponse{
		Sessions: []*apiv1alpha1.Session{c.session},
		Page:     &apiv1alpha1.PageResponse{NextPageToken: c.nextPageToken},
	}, nil
}

func testSession() *apiv1alpha1.Session {
	return &apiv1alpha1.Session{
		Id: testSessionID, Creator: "e2e",

		Agent:     &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: "smoke"},
		State:     apiv1alpha1.SessionState_SESSION_STATE_READY,
		CreatedAt: timestamppb.New(time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)),
	}
}
