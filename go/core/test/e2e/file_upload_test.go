package e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/util/retry"
)

// TestE2EFileUploadGoADKAgent verifies the end-to-end file upload round trip on
// a Go ADK agent: a user uploads a file inline (A2A file part) and the agent
// processes the request (file persisted as an artifact via
// SaveInputBlobsAsArtifacts) and responds successfully (AC8 upload path).
func TestE2EFileUploadGoADKAgent(t *testing.T) {
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_file_upload_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)

	goRuntime := v1alpha2.DeclarativeRuntime_Go
	agent := setupAgentWithOptions(t, cli, modelCfg.Name, nil, AgentOptions{
		Name:          "file-upload-go-adk-test",
		SystemMessage: "You are a helpful test agent that handles uploaded files.",
		Runtime:       &goRuntime,
	})

	a2aClient := setupA2AClient(t, agent)

	fileContent := []byte("hello from an uploaded file")
	filePart := a2atype.NewRawPart(fileContent)
	filePart.Filename = "note.txt"
	filePart.MediaType = "text/plain"
	textPart := a2atype.NewTextPart("Please confirm you received the uploaded file.")

	msg := a2atype.NewMessage(a2atype.MessageRoleUser, textPart, filePart)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var result a2atype.SendMessageResult
	err := retry.OnError(defaultRetry, func(err error) bool { return err != nil }, func() error {
		reqCtx, reqCancel := context.WithTimeout(ctx, 15*time.Second)
		defer reqCancel()
		var sendErr error
		result, sendErr = a2aClient.SendMessage(reqCtx, &a2atype.SendMessageRequest{Message: msg})
		return sendErr
	})
	require.NoError(t, err)

	taskResult, ok := result.(*a2atype.Task)
	require.True(t, ok)

	text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
	jsn, marshalErr := json.Marshal(taskResult)
	require.NoError(t, marshalErr)
	require.Contains(t, text, "received your uploaded file", string(jsn))
}
