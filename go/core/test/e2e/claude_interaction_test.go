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

const claudeE2EHarness = "claude-e2e"

//go:embed mocks/invoke_claude_agent.json mocks/invoke_claude_builtin_tools.json mocks/invoke_claude_local_subagent.json mocks/invoke_claude_resources.json
var claudeInteractionMocks embed.FS

func TestE2EClaudeMockInteractionResumeAndPersistence(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableServerURL(t, startMockLLMServer(t, claudeInteractionMocks, "mocks/invoke_claude_agent.json"), "")
	template := createClaudeMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, template)

	streamed := sendClaudeStreaming(t, fixture, "Return exactly CLAUDE_MOCK_FIRST.")
	if streamed.state != a2atype.TaskStateCompleted {
		t.Fatalf("streamed mock Claude task state = %s, failure = %q, want COMPLETED", streamed.state, streamed.failureText)
	}
	if !streamed.sawWorking || !streamed.sawArtifact {
		t.Fatalf("streamed mock Claude events: working=%t artifact=%t, want both", streamed.sawWorking, streamed.sawArtifact)
	}
	if !strings.Contains(streamed.text, "CLAUDE_MOCK_FIRST") {
		t.Fatalf("streamed mock Claude response = %q, want CLAUDE_MOCK_FIRST", streamed.text)
	}
	first := getClaudeTask(t, fixture, streamed.taskID)
	if first.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(first), "CLAUDE_MOCK_FIRST") {
		t.Fatalf("persisted first Claude task state = %s, text = %q", first.Status.State, taskText(first))
	}

	_, _, resumed := fixture.send(t, "Return exactly CLAUDE_MOCK_SECOND.")
	if resumed.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("resumed mock Claude task state = %s, text = %q, want COMPLETED", resumed.Status.State, taskText(resumed))
	}
	if text := taskText(resumed); !strings.Contains(text, "CLAUDE_MOCK_SECOND") {
		t.Fatalf("resumed mock Claude response = %q, want CLAUDE_MOCK_SECOND", text)
	}
	assertClaudeTaskHistory(t, fixture, first.ID, resumed.ID)
}

func TestE2EClaudeMockCheckpointForkAndResume(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableServerURL(t, startMockLLMServer(t, claudeInteractionMocks, "mocks/invoke_claude_agent.json"), "")
	template := createClaudeMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, template)

	_, _, first := fixture.send(t, "Return exactly CLAUDE_MOCK_FIRST.")
	if first.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("initial mock Claude task state = %s, want COMPLETED", first.Status.State)
	}

	created, err := fixture.checkpoints.CreateCheckpoint(fixture.ctx, &apiv1alpha1.CreateCheckpointRequest{
		Namespace: "kagent", AgentInstanceId: fixture.instanceID, RequestId: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create Claude checkpoint: %v", err)
	}
	checkpointID := created.GetCheckpoint().GetId()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, cleanupErr := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1alpha1.DeleteCheckpointRequest{
			Namespace: "kagent", CheckpointId: checkpointID,
		})
		if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
			t.Errorf("delete Claude checkpoint: %v", cleanupErr)
		}
	})

	forked, err := fixture.checkpoints.ForkAgentInstance(fixture.ctx, &apiv1alpha1.ForkAgentInstanceRequest{
		Namespace: "kagent", CheckpointId: checkpointID, RequestId: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("fork Claude AgentInstance: %v", err)
	}
	fork := forked.GetAgentInstance()
	if fork.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		t.Fatalf("forked Claude AgentInstance state = %s, want READY", fork.GetState())
	}
	forkID := fork.GetId()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, cleanupErr := fixture.instances.DeleteAgentInstance(ctx, &apiv1alpha1.DeleteAgentInstanceRequest{
			Namespace: "kagent", AgentInstanceId: forkID,
		})
		if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
			t.Errorf("delete forked Claude AgentInstance: %v", cleanupErr)
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
		t.Fatalf("build forked Claude task list request: %v", err)
	}
	listedResponse, err := fixture.client.ListTasks(forkCtx, listRequest)
	if err != nil {
		t.Fatalf("list forked Claude tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(listedResponse)
	if err != nil || len(listed.Tasks) != 1 || listed.Tasks[0].ContextID != forkID || listed.Tasks[0].Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("forked Claude tasks = %+v, error %v; want one copied task in context %s", listed, err, forkID)
	}

	forkFixture := &interactionFixture{ctx: forkCtx, client: fixture.client, instanceID: forkID}
	_, _, resumed := forkFixture.send(t, "Return exactly CLAUDE_MOCK_SECOND.")
	if resumed.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(resumed), "CLAUDE_MOCK_SECOND") {
		t.Fatalf("forked Claude task state = %s, text = %q, want completed resumed response", resumed.Status.State, taskText(resumed))
	}
}

