package a2a

import (
	"context"
	"iter"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/go-logr/logr"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// runUsageAgent runs an agent emitting the given responses through the
// executor and returns the artifact updates and the terminal status update.
func runUsageAgent(
	t *testing.T,
	storedTask *a2atype.Task,
	responses ...model.LLMResponse,
) ([]*a2atype.TaskArtifactUpdateEvent, *a2atype.TaskStatusUpdateEvent) {
	t.Helper()

	const appName = "usage-app"

	agent, err := adkagent.New(adkagent.Config{
		Name: "usage-agent",
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
		Logger:         logr.Discard(),
		RunnerConfig: runner.Config{
			AppName: appName,
			Agent:   agent,
		},
	})

	reqCtx := &a2asrv.ExecutorContext{
		TaskID:     "task-1",
		ContextID:  "context-1",
		Message:    a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
		StoredTask: storedTask,
	}

	var (
		updates  []*a2atype.TaskArtifactUpdateEvent
		terminal *a2atype.TaskStatusUpdateEvent
	)
	for event, err := range executor.Execute(t.Context(), reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		switch event := event.(type) {
		case *a2atype.TaskArtifactUpdateEvent:
			updates = append(updates, event)
		case *a2atype.TaskStatusUpdateEvent:
			if event.Status.State.Terminal() || event.Status.State == a2atype.TaskStateInputRequired {
				terminal = event
			}
		}
	}
	if terminal == nil {
		t.Fatal("no terminal status update emitted")
	}
	return updates, terminal
}

func usageTotalFrom(t *testing.T, event *a2atype.TaskStatusUpdateEvent) map[string]any {
	t.Helper()
	total, ok := event.Metadata[usageTotalMetadataKey].(map[string]any)
	if !ok {
		t.Fatalf("metadata[%s] = %#v, want map", usageTotalMetadataKey, event.Metadata[usageTotalMetadataKey])
	}
	return total
}

func assertTokenCount(t *testing.T, total map[string]any, key string, want float64) {
	t.Helper()
	got, ok := total[key].(float64)
	if !ok {
		t.Fatalf("%s = %#v, want number", key, total[key])
	}
	if got != want {
		t.Fatalf("%s = %v, want %v", key, got, want)
	}
}

func usageResponse(text string, prompt, candidates, total int32) model.LLMResponse {
	return model.LLMResponse{
		Content:      genai.NewContentFromText(text, genai.RoleModel),
		ModelVersion: "gpt-4o-2024-11-20",
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     prompt,
			CandidatesTokenCount: candidates,
			TotalTokenCount:      total,
		},
	}
}

func TestTurnUsageAggregatesNonPartialEvents(t *testing.T) {
	_, terminal := runUsageAgent(t, nil,
		usageResponse("first", 10, 5, 15),
		usageResponse("second", 20, 7, 27),
	)

	total := usageTotalFrom(t, terminal)
	assertTokenCount(t, total, "promptTokenCount", 30)
	assertTokenCount(t, total, "candidatesTokenCount", 12)
	assertTokenCount(t, total, "totalTokenCount", 42)
	if got := total["modelVersion"]; got != "gpt-4o-2024-11-20" {
		t.Fatalf("modelVersion = %#v, want the emitting model", got)
	}
}

func TestTurnUsageSkipsPartialAndEmptyEvents(t *testing.T) {
	partial := usageResponse("par", 100, 100, 200)
	partial.Partial = true
	noUsage := model.LLMResponse{Content: genai.NewContentFromText("no usage", genai.RoleModel)}

	_, terminal := runUsageAgent(t, nil, partial, noUsage, usageResponse("final", 10, 5, 15))

	total := usageTotalFrom(t, terminal)
	assertTokenCount(t, total, "promptTokenCount", 10)
	assertTokenCount(t, total, "candidatesTokenCount", 5)
	assertTokenCount(t, total, "totalTokenCount", 15)
}

