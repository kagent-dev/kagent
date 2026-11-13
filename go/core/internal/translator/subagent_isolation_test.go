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

func TestCompileAgentDeliversSubAgentIsolation(t *testing.T) {
	for _, kind := range []string{"kagent", "byo"} {
		t.Run("Given a "+kind+" Harness, when a subagent binding selects Session, assert that only that subagent runs in its own session", func(t *testing.T) {
			session, config := compileIsolation(t, isolationHarness(kind), v1alpha3.SubAgentIsolationSession)

			require.Len(t, config.Agent.SubAgents, 2)
			require.Equal(t, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_SESSION, config.Agent.SubAgents[0].Isolation)
			require.Equal(t, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_UNSPECIFIED, config.Agent.SubAgents[1].Isolation, "a binding that does not ask stays shared")
			require.Equal(t, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_UNSPECIFIED, config.Agent.Isolation, "the root has no parent to isolate from")

			omitted, _ := compileIsolation(t, isolationHarness(kind), "")
			require.NotEqual(t, digest(t, omitted), digest(t, session), "isolation changes how the runtime behaves, so it prepares a new revision")
		})

		t.Run("Given a "+kind+" Harness, when a subagent binding states Shared, assert that the revision matches an omitted isolation", func(t *testing.T) {
			omitted, omittedConfig := compileIsolation(t, isolationHarness(kind), "")
			shared, _ := compileIsolation(t, isolationHarness(kind), v1alpha3.SubAgentIsolationShared)

			require.Equal(t, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_UNSPECIFIED, omittedConfig.Agent.SubAgents[0].Isolation)
			require.Equal(t, digest(t, omitted), digest(t, shared), "Shared is the default and stays unset")
		})
	}
}

func TestCompileAgentRejectsSessionSubAgent(t *testing.T) {
	servers := remoteMCPServer("search", "https://search.example.com/mcp")
	for _, kind := range []string{"codex", "claude"} {
		t.Run("Given a "+kind+" Harness, when a subagent binding selects Session, assert that compilation is rejected", func(t *testing.T) {
			harness := isolationHarness(kind)
			root, reviewer, drafter := isolationTemplates(v1alpha3.SubAgentIsolationSession, false)

			_, err := compiler(t, modelConfig(), reviewer, drafter, harness, servers).CompileAgent(t.Context(), inlineAgent(harness, root))

			var validation *translator.ValidationError
			require.ErrorAs(t, err, &validation)
			require.ErrorContains(t, err, `subagent "reviewer" runs in its own Session, which Harness runtime "`+kind+`" does not support`)
		})
	}

	scenarios := []struct {
		name            string
		isolation       v1alpha3.SubAgentIsolation
		requireApproval bool
		wantErr         string
	}{
		{
			name:      "Given a kagent Harness, when a Session subagent binds an MCP server that requires approval, assert that compilation is rejected",
			isolation: v1alpha3.SubAgentIsolationSession, requireApproval: true,
			wantErr: `subagent "reviewer" runs in its own Session and cannot pause for human approval, but its binding to RemoteMCPServer "search" requires it`,
		},
		{
			name:      "Given a kagent Harness, when a Shared subagent binds an MCP server that requires approval, assert that compilation succeeds",
			isolation: v1alpha3.SubAgentIsolationShared, requireApproval: true,
		},
		{
			name:      "Given a kagent Harness, when a Session subagent binds an MCP server without approval, assert that compilation succeeds",
			isolation: v1alpha3.SubAgentIsolationSession,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			harness := isolationHarness("kagent")
			root, reviewer, drafter := isolationTemplates(scenario.isolation, scenario.requireApproval)

			_, err := compiler(t, modelConfig(), reviewer, drafter, harness, servers).CompileAgent(t.Context(), inlineAgent(harness, root))

			if scenario.wantErr == "" {
				require.NoError(t, err)
				return
			}
			var validation *translator.ValidationError
			require.ErrorAs(t, err, &validation)
			require.ErrorContains(t, err, scenario.wantErr)
		})
	}
}

func TestCompileAgentSessionSubAgentWithTokenPropagation(t *testing.T) {
	servers := remoteMCPServer("search", "https://search.example.com/mcp")
	harness := isolationHarness("kagent")

	t.Run("Given STS token exchange, when a subagent binding selects Session, assert that compilation is rejected", func(t *testing.T) {
		root, reviewer, drafter := isolationTemplates(v1alpha3.SubAgentIsolationSession, false)
		settings := testRuntimeDefaults()
		settings.TokenPropagation = &configv1.TokenPropagationConfig{Enabled: true, StsWellKnownUri: new("https://sts.example.com/.well-known/oauth-authorization-server")}

		_, err := compilerWithSettings(t, settings, modelConfig(), reviewer, drafter, harness, servers).CompileAgent(t.Context(), inlineAgent(harness, root))

		var validation *translator.ValidationError
		require.ErrorAs(t, err, &validation)
		require.ErrorContains(t, err, `subagent "reviewer" runs in its own Session, which cannot be combined with STS token exchange`)
	})

	t.Run("Given token propagation without exchange, when a subagent binding selects Session, assert that compilation succeeds", func(t *testing.T) {
		// The credential travels with the request, not with the session.
		root, reviewer, drafter := isolationTemplates(v1alpha3.SubAgentIsolationSession, false)
		settings := testRuntimeDefaults()
		settings.TokenPropagation = &configv1.TokenPropagationConfig{Enabled: true}

		_, err := compilerWithSettings(t, settings, modelConfig(), reviewer, drafter, harness, servers).CompileAgent(t.Context(), inlineAgent(harness, root))

		require.NoError(t, err)
	})
}

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
	model := &v1alpha3.ModelConfigReference{Name: "default-model"}
	reviewer = &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "reviewer", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{ModelConfig: model, SystemPrompt: "review", Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
			Server: v1alpha3.MCPServerReference{Name: "search"}, RequireApproval: requireApproval,
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

func compileIsolation(t *testing.T, harness *v1alpha3.Harness, isolation v1alpha3.SubAgentIsolation) (*translator.CompileResult, *configv1.ADKConfig) {
	t.Helper()
	root, reviewer, drafter := isolationTemplates(isolation, false)
	result, err := compiler(t, modelConfig(), reviewer, drafter, harness, remoteMCPServer("search", "https://search.example.com/mcp")).
		CompileAgent(t.Context(), inlineAgent(harness, root))
	require.NoError(t, err)
	var config configv1.ADKConfig
	require.NoError(t, runtimeconfig.Unmarshal(result.ConfigJSON, &config))
	return result, &config
}

func digest(t *testing.T, result *translator.CompileResult) translator.RevisionID {
	t.Helper()
	value, err := result.Digest()
	require.NoError(t, err)
	return value
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
