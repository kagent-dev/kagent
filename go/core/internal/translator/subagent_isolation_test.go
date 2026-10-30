package translator_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	byotranslator "github.com/kagent-dev/kagent/go/core/internal/translator/byo"
	kagenttranslator "github.com/kagent-dev/kagent/go/core/internal/translator/kagent"
)

func isolationHarness(kind string) *v1alpha3.Harness {
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: kind, Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Workload:  v1alpha3.HarnessWorkload{Image: new("example.com/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
			Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
		},
	}
	switch kind {
	case "kagent":
		harness.Spec.Kagent = &v1alpha3.KagentHarness{}
	case "byo":
		harness.Spec.BYO = &v1alpha3.BYOHarness{}
		harness.Spec.Workload.Command = []string{"/app"}
	case "codex":
		harness.Spec.Codex = &v1alpha3.CodexHarness{}
	case "claude":
		harness.Spec.Claude = &v1alpha3.ClaudeHarness{}
	}
	return harness
}

// isolationTemplates returns a root with two subagent bindings: reviewer, whose
// isolation the test sets, and a Shared drafter. The reviewer's MCP binding can
// require approval.
func isolationTemplates(isolation v1alpha3.SubAgentIsolation, requireApproval bool) (root, reviewer, drafter *v1alpha3.AgentTemplate) {
	model := &v1alpha3.ObjectReference{Name: "default-model"}
	reviewer = &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "reviewer", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{ModelConfig: model, SystemPrompt: "review", Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
			Server: corev1.TypedObjectReference{Kind: "RemoteMCPServer", Name: "search"}, RequireApproval: requireApproval,
		}}}},
	}
	drafter = &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "drafter", Namespace: "test"},
		Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: model, SystemPrompt: "draft"},
	}
	root = &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "coordinator", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{ModelConfig: model, SystemPrompt: "coordinate", Tools: []v1alpha3.ToolBinding{
			{SubAgent: &v1alpha3.SubAgentToolBinding{Name: "reviewer", Description: "reviews", TemplateRef: &corev1.LocalObjectReference{Name: "reviewer"}, Isolation: isolation}},
			{SubAgent: &v1alpha3.SubAgentToolBinding{Name: "drafter", Description: "drafts", TemplateRef: &corev1.LocalObjectReference{Name: "drafter"}}},
		}},
	}
	return root, reviewer, drafter
}

func TestCompileAgentDeliversSubAgentIsolation(t *testing.T) {
	for _, kind := range []string{"kagent", "byo"} {
		t.Run(kind, func(t *testing.T) {
			harness := isolationHarness(kind)
			compile := func(isolation v1alpha3.SubAgentIsolation) (*translator.CompileResult, *configv1.ADKConfig) {
				root, reviewer, drafter := isolationTemplates(isolation, false)
				result, err := compiler(t, modelConfig(), reviewer, drafter, harness, remoteMCPServer("search", "https://search.example.com/mcp")).
					CompileAgent(t.Context(), inlineAgent(harness, root))
				require.NoError(t, err)
				var config configv1.ADKConfig
				require.NoError(t, runtimeconfig.Unmarshal(result.ConfigJSON, &config))
				return result, &config
			}

			isolated, isolatedConfig := compile(v1alpha3.SubAgentIsolationIsolated)
			require.Len(t, isolatedConfig.Agent.SubAgents, 2)
			require.Equal(t, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_SESSION, isolatedConfig.Agent.SubAgents[0].Isolation)
			require.Equal(t, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_UNSPECIFIED, isolatedConfig.Agent.SubAgents[1].Isolation, "a binding that does not ask stays shared")
			require.Equal(t, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_UNSPECIFIED, isolatedConfig.Agent.Isolation, "the root has no parent to isolate from")

			// Shared, stated or omitted, produces the same runtime revision as before the field existed.
			omitted, omittedConfig := compile("")
			shared, _ := compile(v1alpha3.SubAgentIsolationShared)
			require.Equal(t, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_UNSPECIFIED, omittedConfig.Agent.SubAgents[0].Isolation)
			omittedDigest, err := omitted.Digest()
			require.NoError(t, err)
			sharedDigest, err := shared.Digest()
			require.NoError(t, err)
			isolatedDigest, err := isolated.Digest()
			require.NoError(t, err)
			require.Equal(t, omittedDigest, sharedDigest, "Shared is the default")
			require.NotEqual(t, omittedDigest, isolatedDigest, "isolation changes how the runtime behaves, so it prepares a new revision")
		})
	}
}

