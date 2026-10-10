package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/stretchr/testify/require"
)

type fakeTaskPushClient struct {
	created *a2a.PushConfig
	got     *a2a.GetTaskPushConfigRequest
	deleted *a2a.DeleteTaskPushConfigRequest
}

func (f *fakeTaskPushClient) CreateTaskPushConfig(_ context.Context, config *a2a.PushConfig) (*a2a.PushConfig, error) {
	f.created = config
	copy := *config
	if copy.ID == "" {
		copy.ID = "generated"
	}
	return &copy, nil
}

func (f *fakeTaskPushClient) GetTaskPushConfig(_ context.Context, request *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	f.got = request
	return &a2a.PushConfig{TaskID: request.TaskID, ID: request.ID, URL: "https://receiver.example/callback"}, nil
}

func (f *fakeTaskPushClient) DeleteTaskPushConfig(_ context.Context, request *a2a.DeleteTaskPushConfigRequest) error {
	f.deleted = request
	return nil
}

type fakePushPages struct {
	pageSize  int32
	pageToken string
}

func (f *fakePushPages) ListTaskPushConfigsPage(_ context.Context, sessionID, taskID string, size int32, token string) (*a2apb.ListTaskPushNotificationConfigsResponse, error) {
	f.pageSize, f.pageToken = size, token
	if sessionID != "session" || taskID != "task" {
		return nil, errors.New("wrong task route")
	}
	return &a2apb.ListTaskPushNotificationConfigsResponse{
		Configs:       []*a2apb.TaskPushNotificationConfig{{Id: "callback", TaskId: taskID, Url: "https://receiver.example/callback"}},
		NextPageToken: "next",
	}, nil
}

func TestExecutePushOperations(t *testing.T) {
	client := &fakeTaskPushClient{}
	pages := &fakePushPages{}
	ctx := t.Context()
	var out bytes.Buffer

	require.NoError(t, executePush(ctx, client, pages, pushCreate, "session", "task", nil,
		&pushCfg{URL: "https://receiver.example/callback"}, clioutput.FormatJSON, &out))
	require.Equal(t, a2a.TaskID("task"), client.created.TaskID)
	require.Nil(t, client.created.Auth)
	require.Contains(t, out.String(), `"id":"generated"`)
	require.NoError(t, executePush(ctx, client, pages, pushCreate, "session", "task", nil,
		&pushCfg{URL: "https://receiver.example/callback"}, clioutput.FormatJSON, &out))
	require.Empty(t, client.created.Token)
	require.NoError(t, executePush(ctx, client, pages, pushCreate, "session", "task", nil,
		&pushCfg{URL: "https://receiver.example/callback"}, clioutput.FormatJSON, &out))
	require.Nil(t, client.created.Auth)

	out.Reset()
	require.NoError(t, executePush(ctx, client, pages, pushGet, "session", "task", []string{"callback"},
		&pushCfg{}, clioutput.FormatTable, &out))
	require.Equal(t, "callback", client.got.ID)
	require.Contains(t, out.String(), "https://receiver.example/callback")

	out.Reset()
	require.NoError(t, executePush(ctx, client, pages, pushList, "session", "task", nil,
		&pushCfg{PageSize: 1, PageToken: "previous"}, clioutput.FormatTable, &out))
	require.EqualValues(t, 1, pages.pageSize)
	require.Equal(t, "previous", pages.pageToken)
	require.Contains(t, out.String(), "Next page token: next")

	out.Reset()
	require.NoError(t, executePush(ctx, client, pages, pushDelete, "session", "task", []string{"callback"},
		&pushCfg{}, clioutput.FormatJSON, &out))
	require.Equal(t, "callback", client.deleted.ID)
	var deletion map[string]string
	require.NoError(t, json.Unmarshal(out.Bytes(), &deletion))
	require.Equal(t, "deleted", deletion["status"])
}

type fakePushInvoker struct {
	request *a2a.SendMessageRequest
	state   a2a.TaskState
}

func (f *fakePushInvoker) SendMessage(_ context.Context, request *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	f.request = request
	return &a2a.Task{ID: "task-1", Status: a2a.TaskStatus{State: f.state}}, nil
}

func TestInvokeWithPushRegistersImmediateTask(t *testing.T) {
	client := &fakePushInvoker{state: a2a.TaskStateWorking}
	var out bytes.Buffer
	require.NoError(t, invokeWithPush(t.Context(), client, newInvokeRequest("hello"),
		&InvokeCfg{PushURL: "https://receiver.example/callback"}, clioutput.FormatJSON, &out))
	require.True(t, client.request.Config.ReturnImmediately)
	require.NotEmpty(t, client.request.Config.PushConfig.ID)
	require.Nil(t, client.request.Config.PushConfig.Auth)
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Contains(t, result, "task")
	require.Contains(t, result, "pushConfig")
	require.NotContains(t, out.String(), "receiver-credential")
}

func TestInvokeWithPushAllowsURLWithoutBearer(t *testing.T) {
	client := &fakePushInvoker{state: a2a.TaskStateWorking}
	require.NoError(t, invokeWithPush(t.Context(), client, newInvokeRequest("hello"),
		&InvokeCfg{PushURL: "https://receiver.example/callback"}, clioutput.FormatJSON, &bytes.Buffer{}))
	require.Nil(t, client.request.Config.PushConfig.Auth)
}

func TestInvokeWithPushEmbedsCallbackBeforeCompletedTask(t *testing.T) {
	client := &fakePushInvoker{state: a2a.TaskStateCompleted}
	err := invokeWithPush(t.Context(), client, newInvokeRequest("hello"),
		&InvokeCfg{PushURL: "https://receiver.example/callback"}, clioutput.FormatTable, &bytes.Buffer{})
	require.NoError(t, err)
	require.NotNil(t, client.request.Config.PushConfig)
}

func TestValidatePushURL(t *testing.T) {
	require.NoError(t, validatePushURL("https://receiver.example/callback"))
	require.NoError(t, validatePushURL("http://receiver.example/callback"))
	for _, raw := range []string{"", "receiver.example/callback", "ftp://receiver.example", "https://user:pass@receiver.example", " https://receiver.example"} {
		require.Error(t, validatePushURL(raw), raw)
	}
}