func TestTurnUsageOmittedWhenNoUsageReported(t *testing.T) {
	_, terminal := runUsageAgent(t, nil, model.LLMResponse{
		Content: genai.NewContentFromText("no usage", genai.RoleModel),
	})

	if _, ok := terminal.Metadata[usageTotalMetadataKey]; ok {
		t.Fatalf("metadata[%s] present, want omitted when nothing was reported", usageTotalMetadataKey)
	}
}

// TestTurnUsageShapeMatchesPerEventUsageMetadata pins the aggregate to the same
// serialization as the per-event adk_usage_metadata entry, so consumers can
// share a single parser.
func TestTurnUsageShapeMatchesPerEventUsageMetadata(t *testing.T) {
	updates, terminal := runUsageAgent(t, nil, usageResponse("only", 10, 5, 15))

	var perEvent map[string]any
	for _, update := range updates {
		if usage, ok := update.Artifact.Metadata[adka2a.ToA2AMetaKey("usage_metadata")].(map[string]any); ok {
			perEvent = usage
		}
	}
	if perEvent == nil {
		t.Fatalf("per-event usage metadata missing from %d artifact updates", len(updates))
	}

	total := usageTotalFrom(t, terminal)
	for key, want := range perEvent {
		if got := total[key]; got != want {
			t.Fatalf("%s = %#v, want %#v (same shape as per-event usage)", key, got, want)
		}
	}
}

func TestTurnUsageSeedFromTaskAccumulatesAcrossExecutions(t *testing.T) {
	storedTask := &a2atype.Task{
		ID:        "task-1",
		ContextID: "context-1",
		Metadata: map[string]any{
			usageTotalMetadataKey: map[string]any{
				// float64 as it would be after a JSON round-trip through a task store.
				"promptTokenCount":     float64(100),
				"candidatesTokenCount": float64(50),
				"totalTokenCount":      float64(150),
				"modelVersion":         "gpt-4o-2024-08-06",
			},
		},
	}

	_, terminal := runUsageAgent(t, storedTask, usageResponse("resumed", 10, 5, 15))

	total := usageTotalFrom(t, terminal)
	assertTokenCount(t, total, "promptTokenCount", 110)
	assertTokenCount(t, total, "candidatesTokenCount", 55)
	assertTokenCount(t, total, "totalTokenCount", 165)
	if got := total["modelVersion"]; got != "gpt-4o-2024-11-20" {
		t.Fatalf("modelVersion = %#v, want the model of the latest execution", got)
	}
}

func TestTurnUsageSeedFromTaskIgnoresMissingOrMalformed(t *testing.T) {
	tests := map[string]*a2atype.Task{
		"nil task":          nil,
		"no metadata":       {ID: "task-1"},
		"unrelated keys":    {ID: "task-1", Metadata: map[string]any{"other": 1}},
		"total not a map":   {ID: "task-1", Metadata: map[string]any{usageTotalMetadataKey: "nope"}},
		"counts not number": {ID: "task-1", Metadata: map[string]any{usageTotalMetadataKey: map[string]any{"promptTokenCount": "many"}}},
	}

	for name, task := range tests {
		t.Run(name, func(t *testing.T) {
			usage := &turnUsage{}
			usage.seedFromTask(task)
			if !usage.empty() {
				t.Fatalf("usage = %#v, want empty", usage)
			}
		})
	}
}

