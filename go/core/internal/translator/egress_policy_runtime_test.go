package translator_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	byotranslator "github.com/kagent-dev/kagent/go/core/internal/translator/byo"
	claudetranslator "github.com/kagent-dev/kagent/go/core/internal/translator/claude"
	codextranslator "github.com/kagent-dev/kagent/go/core/internal/translator/codex"
	kagenttranslator "github.com/kagent-dev/kagent/go/core/internal/translator/kagent"
	"github.com/kagent-dev/kagent/go/core/pkg/egressext"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
)

// This consumer uses only the public extension contract.
type defaultPolicyTranslator struct{ uri string }

var _ egressext.PolicyTranslator = defaultPolicyTranslator{}

func (d defaultPolicyTranslator) Translate(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
	if d.uri == "" {
		return draft, nil
	}
	for _, target := range draft.Requirements {
		if target.Preserve || slices.ContainsFunc(draft.Credentials, func(c egressext.Credential) bool { return c.Origin == target.Origin }) {
			continue
		}
		header, prefix := target.Header, target.Prefix
		// A generic requirement must not override the model format at a shared origin.
		if header == "" {
			if slices.ContainsFunc(draft.Requirements, func(r egressext.AuthenticationRequirement) bool { return r.Origin == target.Origin && r.Header != "" }) {
				continue
			}
			header, prefix = "authorization", "Bearer "
		}
		draft.Credentials = append(draft.Credentials, egressext.Credential{Origin: target.Origin, Header: header, Prefix: prefix, URI: d.uri})
	}
	return draft, nil
}

type agentPolicyTranslator map[types.NamespacedName]string

var _ egressext.PolicyTranslator = agentPolicyTranslator(nil)

func (a agentPolicyTranslator) Translate(agent types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
	return defaultPolicyTranslator{uri: a[agent]}.Translate(agent, draft)
}

type recordingPolicyTranslator struct{ draft egressext.PolicyDraft }

func (r *recordingPolicyTranslator) Translate(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
	r.draft = draft
	return draft, nil
}

