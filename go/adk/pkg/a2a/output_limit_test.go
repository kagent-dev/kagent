package a2a

import (
	"context"
	"iter"
	"strings"
	"testing"

	"log/slog"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// runTurn runs one turn of an agent that yields the given responses and returns
// the artifact updates and the terminal task status the executor produced.
func runTurn(t *testing.T, responses []model.LLMResponse) ([]*a2atype.TaskArtifactUpdateEvent, *a2atype.TaskStatusUpdateEvent) {
	t.Helper()
	const appName = "output-limit-app"

	agent, err := adkagent.New(adkagent.Config{
		Name: "output-limit-agent",
		Run: func(ic adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
			return func(yield func(*adksession.Event, error) bool) {
				for _, response := range responses {
					event := &adksession.Event{
						Author:       ic.Agent().Name(),
						InvocationID: ic.InvocationID(),
						Branch:       ic.Branch(),
						LLMResponse:  response,
					}
					if !yield(event, nil) {
						return
					}
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}

	executor := NewKAgentExecutor(KAgentExecutorConfig{
		AppName:        appName,
		SessionService: adksession.InMemoryService(),
		Logger:         slog.New(slog.DiscardHandler),
		RunnerConfig: runner.Config{
			AppName: appName,
			Agent:   agent,
		},
	})
	reqCtx := &a2asrv.ExecutorContext{
		TaskID:    "task-1",
		ContextID: "context-1",
		Message:   a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	}

	var updates []*a2atype.TaskArtifactUpdateEvent
	var terminal *a2atype.TaskStatusUpdateEvent
	for event, err := range executor.Execute(context.Background(), reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		switch event := event.(type) {
		case *a2atype.TaskArtifactUpdateEvent:
			updates = append(updates, event)
		case *a2atype.TaskStatusUpdateEvent:
			if event.Status.State.Terminal() {
				terminal = event
			}
		}
	}
	if terminal == nil {
		t.Fatal("Execute() produced no terminal task status")
	}
	return updates, terminal
}

// statusText returns the text of a terminal status message.
func statusText(t *testing.T, status *a2atype.TaskStatusUpdateEvent) string {
	t.Helper()
	if status.Status.Message == nil || len(status.Status.Message.Parts) == 0 {
		t.Fatalf("terminal status = %#v, want a status message", status)
	}
	return status.Status.Message.Parts[0].Text()
}

func TestKAgentExecutorFailsTheTaskWhenTheModelStopsAtTheOutputLimit(t *testing.T) {
	updates, terminal := runTurn(t, []model.LLMResponse{
		{
			Content: genai.NewContentFromText("the answer starts", genai.RoleModel),
			Partial: true,
		},
		{
			Content:      genai.NewContentFromText("the answer starts and is cut", genai.RoleModel),
			FinishReason: genai.FinishReasonMaxTokens,
			// The reasoning tokens are a breakdown of the billed output, so the
			// limit named is 8192 and not 14192.
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				CandidatesTokenCount: 8192,
				ThoughtsTokenCount:   6000,
			},
		},
	})

	if terminal.Status.State != a2atype.TaskStateFailed {
		t.Fatalf("terminal state = %q, want %q", terminal.Status.State, a2atype.TaskStateFailed)
	}
	text := statusText(t, terminal)
	if !strings.Contains(text, "output limit") || !strings.Contains(text, "max_tokens=8192") {
		t.Fatalf("status message = %q, want the output limit and the limit value", text)
	}
	if len(updates) == 0 {
		t.Fatal("artifact updates = 0, want the partial answer kept")
	}
	last := updates[len(updates)-1]
	if len(last.Artifact.Parts) != 1 || last.Artifact.Parts[0].Text() != "the answer starts and is cut" {
		t.Fatalf("last artifact = %#v, want the streamed partial answer", last.Artifact)
	}
}

func TestKAgentExecutorOmitsTheLimitWhenUsageDoesNotNameIt(t *testing.T) {
	_, terminal := runTurn(t, []model.LLMResponse{{
		Content:      genai.NewContentFromText("cut", genai.RoleModel),
		FinishReason: genai.FinishReasonMaxTokens,
	}})

	if terminal.Status.State != a2atype.TaskStateFailed {
		t.Fatalf("terminal state = %q, want %q", terminal.Status.State, a2atype.TaskStateFailed)
	}
	text := statusText(t, terminal)
	if !strings.Contains(text, "output limit") || strings.Contains(text, "max_tokens=") {
		t.Fatalf("status message = %q, want the output limit without a number", text)
	}
}

func TestKAgentExecutorCompletesTheTaskWhenTheLastResponseStopsNormally(t *testing.T) {
	_, terminal := runTurn(t, []model.LLMResponse{
		{
			Content:      genai.NewContentFromText("a chunk", genai.RoleModel),
			Partial:      true,
			FinishReason: genai.FinishReasonMaxTokens,
		},
		{
			Content:      genai.NewContentFromText("the whole answer", genai.RoleModel),
			FinishReason: genai.FinishReasonStop,
		},
	})

	if terminal.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("terminal state = %q, want %q", terminal.Status.State, a2atype.TaskStateCompleted)
	}
	if terminal.Status.Message != nil {
		t.Fatalf("completed status message = %#v, want none", terminal.Status.Message)
	}
}
