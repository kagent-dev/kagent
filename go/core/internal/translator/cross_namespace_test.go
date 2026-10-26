package translator_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCrossNamespaceModelsAndMCP(t *testing.T) {
	for _, kind := range []string{"kagent", "codex", "claude"} {
		t.Run(kind, func(t *testing.T) {
			model := modelConfig()
			model.Namespace = "shared"
			model.Spec.AllowedNamespaces = &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}
			model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "auth", "token"
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: kind, Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
				Workload:  v1alpha3.HarnessWorkload{Image: new("example.com/runtime@sha256:" + strings.Repeat("a", 64))},
				Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
			}}
			switch kind {
			case "kagent":
				harness.Spec.Kagent = &v1alpha3.KagentHarness{}
			case "codex":
				harness.Spec.Codex = &v1alpha3.CodexHarness{}
				model.Spec.OpenAI = &v1alpha3.OpenAIConfig{APIFormat: new(v1alpha3.OpenAIAPIFormatResponses)}
			case "claude":
				harness.Spec.Claude = &v1alpha3.ClaudeHarness{}
				model.Spec.Provider = v1alpha3.ModelProviderAnthropic
			}
			server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "shared"}, Spec: v1alpha3.RemoteMCPServerSpec{
				URL: "https://tools.example.com/mcp", AllowedNamespaces: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll},
				HeadersFrom: []v1alpha3.ValueRef{
					{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "auth", Key: "token"}},
					{Name: "X-Tenant", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.ConfigMapValueSource, Name: "headers", Key: "tenant"}},
				},
			}}
			template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig: &v1alpha3.ObjectReference{Name: model.Name, Namespace: "shared"}, SystemPrompt: "help",
				Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedObjectReference{Kind: "RemoteMCPServer", Name: "tools", Namespace: new("shared")}}}},
			}}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "shared"}, Data: map[string][]byte{"token": []byte("shared-credential")}}
			headers := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "headers", Namespace: "shared"}, Data: map[string]string{"tenant": "shared-tenant"}}
			result, err := compiler(t, model, harness, server, secret, headers).CompileAgent(t.Context(), inlineAgent(harness, template))
			require.NoError(t, err)
			require.Len(t, result.Credentials, 2)
			for _, credential := range result.Credentials {
				require.Equal(t, "ate-secret://k8s.io/default/shared/auth/token", credential.URI)
			}
			require.Contains(t, string(result.ConfigJSON), "shared-tenant")
			require.NotContains(t, string(result.ConfigJSON), "shared-credential")
			require.Contains(t, string(result.Provenance), "shared/tools")
			require.Contains(t, string(result.Provenance), "shared/default-model")
			preparedRefs, err := translator.PreparedReferencesFromProvenance(result.Provenance, "test")
			require.NoError(t, err)
			require.Len(t, preparedRefs, 2, "all harnesses must retain the model and MCP identities")
			model.Spec.AllowedNamespaces = nil
			_, err = compiler(t, model, harness, server, secret, headers).CompileAgent(t.Context(), inlineAgent(harness, template))
			require.ErrorContains(t, err, "denied by allowedNamespaces")
			require.NotContains(t, err.Error(), "auth")
			model.Spec.AllowedNamespaces = &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}
			server.Spec.AllowedNamespaces = nil
			_, err = compiler(t, model, harness, server, secret, headers).CompileAgent(t.Context(), inlineAgent(harness, template))
			require.ErrorContains(t, err, "denied by allowedNamespaces")
		})
	}
}