func TestPolicyDraftMatchesPersistedApplicationGrant(t *testing.T) {
	for _, test := range []struct {
		kind            translator.HarnessType
		provider        v1alpha3.ModelProvider
		retainedDefault string
	}{
		{translator.HarnessTypeKagent, v1alpha3.ModelProviderOpenAI, "https://api.openai.com:443"},
		{translator.HarnessTypeBYO, v1alpha3.ModelProviderOpenAI, "https://api.openai.com:443"},
		{translator.HarnessTypeKagent, v1alpha3.ModelProviderAnthropic, "https://api.anthropic.com:443"},
		{translator.HarnessTypeBYO, v1alpha3.ModelProviderAnthropic, "https://api.anthropic.com:443"},
		{translator.HarnessTypeClaude, v1alpha3.ModelProviderAnthropic, ""},
		{translator.HarnessTypeCodex, v1alpha3.ModelProviderOpenAI, ""},
	} {
		t.Run(string(test.kind)+"/"+string(test.provider), func(t *testing.T) {
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
				Workload:  v1alpha3.HarnessWorkload{Image: new("example.com/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
				Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
			}}
			switch test.kind {
			case translator.HarnessTypeKagent:
				harness.Spec.Kagent = &v1alpha3.KagentHarness{}
			case translator.HarnessTypeBYO:
				harness.Spec.BYO = &v1alpha3.BYOHarness{}
			case translator.HarnessTypeClaude:
				harness.Spec.Claude = &v1alpha3.ClaudeHarness{}
			case translator.HarnessTypeCodex:
				harness.Spec.Codex = &v1alpha3.CodexHarness{}
			}
			model := modelConfig()
			model.Spec.Provider = test.provider
			model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "model-auth", "token"
			if test.provider == v1alpha3.ModelProviderOpenAI {
				model.Spec.OpenAI = &v1alpha3.OpenAIConfig{BaseURL: "https://models.example.com/v1", APIFormat: new(v1alpha3.OpenAIAPIFormatResponses)}
			} else {
				model.Spec.Anthropic = &v1alpha3.AnthropicConfig{BaseURL: "https://models.example.com/v1"}
			}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test"}, Data: map[string][]byte{"token": []byte("secret")}}
			server := remoteMCPServer("tools", "https://tools.example.com/mcp")
			template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "help",
				Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name}}}},
			}}
			collections := mockCollections(t, harness, model, server, secret, defaultWorkerPool())
			ctx := krt.TestingDummyContext{}
			settings := testRuntimeDefaults()
			adapters := map[translator.HarnessType]translator.HarnessCompiler{
				translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections, settings),
				translator.HarnessTypeBYO:    byotranslator.NewCompiler(ctx, collections, settings),
				translator.HarnessTypeClaude: claudetranslator.NewCompiler(ctx, collections, settings),
				translator.HarnessTypeCodex:  codextranslator.NewCompiler(ctx, collections, settings),
			}
			recorder := &recordingPolicyTranslator{}
			result, err := translator.NewCompiler(ctx, collections, adapters, nil, recorder).CompileAgent(t.Context(), inlineAgent(harness, template))
			require.NoError(t, err)
			want := []string{"https://models.example.com:443", "https://tools.example.com:443"}
			if test.retainedDefault != "" {
				want = append(want, test.retainedDefault)
			}
			slices.Sort(want)
			require.Equal(t, want, recorder.draft.Destinations)
			for _, requirement := range recorder.draft.Requirements {
				require.NotEqual(t, test.retainedDefault, requirement.Origin, "retaining a destination must not make it a credential target")
			}
			want = append(want, settings.GetControlPlaneUrl())
			slices.Sort(want)
			require.Equal(t, want, result.EgressDestinations)
			require.Len(t, result.Credentials, 1)
			require.Equal(t, recorder.draft.Credentials[0].URI, result.Credentials[0].URI)
		})
	}
}

func TestPolicyUsesRunnableAgentIdentity(t *testing.T) {
	policyTranslator := agentPolicyTranslator{
		{Namespace: "team-a", Name: "researcher"}: "ate-secret://provider.example/team-a/researcher",
		{Namespace: "team-a", Name: "reviewer"}:   "ate-secret://provider.example/team-a/reviewer",
		{Namespace: "team-b", Name: "researcher"}: "ate-secret://provider.example/team-b/researcher",
		{Namespace: "team-b", Name: "reviewer"}:   "ate-secret://provider.example/team-b/reviewer",
	}
	for _, namespace := range []string{"team-a", "team-b"} {
		harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: namespace}, Spec: v1alpha3.HarnessSpec{
			Kagent: &v1alpha3.KagentHarness{}, Workload: v1alpha3.HarnessWorkload{Image: new("example.com/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
			Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
		}}
		model := modelConfig()
		model.Namespace = namespace
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "shared-behavior", Namespace: namespace}, Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "help",
		}}
		pool := defaultWorkerPool()
		pool.Namespace = namespace
		collections := mockCollections(t, harness, model, template, pool)
		settings := testRuntimeDefaults()
		ctx := krt.TestingDummyContext{}
		compiler := translator.NewCompiler(ctx, collections, map[translator.HarnessType]translator.HarnessCompiler{
			translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections, settings),
		}, nil, policyTranslator)
		for _, source := range []string{"inline", "reference"} {
			for _, name := range []string{"researcher", "reviewer"} {
				t.Run(namespace+"/"+source+"/"+name, func(t *testing.T) {
					agent := inlineAgent(harness, template)
					agent.Name = name
					if source == "reference" {
						agent.Spec.Template = v1alpha3.AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: template.Name}}
					}
					result, err := compiler.CompileAgent(t.Context(), agent)
					require.NoError(t, err)
					require.Len(t, result.Credentials, 1)
					require.Equal(t, policyTranslator[types.NamespacedName{Namespace: namespace, Name: name}], result.Credentials[0].URI)
				})
			}
		}
	}
}

