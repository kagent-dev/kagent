package agent

import (
	"context"
	"iter"
	"log/slog"
	"sync"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	kagenta2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

type deleteFileArgs struct {
	Path string `json:"path"`
}

// deleteCallModel calls delete_file on its first request and answers with text
// after that, keeping every request it sees.
type deleteCallModel struct {
	mu       sync.Mutex
	requests []*adkmodel.LLMRequest
}

func (m *deleteCallModel) Name() string { return "delete-call" }

func (m *deleteCallModel) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	first := len(m.requests) == 1
	m.mu.Unlock()
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		content := genai.NewContentFromText("done", genai.RoleModel)
		if first {
			call := genai.NewPartFromFunctionCall("delete_file", map[string]any{"path": "/tmp/x"})
			call.FunctionCall.ID = "delete-call"
			content = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{call}}
		}
		yield(&adkmodel.LLMResponse{Content: content, FinishReason: genai.FinishReasonStop}, nil)
	}
}

func (m *deleteCallModel) lastRequest() *adkmodel.LLMRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests[len(m.requests)-1]
}

// The reason a person gives for rejecting a call crosses the A2A decision, the
// adk-go resume and the on-tool-error callback, and reaches the model as the
// response of the rejected call.
func TestRejectionReasonReachesTheModelOnResume(t *testing.T) {
	const appName, taskID, contextID = "reject-app", "reject-task", "reject-context"
	deleteFile, err := functiontool.New(
		functiontool.Config{Name: "delete_file", Description: "Deletes a file.", RequireConfirmation: true},
		func(adkagent.Context, deleteFileArgs) (map[string]any, error) {
			t.Error("a rejected call must not run")
			return nil, nil
		},
	)
	require.NoError(t, err)
	llm := &deleteCallModel{}
	logger := slog.New(slog.DiscardHandler)
	root, err := llmagent.New(llmagent.Config{
		Name:                 "reject-agent",
		Model:                llm,
		Tools:                []tool.Tool{deleteFile},
		OnToolErrorCallbacks: []llmagent.OnToolErrorCallback{makeOnToolErrorCallback(logger)},
	})
	require.NoError(t, err)
	executor, err := kagenta2a.NewKAgentExecutor(kagenta2a.KAgentExecutorConfig{
		AppName: appName, SessionService: adksession.InMemoryService(), Logger: logger,
		RunnerConfig: runner.Config{AppName: appName, Agent: root},
	})
	require.NoError(t, err)
	ctx, callCtx := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
		a2atype.SvcParamExtensions: {kagenta2a.HITLExtensionURI},
	}))
	_, _, err = kagenta2a.HITLActivationInterceptor().Before(ctx, callCtx, &a2asrv.Request{})
	require.NoError(t, err)

	first := &a2asrv.ExecutorContext{
		TaskID: taskID, ContextID: contextID,
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("delete /tmp/x")),
	}
	var pause *a2atype.TaskStatusUpdateEvent
	for event, err := range executor.Execute(ctx, first) {
		require.NoError(t, err)
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State == a2atype.TaskStateInputRequired {
			pause = update
		}
	}
	require.NotNil(t, pause, "the call waits for a decision")
	approval := kagenta2a.GetToolApprovalRequest(pause.Status.Message)
	require.NotNil(t, approval)
	require.Len(t, approval.Tools, 1)

	decision := kagenta2a.AttachHitlExtension(
		a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Decision")),
		&apia2a.ToolApprovalResponse{
			Type: kagenta2a.HITLTypeToolApprovalResponse,
			Approvals: []apia2a.ToolApproval{{
				ID: approval.Tools[0].ID, Approved: false, RejectionReason: "What does this tool do?",
			}},
		},
	)
	decision.TaskID, decision.ContextID = taskID, contextID
	resume := &a2asrv.ExecutorContext{
		TaskID: taskID, ContextID: contextID, Message: decision,
		StoredTask: &a2atype.Task{
			ID: taskID, ContextID: contextID, Status: pause.Status, History: []*a2atype.Message{first.Message, decision},
		},
	}
	for _, err := range executor.Execute(ctx, resume) {
		require.NoError(t, err)
	}

	var rejected *genai.FunctionResponse
	for _, content := range llm.lastRequest().Contents {
		for _, part := range content.Parts {
			if part.FunctionResponse != nil && part.FunctionResponse.ID == "delete-call" {
				rejected = part.FunctionResponse
			}
		}
	}
	require.NotNil(t, rejected, "the model sees the response of the rejected call")
	require.Equal(t, map[string]any{
		"error":   "Tool call was rejected by user. Reason: What does this tool do?",
		"isError": true,
	}, rejected.Response)
}
