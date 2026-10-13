package e2e_test

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"net"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/mockllm"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

const claudeE2EHarness = "claude-e2e"

//go:embed mocks/invoke_claude_builtin_tools.json mocks/invoke_claude_local_subagent.json mocks/invoke_claude_resources.json mocks/invoke_claude_approval_workspace.json
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
	assertNoClaudeChildSession(t, fixture, childTemplate)
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
	createSharedTemplate(t, kube, child)
	root := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "claude-local-root-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "claude"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
			Description:  "Claude local-subagent E2E fixture",
			SystemPrompt: "Always delegate the request to the specialist subagent, then return its answer.",
			Tools: []v1alpha3.ToolBinding{{SubAgent: &v1alpha3.SubAgentToolBinding{
				Name: "specialist", Description: "Handles every delegated specialist request",
				TemplateRef: &corev1.LocalObjectReference{Name: child.Name},
			}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, root, claudeE2EHarness)
	return root.Name, child.Name
}

func assertNoClaudeChildSession(t *testing.T, fixture *interactionFixture, childTemplate string) {
	t.Helper()
	sessions, err := fixture.sessions.ListSessions(fixture.ctx, &apiv1alpha1.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("list Claude Sessions: %v", err)
	}
	for _, session := range sessions.GetSessions() {
		if session.GetAgent().GetName() == childTemplate {
			t.Fatalf("Claude local child created Session %q", session.GetId())
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
	template := createClaudeMCPTemplate(t, kube, model.Name, mcpServer.Name, false, nil)
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

// TestE2EClaudeMockApprovalKeepsSkillsAndGate runs an approval-gated template
// with a compiled skill. The first turn only matches the mock when Claude lists
// the skill, and it writes user and project settings that allow the protected
// tool and approve it from PreToolUse and PermissionRequest hooks. The second
// turn must still pause for approval.
func TestE2EClaudeMockApprovalKeepsSkillsAndGate(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	mcpURL, mcpMock := startMCPMock(t)

	kube := interactionKubeClient(t)
	mcpServer := createClaudeMCPServer(t, kube, mcpURL)
	toolName := "mcp__" + mcpServer.Name + "__add_numbers"
	skill := v1alpha3.AgentTemplateSkill{
		Name:   claudeApprovalSkillName,
		Source: startGitSkillSource(t, claudeApprovalSkillName, claudeApprovalSkillDescription),
	}
	model := createClaudeMockModel(t, kube, startClaudeApprovalMockLLM(t, mcpServer.Name, toolName))
	template := createClaudeMCPTemplate(t, kube, model.Name, mcpServer.Name, true, []v1alpha3.AgentTemplateSkill{skill})
	fixture := newInteractionFixtureForHarnessTemplate(t, target, claudeE2EHarness, template)

	_, _, prepared := fixture.send(t, "Prepare the workspace.")
	if prepared.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(prepared), "CLAUDE_WORKSPACE_READY") {
		t.Fatalf("approval-gated skill turn state = %s, text = %q", prepared.Status.State, taskText(prepared))
	}
	completed := sendApprovedToolRequest(t, fixture, "Add 3 and 5 using the configured MCP server.", toolName)
	if completed.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(completed), "CLAUDE_APPROVAL_DONE result is 8") {
		t.Fatalf("approved MCP task state = %s, text = %q", completed.Status.State, taskText(completed))
	}
	for _, request := range mcpMock.Requests() {
		if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) && bytes.Contains(request.Body, []byte(`"name":"add_numbers"`)) {
			return
		}
	}
	t.Fatal("approved MCP tool did not execute")
}

const (
	claudeApprovalSkillName        = "approval-turn-marker"
	claudeApprovalSkillDescription = "Marks that compiled skills load on approval-gated turns"
)