func TestE2EClaudeMockActiveTaskCancellation(t *testing.T) {
	target := interactionTarget(t)
	modelURL, started := startBlockingClaudeMock(t)
	template := createClaudeMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, template)
	testActiveTaskCancellation(t, fixture, started)
}

func TestE2EClaudeMockBuiltinToolEvents(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableServerURL(t, startMockLLMServer(t, claudeInteractionMocks, "mocks/invoke_claude_builtin_tools.json"), "")
	template := createClaudeMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, template)

	streamed := sendClaudeStreaming(t, fixture, "Create the requested file and read it back.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "CLAUDE_BUILTIN_TOOLS_DONE") {
		t.Fatalf("built-in tool task state = %s, text = %q", streamed.state, streamed.text)
	}
	assertClaudeToolEvents(t, streamed.toolEvents, "Bash", "Read")
	persisted := getClaudeTask(t, fixture, streamed.taskID)
	assertClaudeToolEvents(t, claudeTaskToolEvents(persisted), "Bash", "Read")
}

func TestE2EClaudeMockLocalSubagentRouting(t *testing.T) {
	target := interactionTarget(t)
	modelURL := reachableServerURL(t, startMockLLMServer(t, claudeInteractionMocks, "mocks/invoke_claude_local_subagent.json"), "")
	kube := interactionKubeClient(t)
	model := createClaudeMockModel(t, kube, modelURL)
	rootTemplate, childTemplate := createClaudeLocalAgentTemplates(t, kube, model, "CLAUDE_LOCAL_SPECIALIST_INSTRUCTION")
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, rootTemplate)

	streamed := sendClaudeStreaming(t, fixture, "Delegate this request to the specialist.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "CLAUDE_SUBAGENT_FINAL") {
		t.Fatalf("local subagent task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	const toolName = "Agent"
	assertClaudeToolEvents(t, streamed.toolEvents, toolName)
	persisted := getClaudeTask(t, fixture, streamed.taskID)
	assertClaudeToolEvents(t, claudeTaskToolEvents(persisted), toolName)
	assertNoClaudeChildInstance(t, fixture, childTemplate)
	assertClaudeTaskHistory(t, fixture, streamed.taskID)
}

type claudeStreamResult struct {
	taskID      a2atype.TaskID
	state       a2atype.TaskState
	text        string
	sawWorking  bool
	sawArtifact bool
	toolEvents  []claudeToolEvent
	failureText string
}

type claudeToolEvent struct {
	partType string
	id       string
	name     string
}

func sendClaudeStreaming(t *testing.T, fixture *interactionFixture, text string) claudeStreamResult {
	t.Helper()
	_, request := newMessageRequest(t, text)
	stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
	if err != nil {
		t.Fatalf("start streaming Claude A2A message: %v", err)
	}
	var result claudeStreamResult
	var output strings.Builder
	terminalEvents := 0
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if terminalEvents != 1 {
				t.Fatalf("Claude stream terminal event count = %d, want 1", terminalEvents)
			}
			if result.taskID == "" {
				t.Fatal("Claude stream completed without a task ID")
			}
			result.text = output.String()
			return result
		}
		if err != nil {
			t.Fatalf("receive Claude task stream: %v", err)
		}
		if terminalEvents != 0 {
			t.Fatalf("Claude stream emitted an event after terminal state %s", result.state)
		}
		event, err := pbconv.FromProtoStreamResponse(response)
		if err != nil {
			t.Fatalf("decode Claude task stream: %v", err)
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
				result.toolEvents = append(result.toolEvents, claudeToolEvents(event.Artifact.Parts)...)
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
				result.toolEvents = append(result.toolEvents, claudeToolEvents(event.Status.Message.Parts)...)
			}
		}
		if result.state.Terminal() {
			terminalEvents++
		}
	}
}

func claudeToolEvents(parts []*a2atype.Part) []claudeToolEvent {
	var events []claudeToolEvent
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
		events = append(events, claudeToolEvent{partType: partType, id: id, name: name})
	}
	return events
}

