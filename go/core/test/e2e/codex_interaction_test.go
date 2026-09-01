package e2e_test

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const codexE2EHarness = "codex-e2e"

//go:embed mocks/invoke_codex_agent.json mocks/invoke_codex_builtin_tools.json mocks/invoke_codex_resources.json
var codexInteractionMocks embed.FS

func TestE2ECodexMockInteractionResumeAndPersistence(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableModelURL(t, startMockLLMServer(t, codexInteractionMocks, "mocks/invoke_codex_agent.json"))
	template := createCodexMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, codexE2EHarness, template)

	streamed := sendCodexStreaming(t, fixture, "Return exactly CODEX_MOCK_FIRST.")
	if streamed.state != a2atype.TaskStateCompleted {
		t.Fatalf("streamed mock Codex task state = %s, failure = %q, want COMPLETED", streamed.state, streamed.failureText)
	}
	if !streamed.sawWorking || !streamed.sawArtifact {
		t.Fatalf("streamed mock Codex events: working=%t artifact=%t, want both", streamed.sawWorking, streamed.sawArtifact)
	}
	if !strings.Contains(streamed.text, "CODEX_MOCK_FIRST") {
		t.Fatalf("streamed mock Codex response = %q, want CODEX_MOCK_FIRST", streamed.text)
	}
	first := getCodexTask(t, fixture, streamed.taskID)
	if first.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(first), "CODEX_MOCK_FIRST") {
		t.Fatalf("persisted first Codex task state = %s, text = %q", first.Status.State, taskText(first))
	}

	_, _, resumed := fixture.send(t, "Return exactly CODEX_MOCK_SECOND.")
	if resumed.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("resumed mock Codex task state = %s, text = %q, want COMPLETED", resumed.Status.State, taskText(resumed))
	}
	if text := taskText(resumed); !strings.Contains(text, "CODEX_MOCK_SECOND") {
		t.Fatalf("resumed mock Codex response = %q, want CODEX_MOCK_SECOND", text)
	}
	assertCodexTaskHistory(t, fixture, first.ID, resumed.ID)
}

func TestE2ECodexMockCheckpointForkAndResume(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableModelURL(t, startMockLLMServer(t, codexInteractionMocks, "mocks/invoke_codex_agent.json"))
	template := createCodexMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, codexE2EHarness, template)

	_, _, first := fixture.send(t, "Return exactly CODEX_MOCK_FIRST.")
	if first.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("initial mock Codex task state = %s, want COMPLETED", first.Status.State)
	}

	created, err := fixture.checkpoints.CreateCheckpoint(fixture.ctx, &apiv1alpha1.CreateCheckpointRequest{
		Namespace: "kagent", AgentInstanceId: fixture.instanceID, RequestId: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create Codex checkpoint: %v", err)
	}
	checkpointID := created.GetCheckpoint().GetId()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, cleanupErr := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1alpha1.DeleteCheckpointRequest{
			Namespace: "kagent", CheckpointId: checkpointID,
		})
		if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
			t.Errorf("delete Codex checkpoint: %v", cleanupErr)
		}
	})

	forked, err := fixture.checkpoints.ForkAgentInstance(fixture.ctx, &apiv1alpha1.ForkAgentInstanceRequest{
		Namespace: "kagent", CheckpointId: checkpointID, RequestId: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("fork Codex AgentInstance: %v", err)
	}
	fork := forked.GetAgentInstance()
	if fork.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		t.Fatalf("forked Codex AgentInstance state = %s, want READY", fork.GetState())
	}
	forkID := fork.GetId()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, cleanupErr := fixture.instances.DeleteAgentInstance(ctx, &apiv1alpha1.DeleteAgentInstanceRequest{
			Namespace: "kagent", AgentInstanceId: forkID,
		})
		if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
			t.Errorf("delete forked Codex AgentInstance: %v", cleanupErr)
		}
	})

	forkCtx, forkCancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(),
		"x-user-id", "e2e",
		"x-kagent-agent-instance-namespace", "kagent",
		"x-kagent-agent-instance-id", forkID,
	), 4*time.Minute)
	t.Cleanup(forkCancel)
	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{ContextID: forkID, PageSize: 10})
	if err != nil {
		t.Fatalf("build forked Codex task list request: %v", err)
	}
	listedResponse, err := fixture.client.ListTasks(forkCtx, listRequest)
	if err != nil {
		t.Fatalf("list forked Codex tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(listedResponse)
	if err != nil || len(listed.Tasks) != 1 || listed.Tasks[0].ContextID != forkID || listed.Tasks[0].Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("forked Codex tasks = %+v, error %v; want one copied task in context %s", listed, err, forkID)
	}

	forkFixture := &interactionFixture{ctx: forkCtx, client: fixture.client, instanceID: forkID}
	_, _, resumed := forkFixture.send(t, "Return exactly CODEX_MOCK_SECOND.")
	if resumed.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(resumed), "CODEX_MOCK_SECOND") {
		t.Fatalf("forked Codex task state = %s, text = %q, want completed resumed response", resumed.Status.State, taskText(resumed))
	}
}