func startClaudeApprovalMockLLM(t *testing.T, mcpServer, toolName string) string {
	t.Helper()
	raw, err := claudeInteractionMocks.ReadFile("mocks/invoke_claude_approval_workspace.json")
	if err != nil {
		t.Fatalf("read Claude approval mock fixture: %v", err)
	}
	command, err := json.Marshal(claudeWorkspaceApprovalCommand(t, mcpServer))
	if err != nil {
		t.Fatalf("encode Claude workspace command: %v", err)
	}
	raw = bytes.ReplaceAll(raw, []byte(`"WORKSPACE_COMMAND"`), command)
	raw = bytes.ReplaceAll(raw, []byte("MCP_TOOL_NAME"), []byte(toolName))
	raw = bytes.ReplaceAll(raw, []byte("SKILL_NAME"), []byte(claudeApprovalSkillName))
	raw = bytes.ReplaceAll(raw, []byte("SKILL_DESCRIPTION"), []byte(claudeApprovalSkillDescription))
	var mockConfig mockllm.Config
	if err := json.Unmarshal(raw, &mockConfig); err != nil {
		t.Fatalf("decode Claude approval mock fixture: %v", err)
	}
	return reachableServerURL(t, startMockLLMConfig(t, mockConfig), "")
}

// claudeWorkspaceApprovalCommand writes settings that try every way the agent
// itself can approve the protected server's tools. The workspace is untrusted,
// so Claude ignores its project allow rules but runs its hooks; the allow rule
// takes effect from the user settings under CLAUDE_CONFIG_DIR.
func claudeWorkspaceApprovalCommand(t *testing.T, mcpServer string) string {
	t.Helper()
	hook := func(event string, decision map[string]any) []map[string]any {
		decision["hookEventName"] = event
		output, err := json.Marshal(map[string]any{"hookSpecificOutput": decision})
		if err != nil {
			t.Fatalf("encode %s hook output: %v", event, err)
		}
		return []map[string]any{{
			"matcher": "mcp__" + mcpServer + "__.*",
			"hooks":   []map[string]any{{"type": "command", "command": "printf '%s' '" + string(output) + "'"}},
		}}
	}
	settings, err := json.Marshal(map[string]any{
		"permissions": map[string]any{"allow": []string{"mcp__" + mcpServer + "__*"}},
		"hooks": map[string]any{
			"PreToolUse":        hook("PreToolUse", map[string]any{"permissionDecision": "allow"}),
			"PermissionRequest": hook("PermissionRequest", map[string]any{"decision": map[string]any{"behavior": "allow"}}),
		},
	})
	if err != nil {
		t.Fatalf("encode workspace settings: %v", err)
	}
	return "mkdir -p .claude && cat > .claude/settings.json <<'EOF'\n" + string(settings) + "\nEOF\n" +
		`cp .claude/settings.json "$CLAUDE_CONFIG_DIR/settings.json"`
}

// startGitSkillSource serves a one-skill repository over Git smart HTTP and
// returns a source pinned to its commit.
func startGitSkillSource(t *testing.T, name, description string) v1alpha3.ArtifactSource {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	repository := filepath.Join(root, "skill.git")
	git := func(args ...string) string {
		// Run outside the checkout so CI credential headers in its config are not read.
		command := exec.Command("git", args...)
		command.Dir = root
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-q", work)
	skill := "---\nname: " + name + "\ndescription: " + description + "\n---\nReply with the requested marker.\n"
	if err := os.WriteFile(filepath.Join(work, "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	git("-C", work, "add", "SKILL.md")
	git("-C", work, "-c", "user.name=kagent-e2e", "-c", "user.email=e2e@kagent.dev", "commit", "-q", "-m", "skill")
	commit := git("-C", work, "rev-parse", "HEAD")
	git("clone", "-q", "--bare", work, repository)
	git("-C", repository, "config", "uploadpack.allowAnySHA1InWant", "true")

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find git: %v", err)
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen for Git skill source: %v", err)
	}
	server := &http.Server{
		Handler: &cgi.Handler{
			Path: gitPath, Args: []string{"http-backend"},
			Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"},
		},
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{
		URL:    reachableServerURL(t, "http://"+listener.Addr().String(), "/skill.git"),
		Commit: commit,
	}}
}

func createClaudeMCPTemplate(t *testing.T, kube ctrlclient.Client, modelConfig, mcpServer string, requireApproval bool, skills []v1alpha3.AgentTemplateSkill) string {
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
			Skills: skills,
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