func claudeTaskToolEvents(task *a2atype.Task) []claudeToolEvent {
	var events []claudeToolEvent
	for _, message := range task.History {
		if message != nil {
			events = append(events, claudeToolEvents(message.Parts)...)
		}
	}
	if task.Status.Message != nil {
		events = append(events, claudeToolEvents(task.Status.Message.Parts)...)
	}
	for _, artifact := range task.Artifacts {
		if artifact != nil {
			events = append(events, claudeToolEvents(artifact.Parts)...)
		}
	}
	return events
}

func assertClaudeToolEvents(t *testing.T, events []claudeToolEvent, toolNames ...string) {
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

func getClaudeTask(t *testing.T, fixture *interactionFixture, taskID a2atype.TaskID) *a2atype.Task {
	t.Helper()
	request, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: taskID})
	if err != nil {
		t.Fatalf("build GetTask request: %v", err)
	}
	response, err := fixture.client.GetTask(fixture.ctx, request)
	if err != nil {
		t.Fatalf("get Claude task %s: %v", taskID, err)
	}
	task, err := pbconv.FromProtoTask(response)
	if err != nil {
		t.Fatalf("decode Claude task %s: %v", taskID, err)
	}
	return task
}

func assertClaudeTaskHistory(t *testing.T, fixture *interactionFixture, taskIDs ...a2atype.TaskID) {
	t.Helper()
	request, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{ContextID: fixture.instanceID})
	if err != nil {
		t.Fatalf("build ListTasks request: %v", err)
	}
	response, err := fixture.client.ListTasks(fixture.ctx, request)
	if err != nil {
		t.Fatalf("list Claude tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(response)
	if err != nil {
		t.Fatalf("decode Claude task list: %v", err)
	}
	if len(listed.Tasks) != len(taskIDs) {
		t.Fatalf("Claude task count = %d, want %d", len(listed.Tasks), len(taskIDs))
	}
	want := make(map[a2atype.TaskID]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		want[taskID] = struct{}{}
	}
	for _, task := range listed.Tasks {
		if _, ok := want[task.ID]; !ok {
			t.Fatalf("listed unexpected Claude task %s", task.ID)
		}
		if task.Status.State != a2atype.TaskStateCompleted {
			t.Fatalf("listed Claude task %s state = %s, want COMPLETED", task.ID, task.Status.State)
		}
	}
}

func startBlockingClaudeMock(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_claude_agent.json", claudeInteractionMocks)
	if err != nil {
		t.Fatalf("load Claude mock LLM response: %v", err)
	}
	if len(cfg.Anthropic) == 0 {
		t.Fatal("Claude mock LLM fixture has no Anthropic response")
	}
	baseURL, started := startBlockingMockServer(t, cfg.Anthropic[0].Response)
	return reachableServerURL(t, baseURL, ""), started
}

func createClaudeMockTemplate(t *testing.T, baseURL string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	model := createClaudeMockModel(t, kube, baseURL)
	return createClaudeTemplate(t, kube, model.Name, "Claude mockLLM interaction fixture")
}

func createClaudeMockModel(t *testing.T, kube ctrlclient.Client, baseURL string) *v1alpha3.ModelConfig {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "claude-mock-", Namespace: "kagent"},
		Data:       map[string][]byte{"ANTHROPIC_API_KEY": []byte("mock-key")},
	}
	if err := kube.Create(t.Context(), secret); err != nil {
		t.Fatalf("create Claude mock Secret: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), secret); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Claude mock Secret: %v", err)
		}
	})
	model := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "claude-mock-", Namespace: "kagent"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider:     v1alpha3.ModelProviderAnthropic,
			Model:        "claude-sonnet-4-5",
			APIKeySecret: secret.Name, APIKeySecretKey: "ANTHROPIC_API_KEY",
			Anthropic: &v1alpha3.AnthropicConfig{BaseURL: baseURL},
		},
	}
	if err := kube.Create(t.Context(), model); err != nil {
		t.Fatalf("create Claude mock ModelConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), model); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Claude mock ModelConfig: %v", err)
		}
	})
	return model
}

func createClaudeTemplate(t *testing.T, kube ctrlclient.Client, modelConfig, description string) string {
	t.Helper()
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "claude-interaction-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "claude"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: modelConfig},
			Description: description, SystemPrompt: "Reply concisely and follow the requested output format exactly.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, claudeE2EHarness)
	return template.Name
}