func TestE2ECodexMockBuiltinToolEvents(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableModelURL(t, startMockLLMServer(t, codexInteractionMocks, "mocks/invoke_codex_builtin_tools.json"))
	template := createCodexMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, codexE2EHarness, template)

	streamed := sendCodexStreaming(t, fixture, "Run the requested shell command.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "CODEX_BUILTIN_TOOL_DONE") {
		t.Fatalf("built-in tool task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	assertCodexToolEvents(t, streamed.toolEvents, "command_execution")
	assertCodexToolEvents(t, codexTaskToolEvents(getCodexTask(t, fixture, streamed.taskID)), "command_execution")
}

func TestE2ECodexMockWholeServerMCP(t *testing.T) {
	target := interactionTarget(t)
	mcpURL, mcpMock := startMCPMock(t)

	kube := interactionKubeClient(t)
	mcpServer := createCodexMCPServer(t, kube, mcpURL)
	modelToolNamespace := codexMCPToolNamespace(mcpServer.Name)
	modelURL := startCodexResourceMockLLM(t, modelToolNamespace)
	model := createCodexMockModel(t, kube, modelURL)
	template := createCodexMCPTemplate(t, kube, model.Name, mcpServer.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, codexE2EHarness, template)

	streamed := sendCodexStreaming(t, fixture, "Add 3 and 5 using the configured MCP server.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "CODEX_MCP_DONE result is 8") {
		t.Fatalf("whole-server MCP task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	sawToolCall := false
	for _, request := range mcpMock.Requests() {
		if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) && bytes.Contains(request.Body, []byte(`"name":"add_numbers"`)) {
			sawToolCall = true
			break
		}
	}
	if !sawToolCall {
		t.Fatal("mock MCP server did not receive an add_numbers tool call")
	}
	toolName := mcpServer.Name + ".add_numbers"
	assertCodexToolEvents(t, streamed.toolEvents, toolName)
	assertCodexToolEvents(t, codexTaskToolEvents(getCodexTask(t, fixture, streamed.taskID)), toolName)
}

type codexStreamResult struct {
	taskID      a2atype.TaskID
	state       a2atype.TaskState
	text        string
	sawWorking  bool
	sawArtifact bool
	toolEvents  []codexToolEvent
	failureText string
}

type codexToolEvent struct {
	partType string
	id       string
	name     string
}

func sendCodexStreaming(t *testing.T, fixture *interactionFixture, text string) codexStreamResult {
	t.Helper()
	_, request := newMessageRequest(t, text)
	stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
	if err != nil {
		t.Fatalf("start streaming Codex A2A message: %v", err)
	}
	var result codexStreamResult
	var output strings.Builder
	terminalEvents := 0
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if terminalEvents != 1 {
				t.Fatalf("Codex stream terminal event count = %d, want 1", terminalEvents)
			}
			if result.taskID == "" {
				t.Fatal("Codex stream completed without a task ID")
			}
			result.text = output.String()
			return result
		}
		if err != nil {
			t.Fatalf("receive Codex task stream: %v", err)
		}
		if terminalEvents != 0 {
			t.Fatalf("Codex stream emitted an event after terminal state %s", result.state)
		}
		event, err := pbconv.FromProtoStreamResponse(response)
		if err != nil {
			t.Fatalf("decode Codex task stream: %v", err)
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
				result.toolEvents = append(result.toolEvents, codexToolEvents(event.Artifact.Parts)...)
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
				result.toolEvents = append(result.toolEvents, codexToolEvents(event.Status.Message.Parts)...)
			}
		}
		if result.state.Terminal() {
			terminalEvents++
		}
	}
}

func codexToolEvents(parts []*a2atype.Part) []codexToolEvent {
	var events []codexToolEvent
	for _, part := range parts {
		partType, _ := part.Metadata["kagent_type"].(string)
		if partType != "function_call" && partType != "function_response" {
			continue
		}
		data, ok := part.Data().(map[string]any)
		if !ok {
			continue
		}
		id, _ := data["id"].(string)
		name, _ := data["name"].(string)
		events = append(events, codexToolEvent{partType: partType, id: id, name: name})
	}
	return events
}

func codexTaskToolEvents(task *a2atype.Task) []codexToolEvent {
	var events []codexToolEvent
	for _, message := range task.History {
		if message != nil {
			events = append(events, codexToolEvents(message.Parts)...)
		}
	}
	if task.Status.Message != nil {
		events = append(events, codexToolEvents(task.Status.Message.Parts)...)
	}
	for _, artifact := range task.Artifacts {
		if artifact != nil {
			events = append(events, codexToolEvents(artifact.Parts)...)
		}
	}
	return events
}