func TestCompileContributedCredentialsAcrossHarnesses(t *testing.T) {
	for _, test := range []struct {
		kind     translator.HarnessType
		provider v1alpha3.ModelProvider
	}{
		{translator.HarnessTypeKagent, v1alpha3.ModelProviderOpenAI},
		{translator.HarnessTypeBYO, v1alpha3.ModelProviderOpenAI},
		{translator.HarnessTypeClaude, v1alpha3.ModelProviderAnthropic},
		{translator.HarnessTypeCodex, v1alpha3.ModelProviderOpenAI},
		{translator.HarnessTypeKagent, v1alpha3.ModelProviderBedrock},
		{translator.HarnessTypeBYO, v1alpha3.ModelProviderBedrock},
		{translator.HarnessTypeClaude, v1alpha3.ModelProviderBedrock},
		{translator.HarnessTypeCodex, v1alpha3.ModelProviderBedrock},
	} {
		kind := test.kind
		t.Run(string(kind)+"/"+string(test.provider), func(t *testing.T) {
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
				Workload:  v1alpha3.HarnessWorkload{Image: new("example.com/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
				Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
			}}
			model := modelConfig()
			key := "OPENAI_API_KEY"
			switch kind {
			case translator.HarnessTypeKagent:
				harness.Spec.Kagent = &v1alpha3.KagentHarness{}
			case translator.HarnessTypeBYO:
				harness.Spec.BYO = &v1alpha3.BYOHarness{}
			case translator.HarnessTypeClaude:
				harness.Spec.Claude = &v1alpha3.ClaudeHarness{}
				model.Spec.Provider = v1alpha3.ModelProviderAnthropic
				key = "ANTHROPIC_API_KEY"
			case translator.HarnessTypeCodex:
				harness.Spec.Codex = &v1alpha3.CodexHarness{}
				model.Spec.OpenAI = &v1alpha3.OpenAIConfig{APIFormat: new(v1alpha3.OpenAIAPIFormatResponses)}
			}
			model.Spec.Provider = test.provider
			switch test.provider {
			case v1alpha3.ModelProviderBedrock:
				model.Spec.Model = "gpt-5.2"
				model.Spec.OpenAI = nil
				model.Spec.Bedrock = &v1alpha3.BedrockConfig{Region: "us-east-1"}
				key = "AWS_BEARER_TOKEN_BEDROCK"
			}
			server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://tools.example.com/mcp"}}
			policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Name: "extra", Namespace: "test"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com:8443"}}}}
			template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "help", Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name}}}},
			}}
			agent := inlineAgent(harness, template)
			agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
			collections := mockCollections(t, harness, model, server, policy, defaultWorkerPool())
			compile := func(policyTranslator egressext.PolicyTranslator) (*translator.CompileResult, error) {
				settings := testRuntimeDefaults()
				ctx := krt.TestingDummyContext{}
				compilers := map[translator.HarnessType]translator.HarnessCompiler{
					translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections, settings),
					translator.HarnessTypeBYO:    byotranslator.NewCompiler(ctx, collections, settings),
					translator.HarnessTypeClaude: claudetranslator.NewCompiler(ctx, collections, settings),
					translator.HarnessTypeCodex:  codextranslator.NewCompiler(ctx, collections, settings),
				}
				return translator.NewCompiler(ctx, collections, compilers, nil, policyTranslator).CompileAgent(t.Context(), agent)
			}
			result, err := compile(defaultPolicyTranslator{uri: "ate-secret://provider.example/token"})
			require.NoError(t, err)
			require.Len(t, result.Credentials, 2, "live EgressPolicy grants are not credential targets")
			require.Contains(t, result.Environment, corev1.EnvVar{Name: key, Value: translator.CredentialPlaceholder})
			for _, variable := range result.Environment {
				require.Nil(t, variable.ValueFrom)
			}
			switch kind {
			case translator.HarnessTypeKagent, translator.HarnessTypeBYO:
				var config configv1.ADKConfig
				require.NoError(t, runtimeconfig.Unmarshal(result.ConfigJSON, &config))
				require.Equal(t, translator.CredentialPlaceholder, config.Agent.HttpTools[0].Params.Headers["authorization"])
			case translator.HarnessTypeClaude:
				var config claudeconfig.Config
				require.NoError(t, json.Unmarshal(result.ConfigJSON, &config))
				require.Equal(t, translator.CredentialPlaceholder, config.MCPServers["tools"].Headers["authorization"])
			case translator.HarnessTypeCodex:
				var config codexconfig.Config
				require.NoError(t, json.Unmarshal(result.ConfigJSON, &config))
				require.Equal(t, translator.CredentialPlaceholder, config.MCPServers["tools"].Headers["authorization"])
			}
			first, err := result.Digest()
			require.NoError(t, err)
			again, err := compile(defaultPolicyTranslator{uri: "ate-secret://provider.example/token"})
			require.NoError(t, err)
			same, err := again.Digest()
			require.NoError(t, err)
			require.Equal(t, first, same)
			changed, err := compile(defaultPolicyTranslator{uri: "ate-secret://provider.example/next"})
			require.NoError(t, err)
			second, err := changed.Digest()
			require.NoError(t, err)
			require.NotEqual(t, first, second)
			require.Equal(t, result.EgressDestinations, changed.EgressDestinations)
			if kind == translator.HarnessTypeKagent || kind == translator.HarnessTypeBYO {
				baseline, err := compile(nil)
				require.NoError(t, err)
				require.Equal(t, baseline.EgressDestinations, result.EgressDestinations)
				require.Empty(t, baseline.Credentials)
				noop, err := compile(defaultPolicyTranslator{})
				require.NoError(t, err)
				require.Equal(t, baseline.Revision, noop.Revision)
				nilDigest, err := baseline.Digest()
				require.NoError(t, err)
				noopDigest, err := noop.Digest()
				require.NoError(t, err)
				require.Equal(t, nilDigest, noopDigest)
				require.NotContains(t, string(baseline.ConfigJSON), translator.CredentialPlaceholder)
			} else {
				_, err := compile(nil)
				require.ErrorContains(t, err, "configure apiKeySecret")
				_, err = compile(defaultPolicyTranslator{})
				require.ErrorContains(t, err, "requires a gateway credential")
			}
			model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "model-auth", "token"
			secretKey := "token"
			if test.provider == v1alpha3.ModelProviderBedrock {
				model.Spec.APIKeySecretKey, secretKey = "", "AWS_BEARER_TOKEN_BEDROCK"
			}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test"}, Data: map[string][]byte{secretKey: []byte("model-secret")}}
			collections = mockCollections(t, harness, model, server, policy, defaultWorkerPool(), secret)
			baseline, err := compile(nil)
			require.NoError(t, err)
			if test.provider == v1alpha3.ModelProviderBedrock {
				require.Equal(t, []egress.Credential{{
					Origin: "https://bedrock-runtime.us-east-1.amazonaws.com:443", Header: "authorization", Prefix: "Bearer ",
					URI: "ate-secret://k8s.io/default/test/model-auth/AWS_BEARER_TOKEN_BEDROCK",
				}}, baseline.Credentials)
			}
			noop, err := compile(defaultPolicyTranslator{})
			require.NoError(t, err)
			require.Equal(t, baseline.Revision, noop.Revision)
			defaultDigest, err := baseline.Digest()
			require.NoError(t, err)
			noopDigest, err := noop.Digest()
			require.NoError(t, err)
			require.Equal(t, defaultDigest, noopDigest)
			withDefault, err := compile(defaultPolicyTranslator{uri: "ate-secret://provider.example/token"})
			require.NoError(t, err)
			require.Contains(t, withDefault.Credentials, baseline.Credentials[0], "default credentials preserve explicit model authentication")
		})
	}
}

