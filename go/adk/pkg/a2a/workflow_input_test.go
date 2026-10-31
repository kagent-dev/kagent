package a2a

import (
	"fmt"
	"log/slog"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

func TestWorkflowInputHITLRoundTrip(t *testing.T) {
	for _, answer := range []string{"approve", "reject", `{"approved":true}`} {
		t.Run(answer, func(t *testing.T) {
			var received any
			rerun := true
			ask := workflow.NewEmittingFunctionNode("confirm", func(ctx agent.Context, _ any, emit func(*session.Event) error) (any, error) {
				return workflow.ResumeOrRequestInput(ctx, emit, session.RequestInput{
					InterruptID: "confirmation", Message: "Confirm the proposal",
				})
			}, workflow.NodeConfig{RerunOnResume: &rerun})
			finish := workflow.NewFunctionNode("finish", func(_ agent.Context, input any) (any, error) {
				received = input
				return input, nil
			}, workflow.NodeConfig{})
			root, err := workflowagent.New(workflowagent.Config{
				Name: "workflow", Edges: workflow.Chain(workflow.Start, ask, finish),
			})
			if err != nil {
				t.Fatal(err)
			}
			executor, err := NewKAgentExecutor(KAgentExecutorConfig{
				AppName: "test-app", SessionService: session.InMemoryService(),
				Logger:       slog.New(slog.DiscardHandler),
				RunnerConfig: runner.Config{AppName: "test-app", Agent: root},
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, call := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{
				a2atype.SvcParamExtensions: {HITLExtensionURI},
			}))
			if _, _, err := HITLActivationInterceptor().Before(ctx, call, &a2asrv.Request{}); err != nil {
				t.Fatal(err)
			}
			request := &a2asrv.ExecutorContext{
				TaskID: "task", ContextID: "context",
				Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("begin")),
			}
			var pause *a2atype.TaskStatusUpdateEvent
			for event, err := range executor.Execute(ctx, request) {
				if err != nil {
					t.Fatal(err)
				}
				if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State == a2atype.TaskStateInputRequired {
					pause = update
				}
			}
			if pause == nil || received != nil {
				t.Fatalf("pause = %#v, downstream input = %v", pause, received)
			}
			question := GetAskUserRequest(pause.Status.Message)
			if question == nil || question.ID != "confirmation" || len(question.Questions) != 1 {
				t.Fatalf("workflow input is not a public answerable question: %#v", question)
			}
			request.StoredTask = &a2atype.Task{ID: request.TaskID, ContextID: request.ContextID, Status: pause.Status}
			request.Message = hitlDecisionMessage(&apia2a.AskUserResponse{
				Type: HITLTypeAskUserResponse, ID: question.ID,
				Answers: []apia2a.AskUserAnswer{{Answer: []string{answer}}},
			})
			var completed bool
			for event, err := range executor.Execute(ctx, request) {
				if err != nil {
					t.Fatal(err)
				}
				if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State == a2atype.TaskStateCompleted {
					completed = true
				}
			}
			if !completed || received == nil {
				t.Fatalf("completed = %v, downstream input = %v", completed, received)
			}
			if answer != `{"approved":true}` && received != answer {
				t.Fatalf("downstream input = %v, want %q", received, answer)
			}
			if answer == `{"approved":true}` && fmt.Sprint(received) != "map[approved:true]" {
				t.Fatalf("structured input = %#v", received)
			}
		})
	}
}

func TestWorkflowInputResponseRequiresCorrelation(t *testing.T) {
	message := AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent,
		apia2a.NewToolCallPart("input", workflow.WorkflowInputFunctionCallName, nil)), &apia2a.AskUserRequest{
		Type: HITLTypeAskUserRequest, ID: "input",
		Questions: []apia2a.HITLQuestion{{Question: "Confirm"}},
	})
	stored := &a2atype.Task{Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: message}}
	for _, test := range []struct {
		name    string
		id      string
		answers []apia2a.AskUserAnswer
	}{
		{"stale request", "stale", []apia2a.AskUserAnswer{{Answer: []string{"approve"}}}},
		{"missing answer", "input", nil},
		{"multiple values", "input", []apia2a.AskUserAnswer{{Answer: []string{"approve", "reject"}}}},
		{"multiple answers", "input", []apia2a.AskUserAnswer{{Answer: []string{"approve"}}, {Answer: []string{"reject"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			incoming := hitlDecisionMessage(&apia2a.AskUserResponse{
				Type: HITLTypeAskUserResponse, ID: test.id, Answers: test.answers,
			})
			if _, err := BuildResumeHITLMessage(stored, incoming); err == nil {
				t.Fatal("invalid workflow input was accepted")
			}
		})
	}
}

func TestWorkflowInputRejectsMixedPendingCalls(t *testing.T) {
	for _, other := range []string{workflow.WorkflowInputFunctionCallName, "adk_request_confirmation"} {
		t.Run(other, func(t *testing.T) {
			parts, err := adka2a.ToA2AParts([]*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "input", Name: workflow.WorkflowInputFunctionCallName}},
				{FunctionCall: &genai.FunctionCall{ID: "other", Name: other}},
			}, []string{"input", "other"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := BuildHITLStatusMessage(a2atype.NewMessage(a2atype.MessageRoleAgent, parts...), true); err == nil {
				t.Fatal("mixed pending calls were silently discarded")
			}
		})
	}
}

func TestWorkflowInputWithoutExtensionRetainsReadablePrompt(t *testing.T) {
	parts, err := adka2a.ToA2AParts([]*genai.Part{{FunctionCall: &genai.FunctionCall{
		ID: "input", Name: workflow.WorkflowInputFunctionCallName, Args: map[string]any{"message": "Confirm the proposal"},
	}}}, []string{"input"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := BuildHITLStatusMessage(a2atype.NewMessage(a2atype.MessageRoleAgent, parts...), false)
	if err != nil {
		t.Fatal(err)
	}
	if GetAskUserRequest(public) != nil || messageText(public) != "Confirm the proposal" || !isWorkflowInput(public, "input") {
		t.Fatalf("invalid non-negotiated workflow input: %#v", public)
	}
}