func assertCodexToolEvents(t *testing.T, events []codexToolEvent, toolName string) {
	t.Helper()
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

func getCodexTask(t *testing.T, fixture *interactionFixture, taskID a2atype.TaskID) *a2atype.Task {
	t.Helper()
	request, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: taskID})
	if err != nil {
		t.Fatalf("build GetTask request: %v", err)
	}
	response, err := fixture.client.GetTask(fixture.ctx, request)
	if err != nil {
		t.Fatalf("get Codex task %s: %v", taskID, err)
	}
	task, err := pbconv.FromProtoTask(response)
	if err != nil {
		t.Fatalf("decode Codex task %s: %v", taskID, err)
	}
	return task
}

func assertCodexTaskHistory(t *testing.T, fixture *interactionFixture, taskIDs ...a2atype.TaskID) {
	t.Helper()
	request, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{ContextID: fixture.instanceID})
	if err != nil {
		t.Fatalf("build ListTasks request: %v", err)
	}
	response, err := fixture.client.ListTasks(fixture.ctx, request)
	if err != nil {
		t.Fatalf("list Codex tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(response)
	if err != nil {
		t.Fatalf("decode Codex task list: %v", err)
	}
	if len(listed.Tasks) != len(taskIDs) {
		t.Fatalf("Codex task count = %d, want %d", len(listed.Tasks), len(taskIDs))
	}
	want := make(map[a2atype.TaskID]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		want[taskID] = struct{}{}
	}
	for _, task := range listed.Tasks {
		if _, ok := want[task.ID]; !ok {
			t.Fatalf("listed unexpected Codex task %s", task.ID)
		}
		if task.Status.State != a2atype.TaskStateCompleted {
			t.Fatalf("listed Codex task %s state = %s, want COMPLETED", task.ID, task.Status.State)
		}
	}
}

func createCodexMockTemplate(t *testing.T, baseURL string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	model := createCodexMockModel(t, kube, baseURL)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "codex-interaction-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "codex"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  v1alpha3.AgentTemplateLocalReference{Name: model.Name},
			Description:  "Codex mockLLM interaction fixture",
			SystemPrompt: "Reply concisely and follow the requested output format exactly.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, codexE2EHarness)
	return template.Name
}

func createCodexMockModel(t *testing.T, kube ctrlclient.Client, baseURL string) *v1alpha3.ModelConfig {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "codex-mock-", Namespace: "kagent"},
		Data:       map[string][]byte{"OPENAI_API_KEY": []byte("mock-key")},
	}
	if err := kube.Create(t.Context(), secret); err != nil {
		t.Fatalf("create Codex mock Secret: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), secret); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Codex mock Secret: %v", err)
		}
	})
	responses := v1alpha3.OpenAIAPIFormatResponses
	model := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "codex-mock-", Namespace: "kagent"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider:     v1alpha3.ModelProviderOpenAI,
			Model:        "gpt-5.2-codex",
			APIKeySecret: secret.Name, APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI: &v1alpha3.OpenAIConfig{BaseURL: baseURL, APIFormat: &responses},
		},
	}
	if err := kube.Create(t.Context(), model); err != nil {
		t.Fatalf("create Codex mock ModelConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), model); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Codex mock ModelConfig: %v", err)
		}
	})
	return model
}

func createCodexMCPServer(t *testing.T, kube ctrlclient.Client, mcpURL string) *v1alpha3.RemoteMCPServer {
	t.Helper()
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "codex-resources-", Namespace: "kagent"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Description: "Codex whole-server MCP E2E fixture",
			Protocol:    v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:         mcpURL,
		},
	}
	if err := kube.Create(t.Context(), server); err != nil {
		t.Fatalf("create Codex RemoteMCPServer: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), server); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Codex RemoteMCPServer: %v", err)
		}
	})
	return server
}

func createCodexMCPTemplate(t *testing.T, kube ctrlclient.Client, modelConfig, mcpServer string) string {
	t.Helper()
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "codex-resources-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "codex"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  v1alpha3.AgentTemplateLocalReference{Name: modelConfig},
			Description:  "Codex direct whole-server MCP E2E fixture",
			SystemPrompt: "Use the configured MCP tool. Do not calculate the answer yourself.",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: v1alpha3.AgentTemplateTypedLocalReference{Kind: "RemoteMCPServer", Name: mcpServer},
			}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, codexE2EHarness)
	return template.Name
}

func startCodexResourceMockLLM(t *testing.T, toolNamespace string) string {
	t.Helper()
	raw, err := codexInteractionMocks.ReadFile("mocks/invoke_codex_resources.json")
	if err != nil {
		t.Fatalf("read Codex resource mock fixture: %v", err)
	}
	raw = bytes.ReplaceAll(raw, []byte("MCP_TOOL_NAMESPACE"), []byte(toolNamespace))
	raw = bytes.ReplaceAll(raw, []byte("MCP_TOOL_SEARCH_QUERY"), []byte(toolNamespace+" add_numbers"))
	var cfg mockllm.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode Codex Responses mock fixture: %v", err)
	}
	return reachableModelURL(t, startMockLLMConfig(t, cfg))
}

func codexMCPToolNamespace(serverName string) string {
	return "mcp__" + strings.ReplaceAll(serverName, "-", "_")
}
