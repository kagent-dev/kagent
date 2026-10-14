package translator_test

import (
	"encoding/json"
	"slices"
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
	claudetranslator "github.com/kagent-dev/kagent/go/core/internal/translator/claude"
	codextranslator "github.com/kagent-dev/kagent/go/core/internal/translator/codex"
	kagenttranslator "github.com/kagent-dev/kagent/go/core/internal/translator/kagent"
	"github.com/kagent-dev/kagent/go/core/pkg/egressext"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
)

// This consumer uses only the public extension contract.
type defaultCredentialBinder struct{ uri string }

var _ egressext.CredentialBinder = defaultCredentialBinder{}

func (d defaultCredentialBinder) Bindings(targets []egressext.CredentialTarget, existing []egressext.Credential) ([]egressext.Credential, error) {
	if d.uri == "" {
		return nil, nil
	}
	var added []egressext.Credential
	for _, target := range targets {
		if slices.ContainsFunc(existing, func(c egressext.Credential) bool { return c.Origin == target.Origin }) {
			continue
		}
		header, prefix := target.Header, target.Prefix
		if header == "" {
			header, prefix = "authorization", "Bearer "
		}
		added = append(added, egressext.Credential{Origin: target.Origin, Header: header, Prefix: prefix, URI: d.uri})
	}
	return added, nil
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
			compile := func(binder egressext.CredentialBinder) (*translator.CompileResult, error) {
				settings := testRuntimeDefaults()
				ctx := krt.TestingDummyContext{}
				compilers := map[translator.HarnessType]translator.HarnessCompiler{
					translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections, settings, binder),
					translator.HarnessTypeBYO:    byotranslator.NewCompiler(ctx, collections, settings, binder),
					translator.HarnessTypeClaude: claudetranslator.NewCompiler(ctx, collections, settings, binder),
					translator.HarnessTypeCodex:  codextranslator.NewCompiler(ctx, collections, settings, binder),
				}
				return translator.NewCompiler(ctx, collections, compilers, nil).CompileAgent(t.Context(), agent)
			}
			result, err := compile(defaultCredentialBinder{uri: "ate-secret://provider.example/token"})
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
			again, err := compile(defaultCredentialBinder{uri: "ate-secret://provider.example/token"})
			require.NoError(t, err)
			same, err := again.Digest()
			require.NoError(t, err)
			require.Equal(t, first, same)
			changed, err := compile(defaultCredentialBinder{uri: "ate-secret://provider.example/next"})
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
				noop, err := compile(defaultCredentialBinder{})
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
				require.Error(t, err, "credentials are still required without an extension")
				_, err = compile(defaultCredentialBinder{})
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
			noop, err := compile(defaultCredentialBinder{})
			require.NoError(t, err)
			require.Equal(t, baseline.Revision, noop.Revision)
			defaultDigest, err := baseline.Digest()
			require.NoError(t, err)
			noopDigest, err := noop.Digest()
			require.NoError(t, err)
			require.Equal(t, defaultDigest, noopDigest)
			withDefault, err := compile(defaultCredentialBinder{uri: "ate-secret://provider.example/token"})
			require.NoError(t, err)
			require.Contains(t, withDefault.Credentials, baseline.Credentials[0], "default credentials preserve explicit model authentication")
		})
	}
}
