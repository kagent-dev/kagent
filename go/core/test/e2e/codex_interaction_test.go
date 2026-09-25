package e2e_test

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const codexE2EHarness = "codex-e2e"

//go:embed mocks/invoke_codex_builtin_tools.json mocks/invoke_codex_resources.json
var codexInteractionMocks embed.FS

func TestE2ECodexMockBuiltinToolEvents(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	modelURL := reachableModelURL(t, startMockLLMServer(t, codexInteractionMocks, "mocks/invoke_codex_builtin_tools.json"))
	template := createCodexMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, codexE2EHarness, template)

	streamed := sendStreaming(t, fixture, "Run the requested shell command.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "CODEX_BUILTIN_TOOL_DONE") {
		t.Fatalf("built-in tool task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	assertToolEvents(t, streamed.toolEvents, "command_execution")
	assertToolEvents(t, taskToolEvents(getTask(t, fixture, streamed.taskID)), "command_execution")
}

func TestE2ECodexMockWholeServerMCP(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	mcpURL, mcpMock := startMCPMock(t)

	kube := interactionKubeClient(t)
	mcpServer := createCodexMCPServer(t, kube, mcpURL)
	modelToolNamespace := codexMCPToolNamespace(mcpServer.Name)
	modelURL := startCodexResourceMockLLM(t, modelToolNamespace)
	model := createCodexMockModel(t, kube, modelURL)
	template := createCodexMCPTemplate(t, kube, model.Name, mcpServer.Name, false)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, codexE2EHarness, template)

	streamed := sendStreaming(t, fixture, "Add 3 and 5 using the configured MCP server.")
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
	assertToolEvents(t, streamed.toolEvents, toolName)
	assertToolEvents(t, taskToolEvents(getTask(t, fixture, streamed.taskID)), toolName)
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
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
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
	waitModelConfigAccepted(t, kube, model)
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

func createCodexMCPTemplate(t *testing.T, kube ctrlclient.Client, modelConfig, mcpServer string, requireApproval bool) string {
	t.Helper()
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "codex-resources-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "codex"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: modelConfig},
			Description:  "Codex direct whole-server MCP E2E fixture",
			SystemPrompt: "Use the configured MCP tool. Do not calculate the answer yourself.",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server:          corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: mcpServer},
				RequireApproval: requireApproval,
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