func TestPolicyResolvesAuxiliaryModelsBeforeHarnessRendering(t *testing.T) {
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		Kagent: &v1alpha3.KagentHarness{
			Memory:     &v1alpha3.KagentHarnessMemory{ModelConfigRef: corev1.LocalObjectReference{Name: "embedding"}},
			Compaction: &v1alpha3.KagentHarnessCompaction{Summarizer: &v1alpha3.KagentHarnessSummarizer{ModelConfigRef: &corev1.LocalObjectReference{Name: "summary"}}},
		},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}},
	}}
	root, embedding, summary := modelConfig(), modelConfig(), modelConfig()
	embedding.Name, summary.Name = "embedding", "summary"
	embedding.Spec.OpenAI = &v1alpha3.OpenAIConfig{BaseURL: "https://embeddings.example.com/v1"}
	summary.Spec.OpenAI = &v1alpha3.OpenAIConfig{BaseURL: "https://summary.example.com/v1"}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: root.Name}}}
	collections := mockCollections(t, harness, root, embedding, summary, defaultWorkerPool())
	adapter := &testHarnessCompiler{}
	compiler := translator.NewCompiler(krt.TestingDummyContext{}, collections, map[translator.HarnessType]translator.HarnessCompiler{translator.HarnessTypeKagent: adapter}, testHarnessImages, defaultPolicyTranslator{uri: "ate-secret://provider.example/token"})
	result, err := compiler.CompileAgent(t.Context(), inlineAgent(harness, template))
	require.NoError(t, err)
	require.NotNil(t, adapter.input.SummarizerModel)
	require.NotNil(t, adapter.input.MemoryModel)
	require.True(t, adapter.input.Policy.ModelUsesGateway(adapter.input.Root.ResolvedModelConfig), "authentication must be resolved when rendering starts")
	require.True(t, adapter.input.Policy.ModelUsesGateway(adapter.input.SummarizerModel))
	require.True(t, adapter.input.Policy.ModelUsesGateway(adapter.input.MemoryModel))
	require.Len(t, result.Credentials, 3)
	require.Equal(t, []string{"https://api.openai.com:443", "https://embeddings.example.com:443", "https://summary.example.com:443"}, result.EgressDestinations)
}

