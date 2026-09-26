package e2e_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/stretchr/testify/require"
)

// Completion must reach durable storage with no public stream left attached.
// A reconnect reads the completed task after native work and cleanup finish.
func TestAgentInstanceCompletesAfterDisconnect(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		started, release := make(chan struct{}), make(chan struct{})
		var startedOnce, releaseOnce sync.Once
		upstream := startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json")
		recorder := startModelRecorder(t, upstream, func([]byte) error {
			startedOnce.Do(func() { close(started) })
			select {
			case <-release:
				return nil
			case <-t.Context().Done():
				return t.Context().Err()
			}
		})
		t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
		fixture := newInteractionFixture(t, harness, interactionTarget(t), reachableModelURL(t, recorder.URL))
		_, request := newMessageRequest(t, "What is 2+2?")
		ctx, disconnect := context.WithCancel(fixture.ctx)
		defer disconnect()
		start := time.Now()
		stream, err := fixture.client.SendStreamingMessage(ctx, request)
		require.NoError(t, err)
		first, err := stream.Recv()
		require.NoError(t, err)
		firstEventLatency := time.Since(start)
		event, err := pbconv.FromProtoStreamResponse(first)
		require.NoError(t, err)
		taskID := string(event.TaskInfo().TaskID)
		require.NotEmpty(t, taskID)
		select {
		case <-started:
		case <-time.After(time.Minute):
			t.Fatal("runtime did not call the model")
		}
		disconnect()
		for err == nil {
			_, err = stream.Recv()
		}
		nativeStart := time.Now()
		releaseOnce.Do(func() { close(release) })
		require.Eventually(t, func() bool {
			task, err := fixture.client.GetTask(fixture.ctx, &a2apb.GetTaskRequest{Id: taskID})
			return err == nil && task.GetStatus().GetState() == a2apb.TaskState_TASK_STATE_COMPLETED
		}, time.Minute, 100*time.Millisecond, "disconnected task did not finish and publish")
		t.Logf("first event including actor resume: %s; model release through persisted completion: %s", firstEventLatency, time.Since(nativeStart))
		reconnected, err := fixture.client.SubscribeToTask(fixture.ctx, &a2apb.SubscribeToTaskRequest{Id: taskID})
		require.NoError(t, err)
		waitForTaskState(t, reconnected, a2atype.TaskStateCompleted)
		assertTaskStreamClosed(t, reconnected)
		require.Contains(t, taskText(getTask(t, fixture, a2atype.TaskID(taskID))), "The answer is 4.")
	})
}

func TestAgentInstanceStreamingResumeAndPersistence(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))

		streamed := sendStreaming(t, fixture, "What is 2+2?")
		if streamed.state != a2atype.TaskStateCompleted {
			t.Fatalf("streamed mock task state = %s, failure = %q, want COMPLETED", streamed.state, streamed.failureText)
		}
		if !streamed.sawWorking || !streamed.sawArtifact {
			t.Fatalf("streamed mock events: working=%t artifact=%t, want both", streamed.sawWorking, streamed.sawArtifact)
		}
		if !strings.Contains(streamed.text, "The answer is 4.") {
			t.Fatalf("streamed mock response = %q, want The answer is 4.", streamed.text)
		}
		first := getTask(t, fixture, streamed.taskID)
		if first.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(first), "The answer is 4.") {
			t.Fatalf("persisted first task state = %s, text = %q", first.Status.State, taskText(first))
		}

		_, _, resumed := fixture.send(t, "What is 3+3?")
		if resumed.Status.State != a2atype.TaskStateCompleted {
			t.Fatalf("resumed mock task state = %s, text = %q, want COMPLETED", resumed.Status.State, taskText(resumed))
		}
		if text := taskText(resumed); !strings.Contains(text, "The answer is 6.") {
			t.Fatalf("resumed mock response = %q, want The answer is 6.", text)
		}
		assertTaskHistory(t, fixture, first.ID, resumed.ID)
	})
}

type streamResult struct {
	taskID      a2atype.TaskID
	state       a2atype.TaskState
	text        string
	sawWorking  bool
	sawArtifact bool
	toolEvents  []toolEvent
	failureText string
}

type toolEvent struct {
	partType string
	id       string
	name     string
}