func TestModelReferencePolicy(t *testing.T) {
	for _, test := range []struct {
		name, source, target string
		policy               *v1alpha3.AllowedNamespaces
		namespace            *corev1.Namespace
		allowed              bool
	}{
		{name: "omitted local", source: "test", target: "test", allowed: true},
		{name: "omitted cross", source: "test", target: "shared"},
		{name: "all", source: "test", target: "shared", policy: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}, allowed: true},
		{name: "selector unavailable", source: "test", target: "shared", policy: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSelector, Selector: &metav1.LabelSelector{}}},
		{name: "selector missing", source: "test", target: "shared", policy: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSelector}},
		{name: "selector same namespace mismatch", source: "test", target: "test", policy: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSelector, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"shared": "yes"}}}, namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}},
		{name: "selector match", source: "test", target: "shared", policy: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSelector, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"shared": "yes"}}}, namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"shared": "yes"}}}, allowed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := modelConfig()
			model.Namespace = test.target
			model.Spec.AllowedNamespaces = test.policy
			inputs := mockCollections(t, model)
			inputs.Namespaces = krt.NewStaticCollection(nil, []translator.NamespaceObservation{{Name: test.source, Namespace: test.namespace}})
			result, err := translator.ResolveModelReference(krt.TestingDummyContext{}, inputs, test.source, v1alpha3.ObjectReference{Name: model.Name, Namespace: test.target})
			if test.allowed {
				require.NoError(t, err)
				require.Equal(t, test.target, result.Config.Namespace)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestAuxiliaryModelReferencesUseHarnessNamespaceAndPolicy(t *testing.T) {
	for _, auxiliary := range []string{"memory", "summarizer"} {
		t.Run(auxiliary, func(t *testing.T) {
			root := modelConfig()
			shared := root.DeepCopy()
			shared.Namespace = "shared"
			shared.Spec.Model = "shared-model"
			shared.Spec.AllowedNamespaces = &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runner", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
				Kagent:    &v1alpha3.KagentHarness{},
				Workload:  v1alpha3.HarnessWorkload{Image: new("example.com/runtime@sha256:" + strings.Repeat("a", 64))},
				Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
			}}
			ref := v1alpha3.ObjectReference{Name: shared.Name, Namespace: shared.Namespace}
			if auxiliary == "memory" {
				harness.Spec.Kagent.Memory = &v1alpha3.KagentHarnessMemory{ModelConfigRef: ref}
			} else {
				harness.Spec.Kagent.Compaction = &v1alpha3.KagentHarnessCompaction{CompactionInterval: new(5), Summarizer: &v1alpha3.KagentHarnessSummarizer{ModelConfigRef: &ref}}
			}
			template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: root.Name}}}
			result, err := compiler(t, root, shared, harness).CompileAgent(t.Context(), inlineAgent(harness, template))
			require.NoError(t, err)
			require.Contains(t, string(result.ConfigJSON), "shared-model", "same-name auxiliary model must not alias the root")
			require.Contains(t, string(result.Provenance), "shared/default-model")
			shared.Spec.AllowedNamespaces = nil
			// A root model error must not hide auxiliary model revocation.
			root.Spec.APIKeySecret, root.Spec.APIKeySecretKey = "missing", "token"
			_, err = compiler(t, root, shared, harness).CompileAgent(t.Context(), inlineAgent(harness, template))
			var denied *translator.ReferencePolicyError
			require.ErrorAs(t, err, &denied)
		})
	}
}

func TestCrossNamespaceMCPNames(t *testing.T) {
	a := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "same.tools", Namespace: "team-a"}}
	b := a.DeepCopy()
	b.Namespace = "team-b"
	require.Equal(t, "same_tools", translator.MCPRuntimeName("team-a", a))
	require.NotEqual(t, translator.MCPRuntimeName("consumer", a), translator.MCPRuntimeName("consumer", b))
	require.Equal(t, translator.MCPRuntimeName("consumer", a), translator.MCPRuntimeName("consumer", a.DeepCopy()))
}