func createClaudeLocalAgentTemplates(t *testing.T, kube ctrlclient.Client, model *v1alpha3.ModelConfig, childPrompt string) (string, string) {
	t.Helper()
	child := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "claude-local-child-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "claude"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
			Description:  "Claude local specialist",
			SystemPrompt: childPrompt,
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, child, claudeE2EHarness)
	root := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "claude-local-root-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "claude"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
			Description:  "Claude local-subagent E2E fixture",
			SystemPrompt: "Always delegate the request to the specialist subagent, then return its answer.",
			Tools: []v1alpha3.ToolBinding{{Agent: &v1alpha3.AgentToolBinding{
				Name: "specialist", Description: "Handles every delegated specialist request",
				TemplateRef: corev1.LocalObjectReference{Name: child.Name},
				Isolation:   v1alpha3.AgentToolIsolationShared,
			}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, root, claudeE2EHarness)
	return root.Name, child.Name
}

func assertNoClaudeChildInstance(t *testing.T, fixture *interactionFixture, childTemplate string) {
	t.Helper()
	instances, err := fixture.instances.ListAgentInstances(fixture.ctx, &apiv1alpha1.ListAgentInstancesRequest{Namespace: "kagent"})
	if err != nil {
		t.Fatalf("list Claude AgentInstances: %v", err)
	}
	for _, instance := range instances.GetAgentInstances() {
		if instance.GetAgentTemplate().GetName() == childTemplate {
			t.Fatalf("Claude local child created AgentInstance %q", instance.GetId())
		}
	}
}

func TestE2EClaudeMockWholeServerMCP(t *testing.T) {
	target := interactionTarget(t)
	mcpURL, mcpMock := startMCPMock(t)

	kube := interactionKubeClient(t)
	mcpServer := createClaudeMCPServer(t, kube, mcpURL)
	toolName := "mcp__" + mcpServer.Name + "__add_numbers"
	llmURL := startClaudeResourceMockLLM(t, toolName)
	model := createClaudeMockModel(t, kube, llmURL)
	template := createClaudeMCPTemplate(t, kube, model.Name, mcpServer.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, template)

	streamed := sendClaudeStreaming(t, fixture, "Add 3 and 5 using the configured MCP server.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "CLAUDE_MCP_DONE result is 8") {
		t.Fatalf("whole-server MCP task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	assertClaudeToolEvents(t, streamed.toolEvents, toolName)
	assertClaudeToolEvents(t, claudeTaskToolEvents(getClaudeTask(t, fixture, streamed.taskID)), toolName)

	for _, request := range mcpMock.Requests() {
		if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) && bytes.Contains(request.Body, []byte(`"name":"add_numbers"`)) {
			return
		}
	}
	t.Fatal("mock MCP server did not receive an add_numbers tool call")
}

func createClaudeMCPServer(t *testing.T, kube ctrlclient.Client, mcpURL string) *v1alpha3.RemoteMCPServer {
	t.Helper()
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "claude-resources-", Namespace: "kagent"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Description: "Claude whole-server MCP E2E fixture",
			Protocol:    v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:         mcpURL,
		},
	}
	if err := kube.Create(t.Context(), server); err != nil {
		t.Fatalf("create Claude RemoteMCPServer: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), server); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Claude RemoteMCPServer: %v", err)
		}
	})

	return server
}

func createClaudeMCPTemplate(t *testing.T, kube ctrlclient.Client, modelConfig, mcpServer string) string {
	t.Helper()
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "claude-resources-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "claude"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: modelConfig},
			Description:  "Claude direct whole-server MCP E2E fixture",
			SystemPrompt: "Use the configured MCP tool. Do not calculate the answer yourself.",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: mcpServer},
			}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, claudeE2EHarness)
	return template.Name
}

func startClaudeResourceMockLLM(t *testing.T, toolName string) string {
	t.Helper()
	raw, err := claudeInteractionMocks.ReadFile("mocks/invoke_claude_resources.json")
	if err != nil {
		t.Fatalf("read Claude resource mock fixture: %v", err)
	}
	raw = bytes.ReplaceAll(raw, []byte("MCP_TOOL_NAME"), []byte(toolName))
	var cfg mockllm.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode Claude resource mock fixture: %v", err)
	}
	return reachableServerURL(t, startMockLLMConfig(t, cfg), "")
}
