package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
		&pushCfg{URL: "https://receiver.example/callback", BearerCredential: "receiver-credential"}, clioutput.FormatJSON, &out))
	require.Equal(t, a2a.TaskID("task"), client.created.TaskID)
	require.Equal(t, "receiver-credential", client.created.Auth.Credentials)
	require.Contains(t, out.String(), `"id":"generated"`)
	require.NoError(t, executePush(ctx, client, pages, pushCreate, "session", "task", nil,
		&pushCfg{URL: "https://receiver.example/callback", Token: "secret", BearerCredential: "receiver-credential"}, clioutput.FormatJSON, &out))
	require.Equal(t, "secret", client.created.Token)

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

func TestReadPushToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte("secret\r\n"), 0o600))
	token, err := readPushToken(path)
	require.NoError(t, err)
	require.Equal(t, "secret", token)
	_, err = readPushToken(path + "-missing")
	require.ErrorContains(t, err, "read notification token file")
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
	credentialFile := filepath.Join(t.TempDir(), "bearer")
	require.NoError(t, os.WriteFile(credentialFile, []byte("receiver-credential"), 0o600))
	client := &fakePushInvoker{state: a2a.TaskStateWorking}
	var out bytes.Buffer
	require.NoError(t, invokeWithPush(t.Context(), client, newInvokeRequest("hello"),
		&InvokeCfg{PushURL: "https://receiver.example/callback", PushBearerTokenFile: credentialFile}, clioutput.FormatJSON, &out))
	require.True(t, client.request.Config.ReturnImmediately)
	require.NotEmpty(t, client.request.Config.PushConfig.ID)
	require.Equal(t, "receiver-credential", client.request.Config.PushConfig.Auth.Credentials)
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Contains(t, result, "task")
	require.Contains(t, result, "pushConfig")
	require.NotContains(t, out.String(), "receiver-credential")
}

func TestInvokeWithPushEmbedsCallbackBeforeCompletedTask(t *testing.T) {
	credentialFile := filepath.Join(t.TempDir(), "bearer")
	require.NoError(t, os.WriteFile(credentialFile, []byte("receiver-credential"), 0o600))
	client := &fakePushInvoker{state: a2a.TaskStateCompleted}
	err := invokeWithPush(t.Context(), client, newInvokeRequest("hello"),
		&InvokeCfg{PushURL: "https://receiver.example/callback", PushBearerTokenFile: credentialFile}, clioutput.FormatTable, &bytes.Buffer{})
	require.NoError(t, err)
	require.NotNil(t, client.request.Config.PushConfig)
}

func TestValidatePushURL(t *testing.T) {
	require.NoError(t, validatePushURL("https://receiver.example/callback"))
	for _, raw := range []string{"", "receiver.example/callback", "ftp://receiver.example", "http://receiver.example", "https://user:pass@receiver.example", " https://receiver.example"} {
		require.Error(t, validatePushURL(raw), raw)
	}
}
