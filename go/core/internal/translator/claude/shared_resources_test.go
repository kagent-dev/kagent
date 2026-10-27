package claude

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
)

func TestCompileSharedResources(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-root", APIKeySecret: "model-auth", APIKeySecretKey: "api-key"}
	input, _ := testInput(t, model, nil)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test"}, Data: map[string][]byte{"api-key": []byte("never-serialize-model-key"), "token": []byte("never-serialize-mcp-key")}}
	headers := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "headers", Namespace: "test", UID: "headers-uid"}, Data: map[string]string{"tenant": "team-a"}}
	mock := krttest.NewMock(t, []any{secret, headers})
	collections := translator.Collections{Secrets: krttest.GetMockCollection[*corev1.Secret](mock), ConfigMaps: krttest.GetMockCollection[*corev1.ConfigMap](mock)}
	server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "child.tools", Namespace: "test", UID: "child-server"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL: "https://child.example.com/mcp", HeadersFrom: []v1alpha3.ValueRef{
			{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: secret.Name, Key: "token"}},
			{Name: "X-Tenant", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.ConfigMapValueSource, Name: headers.Name, Key: "tenant"}},
		},
	}}
	server.Generation = 1
	server.Status = v1alpha3.RemoteMCPServerStatus{ObservedGeneration: 1, DiscoveredTools: []*v1alpha3.MCPTool{{Name: "read"}, {Name: "write"}}}
	childModel := model
	childModel.Model = "claude-child"
	child := &translator.AgentInput{
		Template: &translator.TemplateConfiguration{Name: "child", Namespace: "test", Source: &metav1.ObjectMeta{Name: "child", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
			Skills: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{URL: "https://skills.example.com/review.git", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}},
			Tools:  []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Tools: []string{"read"}}}},
		}},
		ResolvedModelConfig: &translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "child-model", Namespace: "test"}, Spec: childModel}},
		Instruction:         "Review the deployment.",
		MCPTools:            []translator.ResolvedMCPTool{{Server: server, Binding: v1alpha3.MCPToolBinding{Tools: []string{"read"}, RequireApproval: true}}},
	}
	input.Root.Shared = []translator.AgentInputBinding{{Name: "reviewer", Description: "Reviews deployments", Agent: child}}
	compiler := NewCompiler(krt.TestingDummyContext{}, collections, testRuntimeDefaults())
	result, err := compiler.Compile(t.Context(), input)
	require.NoError(t, err)
	cfg, err := claudeconfig.Parse(result.ConfigJSON)
	require.NoError(t, err)
	require.Empty(t, cfg.MCPServers, "child servers must not be installed on root")
	agent := cfg.Agents["reviewer"]
	require.Equal(t, []string{"review"}, agent.Skills)
	require.Contains(t, agent.Tools, "mcp__child_tools__read")
	require.NotContains(t, agent.Tools, "mcp__child_tools__*")
	require.Equal(t, []string{"mcp__child_tools__write"}, agent.DisallowedTools)
	require.Contains(t, agent.Tools, "Skill")
	require.True(t, agent.MCPServers["child_tools"].RequireApproval)
	require.Equal(t, "team-a", agent.MCPServers["child_tools"].Headers["X-Tenant"])
	require.Len(t, cfg.SkillResources.Skills, 1)
	require.Contains(t, result.EgressDestinations, "https://child.example.com:443")
	require.Contains(t, result.EgressDestinations, "https://skills.example.com:443")
	require.Empty(t, result.Warnings, "child tool selection is enforced")
	require.Contains(t, string(result.Provenance), `"name":"headers"`)
	require.Contains(t, string(result.Provenance), `"name":"child.tools"`)
	require.Len(t, result.Credentials, 2)
	for _, variable := range result.Environment {
		require.Nil(t, variable.ValueFrom)
	}
	require.False(t, bytes.Contains(result.ConfigJSON, secret.Data["token"]))
	require.False(t, bytes.Contains(result.Provenance, secret.Data["api-key"]))
	again, err := compiler.Compile(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, result, again)

	before, err := result.Digest()
	require.NoError(t, err)
	server.Spec.HeadersFrom[0].ValueFrom.Key = "api-key"
	changed, err := compiler.Compile(t.Context(), input)
	require.NoError(t, err)
	after, err := changed.Digest()
	require.NoError(t, err)
	require.NotEqual(t, before, after, "child credentials must affect the revision")

	child.Shared = []translator.AgentInputBinding{{Name: "nested"}}
	_, err = compiler.Compile(t.Context(), input)
	require.ErrorContains(t, err, "nested agent tools")
	child.Shared = nil
	server.Status.ObservedGeneration = 0
	_, err = compiler.Compile(t.Context(), input)
	require.ErrorContains(t, err, "requires current tool discovery")
	server.Status.ObservedGeneration = 1
	child.MCPTools[0].Binding.Tools = []string{"missing"}
	_, err = compiler.Compile(t.Context(), input)
	require.ErrorContains(t, err, "selects unknown")
	child.MCPTools[0].Binding.Tools = nil
	whole, err := compiler.Compile(t.Context(), input)
	require.NoError(t, err)
	wholeConfig, err := claudeconfig.Parse(whole.ConfigJSON)
	require.NoError(t, err)
	require.Contains(t, wholeConfig.Agents["reviewer"].Tools, "mcp__child_tools__*")
	require.Empty(t, wholeConfig.Agents["reviewer"].DisallowedTools)
	server.Spec.TokenPassthrough = true
	_, err = compiler.Compile(t.Context(), input)
	require.ErrorContains(t, err, "tokenPassthrough")
}

func TestSharedAgentNativeJSON(t *testing.T) {
	cfg := claudeconfig.Production("claude-root", "Root")
	cfg.Agents = map[string]claudeconfig.Agent{"reviewer": {
		Description: "Reviews", Prompt: "Review", Skills: []string{"review"}, Tools: []string{"mcp__tools__read"},
		MCPServers: map[string]claudeconfig.MCPServer{"tools": {Type: "http", URL: "https://mcp.example.com", Headers: map[string]string{"Authorization": "${TOKEN}"}, RequireApproval: true}},
	}}
	raw, err := cfg.AgentsJSON()
	require.NoError(t, err)
	require.JSONEq(t, `{"reviewer":{"description":"Reviews","prompt":"Review","skills":["kagent-skills:review"],"tools":["mcp__tools__read"],"mcpServers":[{"tools":{"type":"http","url":"https://mcp.example.com","headers":{"Authorization":"${TOKEN}"}}}]}}`, raw)
	contract, err := json.Marshal(cfg)
	require.NoError(t, err)
	parsed, err := claudeconfig.Parse(contract)
	require.NoError(t, err)
	require.Equal(t, cfg, parsed)
}
