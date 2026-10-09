package a2a

import (
	"context"
	"iter"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// scriptedAgent emits one event per response. With a non-nil reported
// channel it then closes it and blocks until the invocation is canceled.
func scriptedAgent(t *testing.T, reported chan struct{}, responses ...model.LLMResponse) adkagent.Agent {
	t.Helper()
	agent, err := adkagent.New(adkagent.Config{
		Name: "usage-agent",
		Run: func(ic adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
			return func(yield func(*adksession.Event, error) bool) {
				for _, response := range responses {
					if !yield(&adksession.Event{
						Author: ic.Agent().Name(), InvocationID: ic.InvocationID(), Branch: ic.Branch(), LLMResponse: response,
					}, nil) {
						return
					}
				}
				if reported != nil {
					close(reported)
					<-ic.Done()
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	return agent
}

func newUsageExecutor(t *testing.T, agent adkagent.Agent) *KAgentExecutor {
	t.Helper()
	executor, err := NewKAgentExecutor(KAgentExecutorConfig{
		AppName: "usage-app", SessionService: adksession.InMemoryService(), Logger: slog.New(slog.DiscardHandler),
		RunnerConfig: runner.Config{AppName: "usage-app", Agent: agent},
	})
	if err != nil {
		t.Fatalf("NewKAgentExecutor() error = %v", err)
	}
	return executor
}

// storedTaskWithUsage is a task whose persisted usage went through a JSON
// round-trip in the task store, hence the float64 counts.
func storedTaskWithUsage(input, output, total float64) *a2atype.Task {
	return &a2atype.Task{
		ID: "task-1", ContextID: "context-1",
		Metadata: map[string]any{UsageExtensionURI: map[string]any{
			"inputTokens": input, "outputTokens": output, "totalTokens": total,
		}},
	}
}

// runUsageAgent runs an agent emitting the given responses through the
// executor and returns the terminal status update.
func runUsageAgent(t *testing.T, storedTask *a2atype.Task, responses ...model.LLMResponse) *a2atype.TaskStatusUpdateEvent {
	t.Helper()
	executor := newUsageExecutor(t, scriptedAgent(t, nil, responses...))
	reqCtx := &a2asrv.ExecutorContext{
		TaskID:     "task-1",
		ContextID:  "context-1",
		Message:    a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
		StoredTask: storedTask,
	}

	var terminal *a2atype.TaskStatusUpdateEvent
	for event, err := range executor.Execute(t.Context(), reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if status, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && (status.Status.State.Terminal() || status.Status.State == a2atype.TaskStateInputRequired) {
			terminal = status
		}
	}
	if terminal == nil {
		t.Fatal("no terminal status update emitted")
	}
	return terminal
}

func usageFrom(t *testing.T, event *a2atype.TaskStatusUpdateEvent) apia2a.Usage {
	t.Helper()
	usage, ok := apia2a.UsageFromMetadata(event.Metadata)
	if !ok {
		t.Fatalf("metadata[%s] = %#v, want a usage payload", UsageExtensionURI, event.Metadata[UsageExtensionURI])
	}
	return usage
}

func assertTotals(t *testing.T, usage apia2a.Usage, input, output, total int64) {
	t.Helper()
	if usage.InputTokens != input || usage.OutputTokens != output || usage.TotalTokens != total {
		t.Fatalf("totals = %+v, want input=%d output=%d total=%d", usage.TokenCounts, input, output, total)
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
	terminal := runUsageAgent(t, nil,
		usageResponse("first", 10, 5, 15),
		usageResponse("second", 20, 7, 27),
	)

	usage := usageFrom(t, terminal)
	assertTotals(t, usage, 30, 12, 42)
	want := []apia2a.ModelUsage{{Model: "gpt-4o-2024-11-20", TokenCounts: usage.TokenCounts}}
	if !slices.Equal(usage.Models, want) {
		t.Fatalf("models = %+v, want %+v", usage.Models, want)
	}
}

func TestTurnUsageSkipsPartialAndEmptyEvents(t *testing.T) {
	partial := usageResponse("par", 100, 100, 200)
	partial.Partial = true
	noUsage := model.LLMResponse{Content: genai.NewContentFromText("no usage", genai.RoleModel)}

	terminal := runUsageAgent(t, nil, partial, noUsage, usageResponse("final", 10, 5, 15))

	total := usageFrom(t, terminal)
	assertTotals(t, total, 10, 5, 15)
}

func TestTurnUsageOmittedWhenNoUsageReported(t *testing.T) {
	terminal := runUsageAgent(t, nil, model.LLMResponse{
		Content: genai.NewContentFromText("no usage", genai.RoleModel),
	})

	if _, ok := terminal.Metadata[UsageExtensionURI]; ok {
		t.Fatalf("metadata[%s] present, want omitted when nothing was reported", UsageExtensionURI)
	}
}

// TestTurnUsagePayloadIsProviderNeutral pins the public wire shape: the
// extension payload uses its own field names, not the genai usage type.
func TestTurnUsagePayloadIsProviderNeutral(t *testing.T) {
	response := usageResponse("only", 10, 5, 20)
	response.UsageMetadata.ThoughtsTokenCount = 3
	response.UsageMetadata.CachedContentTokenCount = 4

	terminal := runUsageAgent(t, nil, response)

	counts := map[string]any{
		"inputTokens": float64(10), "outputTokens": float64(5), "reasoningTokens": float64(3),
		"cachedInputTokens": float64(4), "totalTokens": float64(20),
	}
	model := map[string]any{"model": "gpt-4o-2024-11-20"}
	maps.Copy(model, counts)
	want := map[string]any{"models": []any{model}}
	maps.Copy(want, counts)
	if got := terminal.Metadata[UsageExtensionURI]; !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
}

// TestTurnUsageAttributesCountsPerModel covers a task calling two models: each
// model keeps its own counts instead of the last model naming the whole total.
func TestTurnUsageAttributesCountsPerModel(t *testing.T) {
	other := usageResponse("other model", 20, 7, 27)
	other.ModelVersion = "claude-sonnet-4"
	unnamed := usageResponse("unnamed model", 1, 1, 2)
	unnamed.ModelVersion = ""

	terminal := runUsageAgent(t, nil, usageResponse("first", 10, 5, 15), other, unnamed)

	usage := usageFrom(t, terminal)
	assertTotals(t, usage, 31, 13, 44)
	want := []apia2a.ModelUsage{
		{Model: "claude-sonnet-4", TokenCounts: apia2a.TokenCounts{InputTokens: 20, OutputTokens: 7, TotalTokens: 27}},
		{Model: "gpt-4o-2024-11-20", TokenCounts: apia2a.TokenCounts{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}},
	}
	if !slices.Equal(usage.Models, want) {
		t.Fatalf("models = %+v, want %+v", usage.Models, want)
	}
}

func TestTurnUsageSeedFromTaskAccumulatesAcrossExecutions(t *testing.T) {
	storedTask := &a2atype.Task{
		ID:        "task-1",
		ContextID: "context-1",
		Metadata: map[string]any{
			// float64 as it would be after a JSON round-trip through a task store.
			UsageExtensionURI: map[string]any{
				"inputTokens": float64(100), "outputTokens": float64(50), "totalTokens": float64(150),
				"models": []any{map[string]any{
					"model": "gpt-4o-2024-11-20", "inputTokens": float64(60), "outputTokens": float64(30), "totalTokens": float64(90),
				}, map[string]any{
					"model": "gpt-4o-2024-08-06", "inputTokens": float64(40), "outputTokens": float64(20), "totalTokens": float64(60),
				}},
			},
		},
	}

	terminal := runUsageAgent(t, storedTask, usageResponse("resumed", 10, 5, 15))

	usage := usageFrom(t, terminal)
	assertTotals(t, usage, 110, 55, 165)
	want := []apia2a.ModelUsage{
		{Model: "gpt-4o-2024-08-06", TokenCounts: apia2a.TokenCounts{InputTokens: 40, OutputTokens: 20, TotalTokens: 60}},
		{Model: "gpt-4o-2024-11-20", TokenCounts: apia2a.TokenCounts{InputTokens: 70, OutputTokens: 35, TotalTokens: 105}},
	}
	if !slices.Equal(usage.Models, want) {
		t.Fatalf("models = %+v, want %+v", usage.Models, want)
	}
}

func TestTurnUsageSeedFromTaskIgnoresMissingOrMalformed(t *testing.T) {
	tests := map[string]*a2atype.Task{
		"nil task":          nil,
		"no metadata":       {ID: "task-1"},
		"unrelated keys":    {ID: "task-1", Metadata: map[string]any{"other": 1}},
		"usage not a map":   {ID: "task-1", Metadata: map[string]any{UsageExtensionURI: "nope"}},
		"counts not number": {ID: "task-1", Metadata: map[string]any{UsageExtensionURI: map[string]any{"inputTokens": "many"}}},
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
	executor := newUsageExecutor(t, agent)

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
	paused := usageFrom(t, pause)
	assertTotals(t, paused, 10, 5, 15)

	// a2a-go merges terminal status update metadata into the stored task.
	stored := &a2atype.Task{ID: taskID, ContextID: contextID, Status: pause.Status, Metadata: pause.Metadata}
	decision := hitlDecisionMessage(&apia2a.ToolApprovalResponse{
		Type:      HITLTypeToolApprovalResponse,
		Approvals: []apia2a.ToolApproval{{ID: "confirmation-call", Approved: true}},
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

	total := usageFrom(t, terminal)
	assertTotals(t, total, 30, 12, 42)
}

// TestTurnUsageDerivesTotalPerCall covers a task mixing calls that report a
// total with calls that do not: the derived counts of the second call must
// extend the total reported by the first.
func TestTurnUsageDerivesTotalPerCall(t *testing.T) {
	terminal := runUsageAgent(t, nil,
		usageResponse("with total", 10, 5, 15),
		usageResponse("without total", 20, 7, 0),
	)

	total := usageFrom(t, terminal)
	assertTotals(t, total, 30, 12, 42)
}

// TestTurnUsageDerivedTotalGrowsAcrossExecutions covers a resumed task whose
// persisted total was itself derived: the next execution must add its own
// derived total instead of keeping the stored one.
func TestTurnUsageDerivedTotalGrowsAcrossExecutions(t *testing.T) {
	terminal := runUsageAgent(t, storedTaskWithUsage(100, 20, 120), usageResponse("no total", 200, 30, 0))

	total := usageFrom(t, terminal)
	assertTotals(t, total, 300, 50, 350)
}

func TestUsageActivationInterceptor(t *testing.T) {
	tests := map[string]struct {
		requested []string
		want      bool
	}{
		"requested":     {requested: []string{HITLExtensionURI, UsageExtensionURI}, want: true},
		"joined value":  {requested: []string{HITLExtensionURI + ", " + UsageExtensionURI}, want: true},
		"not requested": {requested: []string{HITLExtensionURI}, want: false},
		"other version": {requested: []string{"https://kagent.dev/extensions/usage/v2"}, want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, callCtx := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{
				a2atype.SvcParamExtensions: tt.requested,
			}))
			if _, _, err := UsageActivationInterceptor().Before(ctx, callCtx, &a2asrv.Request{}); err != nil {
				t.Fatalf("Before() error = %v", err)
			}
			extensions, ok := a2asrv.ExtensionsFrom(ctx)
			if got := ok && extensions.Active(&usageAgentExtension); got != tt.want {
				t.Fatalf("active = %v, want %v", got, tt.want)
			}
		})
	}
}

func cancelledStatus(t *testing.T, executor *KAgentExecutor, reqCtx *a2asrv.ExecutorContext) *a2atype.TaskStatusUpdateEvent {
	t.Helper()
	for event, err := range executor.Cancel(t.Context(), reqCtx) {
		if err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		if status, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && status.Status.State == a2atype.TaskStateCanceled {
			return status
		}
	}
	t.Fatal("no canceled status update emitted")
	return nil
}

// TestTurnUsageReportedOnCancelOfRunningExecution covers a cancel request that
// interrupts an execution: a2a-go makes the canceled status the task's final
// event and discards the execution's own, so the canceled status must carry
// the calls completed before the cancel.
func TestTurnUsageReportedOnCancelOfRunningExecution(t *testing.T) {
	counted := make(chan struct{})
	executor := newUsageExecutor(t, scriptedAgent(t, counted, usageResponse("first", 10, 5, 15), usageResponse("second", 20, 7, 27)))
	stored := storedTaskWithUsage(100, 20, 120)
	executionCtx, stopExecution := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range executor.Execute(executionCtx, &a2asrv.ExecutorContext{
			TaskID: "task-1", ContextID: "context-1", StoredTask: stored,
			Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
		}) {
		}
	}()
	t.Cleanup(func() {
		stopExecution()
		<-done
	})
	<-counted

	status := cancelledStatus(t, executor, &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "context-1", StoredTask: stored})

	usage := usageFrom(t, status)
	assertTotals(t, usage, 130, 32, 162)
	want := []apia2a.ModelUsage{{Model: "gpt-4o-2024-11-20", TokenCounts: apia2a.TokenCounts{InputTokens: 30, OutputTokens: 12, TotalTokens: 42}}}
	if !slices.Equal(usage.Models, want) {
		t.Fatalf("models = %+v, want %+v", usage.Models, want)
	}
}

// TestTurnUsageReportedOnCancelOfParkedTask covers a cancel request for a task
// with no running execution, such as one waiting for input: the canceled
// status repeats the persisted total so it stays the task's latest value.
func TestTurnUsageReportedOnCancelOfParkedTask(t *testing.T) {
	executor := newUsageExecutor(t, scriptedAgent(t, nil))
	stored := storedTaskWithUsage(100, 20, 120)
	stored.Status.State = a2atype.TaskStateInputRequired

	status := cancelledStatus(t, executor, &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "context-1", StoredTask: stored})

	assertTotals(t, usageFrom(t, status), 100, 20, 120)
}

func TestRunningUsageTracksOnlyTheLatestExecution(t *testing.T) {
	var running runningUsage
	first, second := &turnUsage{}, &turnUsage{}

	untrackFirst := running.track("task-1", first)
	untrackSecond := running.track("task-1", second)
	untrackFirst()
	if got := running.of("task-1"); got != second {
		t.Fatalf("of(task-1) = %p, want the latest execution %p", got, second)
	}
	untrackSecond()
	if got := running.of("task-1"); got != nil {
		t.Fatalf("of(task-1) = %p, want nil after the execution ends", got)
	}
}