// TestTurnUsageAcrossHITLCycle covers the input-required terminal state: the
// pause carries the total so far, and the execution resuming from the stored
// task continues from it instead of restarting at zero.
func TestTurnUsageAcrossHITLCycle(t *testing.T) {
	const (
		appName   = "hitl-usage-app"
		contextID = "hitl-usage-context"
		taskID    = "hitl-usage-task"
	)

	invocations := 0
	agent, err := adkagent.New(adkagent.Config{
		Name: "hitl-usage-agent",
		Run: func(ic adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
			return func(yield func(*adksession.Event, error) bool) {
				invocations++
				if invocations == 1 {
					call := genai.NewPartFromFunctionCall("adk_request_confirmation", map[string]any{
						"originalFunctionCall": map[string]any{"name": "delete_file", "id": "tool-call", "args": map[string]any{"path": "/tmp/x"}},
						"toolConfirmation":     map[string]any{"hint": "Delete /tmp/x?", "confirmed": false, "payload": nil},
					})
					call.FunctionCall.ID = "confirmation-call"
					yield(&adksession.Event{
						Author: ic.Agent().Name(), InvocationID: ic.InvocationID(), Branch: ic.Branch(),
						LLMResponse: model.LLMResponse{
							Content:      &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{call}},
							ModelVersion: "gpt-4o-2024-11-20",
							UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
								PromptTokenCount: 10, CandidatesTokenCount: 5, TotalTokenCount: 15,
							},
						},
						LongRunningToolIDs: []string{"confirmation-call"},
					}, nil)
					return
				}
				yield(&adksession.Event{
					Author: ic.Agent().Name(), InvocationID: ic.InvocationID(), Branch: ic.Branch(),
					LLMResponse: usageResponse("resumed", 20, 7, 27),
				}, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}

	executor := NewKAgentExecutor(KAgentExecutorConfig{
		AppName: appName, SessionService: adksession.InMemoryService(), Logger: logr.Discard(),
		RunnerConfig: runner.Config{AppName: appName, Agent: agent},
	})

	var pause *a2atype.TaskStatusUpdateEvent
	first := &a2asrv.ExecutorContext{
		TaskID: taskID, ContextID: contextID,
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("delete it")),
	}
	for event, err := range executor.Execute(t.Context(), first) {
		if err != nil {
			t.Fatalf("pause Execute() error = %v", err)
		}
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State == a2atype.TaskStateInputRequired {
			pause = update
		}
	}
	if pause == nil {
		t.Fatal("no input-required status update emitted")
	}
	paused := usageTotalFrom(t, pause)
	assertTokenCount(t, paused, "promptTokenCount", 10)
	assertTokenCount(t, paused, "totalTokenCount", 15)

	// a2a-go merges terminal status update metadata into the stored task.
	stored := &a2atype.Task{ID: taskID, ContextID: contextID, Status: pause.Status, Metadata: pause.Metadata}
	decision := hitlDecisionMessage(&ToolApprovalResponse{
		Type:      HITLTypeToolApprovalResponse,
		Approvals: []ToolApproval{{ID: "confirmation-call", Approved: true}},
	})
	decision.TaskID, decision.ContextID = taskID, contextID

	resume := &a2asrv.ExecutorContext{
		TaskID: taskID, ContextID: contextID, Message: decision, StoredTask: stored,
	}
	var terminal *a2atype.TaskStatusUpdateEvent
	for event, err := range executor.Execute(t.Context(), resume) {
		if err != nil {
			t.Fatalf("resume Execute() error = %v", err)
		}
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State.Terminal() {
			terminal = update
		}
	}
	if terminal == nil {
		t.Fatal("no terminal status update emitted on resume")
	}

	total := usageTotalFrom(t, terminal)
	assertTokenCount(t, total, "promptTokenCount", 30)
	assertTokenCount(t, total, "candidatesTokenCount", 12)
	assertTokenCount(t, total, "totalTokenCount", 42)
}

func TestTurnUsageIsPerExecution(t *testing.T) {
	if got := turnUsageFrom(context.Background()); got != nil {
		t.Fatalf("turnUsageFrom(background) = %#v, want nil", got)
	}

	usage := &turnUsage{}
	ctx := withTurnUsage(context.Background(), usage)
	if got := turnUsageFrom(ctx); got != usage {
		t.Fatalf("turnUsageFrom(ctx) = %#v, want the bound accumulator", got)
	}
}