func TestHarnessLiteralKeyOverridesModelSecretBinding(t *testing.T) {
	for _, test := range []struct {
		name, env string
		model     v1alpha3.ModelConfigSpec
		secret    map[string][]byte
	}{
		{"bedrock", "AWS_BEARER_TOKEN_BEDROCK",
			v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "claude", Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"}, APIKeySecret: "model-auth"},
			map[string][]byte{"AWS_BEARER_TOKEN_BEDROCK": []byte("secret")}},
		{"openai", "OPENAI_API_KEY",
			v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4o", APIKeySecret: "model-auth", APIKeySecretKey: "token"},
			map[string][]byte{"token": []byte("secret")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
				Kagent:    &v1alpha3.KagentHarness{},
				Env:       []v1alpha3.RuntimeEnvVar{{Name: test.env, Value: "explicit"}},
				Workload:  v1alpha3.HarnessWorkload{Image: new("example.com/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
				Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
			}}
			model := modelConfig()
			model.Spec = test.model
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test"}, Data: test.secret}
			template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "help",
			}}
			agent := inlineAgent(harness, template)
			collections := mockCollections(t, harness, model, secret, defaultWorkerPool())
			ctx := krt.TestingDummyContext{}
			compilers := map[translator.HarnessType]translator.HarnessCompiler{
				translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections, testRuntimeDefaults()),
			}
			result, err := translator.NewCompiler(ctx, collections, compilers, nil, nil).CompileAgent(t.Context(), agent)
			require.NoError(t, err)
			require.Contains(t, result.Environment, corev1.EnvVar{Name: test.env, Value: "explicit"})
			require.Empty(t, result.Credentials, "the gateway must not replace an explicit Harness key")
		})
	}
}
