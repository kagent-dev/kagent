package e2e_test

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const claudeE2EHarness = "claude-e2e"

//go:embed mocks/invoke_claude_builtin_tools.json mocks/invoke_claude_local_subagent.json mocks/invoke_claude_resources.json
var claudeInteractionMocks embed.FS

func TestE2EClaudeMockBuiltinToolEvents(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	modelURL := reachableServerURL(t, startMockLLMServer(t, claudeInteractionMocks, "mocks/invoke_claude_builtin_tools.json"), "")
	template := createClaudeMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, template)

	streamed := sendStreaming(t, fixture, "Create the requested file and read it back.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "CLAUDE_BUILTIN_TOOLS_DONE") {
		t.Fatalf("built-in tool task state = %s, text = %q", streamed.state, streamed.text)
	}
	assertToolEvents(t, streamed.toolEvents, "Bash", "Read")
	persisted := getTask(t, fixture, streamed.taskID)
	assertToolEvents(t, taskToolEvents(persisted), "Bash", "Read")
}

func TestE2EClaudeMockLocalSubagentRouting(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	modelURL := reachableServerURL(t, startMockLLMServer(t, claudeInteractionMocks, "mocks/invoke_claude_local_subagent.json"), "")
	kube := interactionKubeClient(t)
	model := createClaudeMockModel(t, kube, modelURL)
	rootTemplate, childTemplate := createClaudeLocalAgentTemplates(t, kube, model, "CLAUDE_LOCAL_SPECIALIST_INSTRUCTION")
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, rootTemplate)

	streamed := sendStreaming(t, fixture, "Delegate this request to the specialist.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "CLAUDE_SUBAGENT_FINAL") {
		t.Fatalf("local subagent task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	const toolName = "Agent"
	assertToolEvents(t, streamed.toolEvents, toolName)
	persisted := getTask(t, fixture, streamed.taskID)
	assertToolEvents(t, taskToolEvents(persisted), toolName)
	assertNoClaudeChildInstance(t, fixture, childTemplate)
	assertTaskHistory(t, fixture, streamed.taskID)
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
	waitModelConfigReady(t, kube, model)
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
	instances, err := fixture.instances.ListAgentInstances(fixture.ctx, &apiv1alpha1.ListAgentInstancesRequest{})
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
	t.Parallel()
	target := interactionTarget(t)
	mcpURL, mcpMock := startMCPMock(t)

	kube := interactionKubeClient(t)
	mcpServer := createClaudeMCPServer(t, kube, mcpURL)
	toolName := "mcp__" + mcpServer.Name + "__add_numbers"
	llmURL := startClaudeResourceMockLLM(t, toolName)
	model := createClaudeMockModel(t, kube, llmURL)
	template := createClaudeMCPTemplate(t, kube, model.Name, mcpServer.Name, false)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, template)

	streamed := sendStreaming(t, fixture, "Add 3 and 5 using the configured MCP server.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "CLAUDE_MCP_DONE result is 8") {
		t.Fatalf("whole-server MCP task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	assertToolEvents(t, streamed.toolEvents, toolName)
	assertToolEvents(t, taskToolEvents(getTask(t, fixture, streamed.taskID)), toolName)

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

func createClaudeMCPTemplate(t *testing.T, kube ctrlclient.Client, modelConfig, mcpServer string, requireApproval bool) string {
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
				Server:          corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: mcpServer},
				RequireApproval: requireApproval,
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