func TestReferencePolicyMatrix(t *testing.T) {
	selector := &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSelector, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "engineering"}}}
	for _, kind := range []string{"ModelConfig", "RemoteMCPServer"} {
		for _, test := range []struct {
			name, target string
			policy       *v1alpha3.AllowedNamespaces
			labels       map[string]string
			observeError string
			allowed      bool
		}{
			{name: "default local", target: "test", allowed: true},
			{name: "default cross namespace", target: "shared"},
			{name: "empty policy", target: "shared", policy: &v1alpha3.AllowedNamespaces{}},
			{name: "same local", target: "test", policy: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSame}, allowed: true},
			{name: "same cross namespace", target: "shared", policy: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSame}},
			{name: "all", target: "shared", policy: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}, allowed: true},
			{name: "selector match", target: "shared", policy: selector, labels: map[string]string{"team": "engineering"}, allowed: true},
			{name: "selector mismatch", target: "shared", policy: selector, labels: map[string]string{"team": "other"}},
			{name: "selector local mismatch", target: "test", policy: selector, labels: map[string]string{"team": "other"}},
			{name: "selector missing observation", target: "shared", policy: selector},
			{name: "selector observation error", target: "shared", policy: selector, labels: map[string]string{"team": "engineering"}, observeError: "forbidden"},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				model := modelConfig()
				server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://tools.example.com/mcp"}}
				if kind == "ModelConfig" {
					model.Namespace, model.Spec.AllowedNamespaces = test.target, test.policy
				} else {
					server.Namespace, server.Spec.AllowedNamespaces = test.target, test.policy
				}
				harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runner", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
					Kagent: &v1alpha3.KagentHarness{}, Workload: v1alpha3.HarnessWorkload{Image: new("example.com/runtime@sha256:" + strings.Repeat("a", 64))},
					Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
				}}
				template := &v1alpha3.AgentTemplate{Spec: v1alpha3.AgentTemplateSpec{
					ModelConfig: &v1alpha3.ObjectReference{Name: model.Name, Namespace: model.Namespace},
					Tools:       []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedObjectReference{Kind: "RemoteMCPServer", Name: server.Name, Namespace: new(server.Namespace)}}}},
				}}
				objects := []any{model, server, harness}
				if test.labels != nil || test.observeError != "" {
					objects = append(objects, translator.NamespaceObservation{Name: "test", Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: test.labels}}, Error: test.observeError})
				}
				_, err := compiler(t, objects...).CompileAgent(t.Context(), inlineAgent(harness, template))
				if test.allowed {
					require.NoError(t, err)
				} else {
					var denied *translator.ReferencePolicyError
					require.ErrorAs(t, err, &denied)
				}
			})
		}
	}
}

func TestReferenceRevocationWithOtherFailure(t *testing.T) {
	model := modelConfig()
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runner", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		Kagent:    &v1alpha3.KagentHarness{},
		Workload:  v1alpha3.HarnessWorkload{Image: new("example.com/runtime@sha256:" + strings.Repeat("a", 64))},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
	}}
	server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "shared"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://tools.example.com/mcp", AllowedNamespaces: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}}}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
		ModelConfig: &v1alpha3.ObjectReference{Name: model.Name},
		Tools:       []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedObjectReference{Kind: "RemoteMCPServer", Name: "tools", Namespace: new("shared")}}}},
	}}
	_, err := compiler(t, model, harness, server).CompileAgent(t.Context(), inlineAgent(harness, template))
	require.NoError(t, err)
	// An unrelated model failure must not hide the now-denied MCP policy.
	model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "missing", "token"
	server.Spec.AllowedNamespaces = &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSame}
	_, err = compiler(t, model, harness, server).CompileAgent(t.Context(), inlineAgent(harness, template))
	var denied *translator.ReferencePolicyError
	require.ErrorAs(t, err, &denied)
}

func TestReferencePolicyCheckedBeforeTemplateResolution(t *testing.T) {
	server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "shared"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://tools.example.com/mcp"}}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{Tools: []v1alpha3.ToolBinding{
		{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedObjectReference{Kind: "RemoteMCPServer", Name: "tools", Namespace: new("shared")}}},
		{SubAgent: &v1alpha3.SubAgentToolBinding{Name: "cycle", TemplateRef: &corev1.LocalObjectReference{Name: "child"}}},
	}}}
	agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "test"}, Spec: v1alpha3.AgentSpec{
		HarnessRef: corev1.LocalObjectReference{Name: "missing"},
		Template: v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{Tools: []v1alpha3.ToolBinding{
			{SubAgent: &v1alpha3.SubAgentToolBinding{Name: "missing", TemplateRef: &corev1.LocalObjectReference{Name: "missing"}}},
			{SubAgent: &v1alpha3.SubAgentToolBinding{Name: "child", TemplateRef: &corev1.LocalObjectReference{Name: "child"}}},
		}}},
	}}
	_, err := compiler(t, server, template).CompileAgent(t.Context(), agent)
	var denied *translator.ReferencePolicyError
	require.ErrorAs(t, err, &denied)
	require.ErrorContains(t, err, "shared/tools")
}