func TestCompileAgentRejectsIsolatedSubAgentOnUnsupportedHarnesses(t *testing.T) {
	for _, kind := range []string{"codex", "claude"} {
		t.Run(kind, func(t *testing.T) {
			harness := isolationHarness(kind)
			root, reviewer, drafter := isolationTemplates(v1alpha3.SubAgentIsolationIsolated, false)
			_, err := compiler(t, modelConfig(), reviewer, drafter, harness, remoteMCPServer("search", "https://search.example.com/mcp")).
				CompileAgent(t.Context(), inlineAgent(harness, root))
			var validation *translator.ValidationError
			require.ErrorAs(t, err, &validation)
			require.ErrorContains(t, err, `subagent "reviewer" is Isolated, which Harness runtime "`+kind+`" does not support`)
		})
	}
}

func TestCompileAgentRejectsIsolatedSubAgentThatNeedsApproval(t *testing.T) {
	harness := isolationHarness("kagent")
	servers := remoteMCPServer("search", "https://search.example.com/mcp")

	root, reviewer, drafter := isolationTemplates(v1alpha3.SubAgentIsolationIsolated, true)
	_, err := compiler(t, modelConfig(), reviewer, drafter, harness, servers).CompileAgent(t.Context(), inlineAgent(harness, root))
	var validation *translator.ValidationError
	require.ErrorAs(t, err, &validation)
	require.ErrorContains(t, err, `subagent "reviewer" is Isolated and cannot pause for human approval, but its binding to RemoteMCPServer "search" requires it`)

	// The same subagent is fine when it shares the parent's session: the
	// approval request can reach the person through the parent.
	root, reviewer, drafter = isolationTemplates(v1alpha3.SubAgentIsolationShared, true)
	_, err = compiler(t, modelConfig(), reviewer, drafter, harness, servers).CompileAgent(t.Context(), inlineAgent(harness, root))
	require.NoError(t, err)

	// An Isolated subagent without the approval requirement is fine.
	root, reviewer, drafter = isolationTemplates(v1alpha3.SubAgentIsolationIsolated, false)
	_, err = compiler(t, modelConfig(), reviewer, drafter, harness, servers).CompileAgent(t.Context(), inlineAgent(harness, root))
	require.NoError(t, err)
}

func TestCompileAgentRejectsIsolatedSubAgentWithSTSExchange(t *testing.T) {
	harness := isolationHarness("kagent")
	root, reviewer, drafter := isolationTemplates(v1alpha3.SubAgentIsolationIsolated, false)

	sts := testRuntimeDefaults()
	sts.TokenPropagation = &configv1.TokenPropagationConfig{Enabled: true, StsWellKnownUri: new("https://sts.example.com/.well-known/oauth-authorization-server")}
	_, err := compilerWithSettings(t, sts, modelConfig(), reviewer, drafter, harness, remoteMCPServer("search", "https://search.example.com/mcp")).
		CompileAgent(t.Context(), inlineAgent(harness, root))
	var validation *translator.ValidationError
	require.ErrorAs(t, err, &validation)
	require.ErrorContains(t, err, `subagent "reviewer" is Isolated, which cannot be combined with STS token exchange`)

	// Forwarding the caller's token without an exchange works in a new session:
	// the credential travels with the request, not with the session.
	propagate := testRuntimeDefaults()
	propagate.TokenPropagation = &configv1.TokenPropagationConfig{Enabled: true}
	_, err = compilerWithSettings(t, propagate, modelConfig(), reviewer, drafter, harness, remoteMCPServer("search", "https://search.example.com/mcp")).
		CompileAgent(t.Context(), inlineAgent(harness, root))
	require.NoError(t, err)
}

// compilerWithSettings is compiler with the given runtime defaults.
func compilerWithSettings(t *testing.T, settings *configv1.RuntimeDefaultsConfig, objects ...any) *translator.Compiler {
	t.Helper()
	collections := mockCollections(t, append(objects, defaultWorkerPool())...)
	ctx := krt.TestingDummyContext{}
	return translator.NewCompiler(ctx, collections, map[translator.HarnessType]translator.HarnessCompiler{
		translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections, settings),
		translator.HarnessTypeBYO:    byotranslator.NewCompiler(ctx, collections, settings),
	}, testHarnessImages, settings)
}