func sendStreaming(t *testing.T, fixture *interactionFixture, text string) streamResult {
	t.Helper()
	_, request := newMessageRequest(t, text)
	stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
	if err != nil {
		t.Fatalf("start streaming A2A message: %v", err)
	}
	var result streamResult
	var output strings.Builder
	terminalEvents := 0
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if terminalEvents != 1 {
				t.Fatalf("stream terminal event count = %d, want 1", terminalEvents)
			}
			if result.taskID == "" {
				t.Fatal("stream completed without a task ID")
			}
			result.text = output.String()
			return result
		}
		if err != nil {
			t.Fatalf("receive task stream: %v", err)
		}
		if terminalEvents != 0 {
			t.Fatalf("stream emitted an event after terminal state %s", result.state)
		}
		event, err := pbconv.FromProtoStreamResponse(response)
		if err != nil {
			t.Fatalf("decode task stream: %v", err)
		}
		if info := event.TaskInfo(); info.TaskID != "" {
			result.taskID = info.TaskID
		}
		switch event := event.(type) {
		case *a2atype.Task:
			result.state = event.Status.State
			if event.Status.State == a2atype.TaskStateWorking {
				result.sawWorking = true
			}
		case *a2atype.TaskArtifactUpdateEvent:
			result.sawArtifact = true
			if event.Artifact != nil {
				result.toolEvents = append(result.toolEvents, toolEvents(event.Artifact.Parts)...)
				for _, part := range event.Artifact.Parts {
					output.WriteString(part.Text())
				}
			}
		case *a2atype.TaskStatusUpdateEvent:
			result.state = event.Status.State
			if event.Status.State == a2atype.TaskStateWorking {
				result.sawWorking = true
			}
			if event.Status.State == a2atype.TaskStateFailed && event.Status.Message != nil {
				var parts []string
				for _, part := range event.Status.Message.Parts {
					parts = append(parts, part.Text())
				}
				result.failureText = strings.Join(parts, "\n")
			}
			if event.Status.Message != nil {
				result.toolEvents = append(result.toolEvents, toolEvents(event.Status.Message.Parts)...)
			}
		}
		if result.state.Terminal() {
			terminalEvents++
		}
	}
}

func toolEvents(parts []*a2atype.Part) []toolEvent {
	var events []toolEvent
	for _, part := range parts {
		partType, _ := part.Metadata[apia2a.PartTypeMetadataKey].(string)
		if partType != "function_call" && partType != "function_response" {
			continue
		}
		data, ok := part.Data().(map[string]any)
		if !ok {
			continue
		}
		id, _ := data["id"].(string)
		name, _ := data["name"].(string)
		events = append(events, toolEvent{partType: partType, id: id, name: name})
	}
	return events
}

func taskToolEvents(task *a2atype.Task) []toolEvent {
	var events []toolEvent
	for _, message := range task.History {
		if message != nil {
			events = append(events, toolEvents(message.Parts)...)
		}
	}
	if task.Status.Message != nil {
		events = append(events, toolEvents(task.Status.Message.Parts)...)
	}
	for _, artifact := range task.Artifacts {
		if artifact != nil {
			events = append(events, toolEvents(artifact.Parts)...)
		}
	}
	return events
}

func assertToolEvents(t *testing.T, events []toolEvent, toolNames ...string) {
	t.Helper()
	for _, toolName := range toolNames {
		calls, responses := 0, 0
		ids := map[string]struct{}{}
		for _, event := range events {
			if event.name != toolName {
				continue
			}
			if event.id == "" {
				t.Fatalf("%s event for %s has no tool-use ID", event.partType, toolName)
			}
			switch event.partType {
			case "function_call":
				calls++
				ids[event.id] = struct{}{}
			case "function_response":
				responses++
				if _, ok := ids[event.id]; !ok {
					t.Fatalf("response for %s tool-use ID %q has no preceding call", toolName, event.id)
				}
			}
		}
		if calls != 1 || responses != 1 {
			t.Fatalf("A2A events for %s: calls=%d responses=%d, want one of each; all events=%#v", toolName, calls, responses, events)
		}
	}
}

func getTask(t *testing.T, fixture *interactionFixture, taskID a2atype.TaskID) *a2atype.Task {
	t.Helper()
	request, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: taskID})
	if err != nil {
		t.Fatalf("build GetTask request: %v", err)
	}
	response, err := fixture.client.GetTask(fixture.ctx, request)
	if err != nil {
		t.Fatalf("get task %s: %v", taskID, err)
	}
	task, err := pbconv.FromProtoTask(response)
	if err != nil {
		t.Fatalf("decode task %s: %v", taskID, err)
	}
	return task
}

func assertTaskHistory(t *testing.T, fixture *interactionFixture, taskIDs ...a2atype.TaskID) {
	t.Helper()
	request, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{ContextID: fixture.contextID})
	if err != nil {
		t.Fatalf("build ListTasks request: %v", err)
	}
	response, err := fixture.client.ListTasks(fixture.ctx, request)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(response)
	if err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	if len(listed.Tasks) != len(taskIDs) {
		t.Fatalf("task count = %d, want %d", len(listed.Tasks), len(taskIDs))
	}
	want := make(map[a2atype.TaskID]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		want[taskID] = struct{}{}
	}
	for _, task := range listed.Tasks {
		if _, ok := want[task.ID]; !ok {
			t.Fatalf("listed unexpected task %s", task.ID)
		}
		if task.Status.State != a2atype.TaskStateCompleted {
			t.Fatalf("listed task %s state = %s, want COMPLETED", task.ID, task.Status.State)
		}
	}
}
