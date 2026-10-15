package translator

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/egressext"
)

type policyTranslatorFunc func(types.NamespacedName, egressext.PolicyDraft) (egressext.PolicyDraft, error)

func (f policyTranslatorFunc) Translate(agent types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
	return f(agent, draft)
}

var _ egressext.PolicyTranslator = policyTranslatorFunc(nil)

func TestPolicyDraftTargets(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "model-key", APIKeySecretKey: "token"})
	input.Root.MCPTools = []ResolvedMCPTool{
		{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://MCP.Example.com./tools"}}},
		{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "http://local.example.com/tools"}}},
	}
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic}).Root}}
	extension := policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
		require.Len(t, draft.Credentials, 1)
		require.Contains(t, draft.Requirements, egressext.AuthenticationRequirement{Origin: "https://api.anthropic.com:443", Header: "x-api-key"})
		require.Contains(t, draft.Requirements, egressext.AuthenticationRequirement{Origin: "https://mcp.example.com:443"})
		require.Contains(t, draft.Destinations, "http://local.example.com:80")
		draft.Credentials = append(draft.Credentials, egressext.Credential{Origin: "https://api.anthropic.com", Header: "x-api-key", URI: "ate-secret://provider.example/token"})
		return draft, nil
	})
	policy, err := ResolvePolicy(input, nil, extension)
	require.NoError(t, err)
	require.Len(t, policy.access.Credentials, 2)
	rendered, err := policy.RenderEnvironment([]corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "model-key", "token")})
	require.NoError(t, err)
	require.Contains(t, rendered, corev1.EnvVar{Name: "ANTHROPIC_API_KEY", Value: CredentialPlaceholder})
	require.Equal(t, "ate-secret://k8s.io/default/team/model-key/token", policy.access.Credentials[1].URI)
}

func TestPolicyPreservesExplicitAuthentication(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*HarnessInput)
	}{
		{"passthrough", func(input *HarnessInput) { input.Root.ResolvedModelConfig.Config.Spec.APIKeyPassthrough = true }},
		{"model headers", func(input *HarnessInput) {
			input.Root.ResolvedModelConfig.Config.Spec.DefaultHeaders = map[string]string{"Authorization": "literal"}
		}},
		{"harness key", func(input *HarnessInput) {
			input.Harness.Spec.Env = []v1alpha3.RuntimeEnvVar{{Name: "OPENAI_API_KEY", Value: "literal"}}
		}},
		{"literal MCP header", func(input *HarnessInput) {
			input.Root.MCPTools = []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://api.openai.com/mcp", HeadersFrom: []v1alpha3.ValueRef{{Name: "X-Trace", Value: "literal"}}}}}}
		}},
		{"ConfigMap MCP header", func(input *HarnessInput) {
			input.Root.MCPTools = []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://api.openai.com/mcp", HeadersFrom: []v1alpha3.ValueRef{{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.ConfigMapValueSource, Name: "auth", Key: "token"}}}}}}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI})
			test.configure(input)
			_, err := ResolvePolicy(input, nil, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
				for _, requirement := range draft.Requirements {
					require.True(t, requirement.Preserve)
				}
				return draft, nil
			}))
			require.NoError(t, err)
			_, err = ResolvePolicy(input, nil, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
				draft.Credentials = append(draft.Credentials, egressext.Credential{Origin: "https://api.openai.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://provider.example/token"})
				return draft, nil
			}))
			require.ErrorContains(t, err, "preserves explicit authentication")
		})
	}
}

func TestPolicyRejectsInvalidBindings(t *testing.T) {
	base := egressext.Credential{Origin: "https://api.openai.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://provider.example/token"}
	for _, test := range []struct {
		name   string
		change func(*egressext.Credential)
		want   string
	}{
		{"unknown origin", func(c *egressext.Credential) { c.Origin = "https://unrelated.example.com" }, "not explicitly allowed"},
		{"different port", func(c *egressext.Credential) { c.Origin = "https://api.openai.com:8443" }, "not explicitly allowed"},
		{"HTTP", func(c *egressext.Credential) { c.Origin = "http://api.openai.com" }, "not explicitly allowed"},
		{"wildcard", func(c *egressext.Credential) { c.Origin = "https://*.openai.com" }, "exact HTTP(S) DNS origin"},
		{"routing header", func(c *egressext.Credential) { c.Header = "Host" }, "invalid credential injection header"},
		{"invalid prefix", func(c *egressext.Credential) { c.Prefix = "Bearer\n" }, "invalid credential injection header"},
		{"invalid URI", func(c *egressext.Credential) { c.URI = "https://provider.example/token" }, "invalid credential URI"},
		{"model header mismatch", func(c *egressext.Credential) { c.Header = "x-api-key" }, "must match a model header and prefix"},
		{"model prefix mismatch", func(c *egressext.Credential) { c.Prefix = "Basic " }, "must match a model header and prefix"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI})
			input.Root.MCPTools = []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://api.openai.com/mcp"}}}}
			binding := base
			test.change(&binding)
			_, err := ResolvePolicy(input, nil, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
				draft.Credentials = append(draft.Credentials, binding)
				return draft, nil
			}))
			require.ErrorContains(t, err, test.want)
		})
	}
	failure := errors.New("policy unavailable")
	_, err := ResolvePolicy(credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI}), nil, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
		return draft, failure
	}))
	require.ErrorIs(t, err, failure)
}

func TestPolicyCannotWeakenDraft(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*egressext.PolicyDraft)
		want   string
	}{
		{"destinations", func(d *egressext.PolicyDraft) { d.Destinations[0] = "https://other.example.com:443" }, "cannot change destinations"},
		{"header", func(d *egressext.PolicyDraft) { d.Requirements[0].Header = "x-other" }, "cannot change destinations"},
		{"preserve", func(d *egressext.PolicyDraft) { d.Requirements[0].Preserve = true }, "cannot change destinations"},
		{"required", func(d *egressext.PolicyDraft) { d.Requirements[0].Required = true }, "cannot change destinations"},
		{"remove binding", func(d *egressext.PolicyDraft) { d.Credentials = nil }, "cannot remove or replace"},
		{"replace binding", func(d *egressext.PolicyDraft) { d.Credentials[0].URI = "ate-secret://provider.example/changed" }, "cannot remove or replace"},
		{"conflict", func(d *egressext.PolicyDraft) {
			binding := d.Credentials[0]
			binding.URI = "ate-secret://provider.example/changed"
			d.Credentials = append(d.Credentials, binding)
		}, "conflicting credentials"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "auth", APIKeySecretKey: "token"})
			before, err := ResolvePolicy(input, nil, nil)
			require.NoError(t, err)
			_, err = ResolvePolicy(input, nil, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
				test.change(&draft)
				return draft, nil
			}))
			require.ErrorContains(t, err, test.want)
			after, err := ResolvePolicy(input, nil, nil)
			require.NoError(t, err)
			require.Equal(t, before, after, "extension mutation must not affect subsequent compilation")
		})
	}
}

func TestPolicyAuxiliaryModels(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI})
	input.MemoryModel = &ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "embedding", Namespace: "team"}, Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://embeddings.example.com/v1"}}}}
	input.SummarizerModel = &ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "summary", Namespace: "team"}, Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic}}}
	_, err := ResolvePolicy(input, nil, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
		require.Len(t, draft.Requirements, 3)
		require.Contains(t, draft.Destinations, "https://embeddings.example.com:443")
		require.Contains(t, draft.Destinations, "https://api.anthropic.com:443")
		return draft, nil
	}))
	require.NoError(t, err)
}

func TestPolicyGeminiRequirements(t *testing.T) {
	for _, secret := range []string{"", "model-key"} {
		t.Run("secret="+secret, func(t *testing.T) {
			input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderGemini, APIKeySecret: secret, APIKeySecretKey: "token"})
			draft, err := policyDraft(input, policyModels(input), nil)
			require.NoError(t, err)
			require.NotEmpty(t, draft.Requirements)
			for _, r := range draft.Requirements {
				require.Equal(t, secret == "", r.Preserve)
			}
		})
	}
}

func TestPolicyRequiredAuthenticationIsNotAPlaceholderSentinel(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI})
	input.Harness.Spec.Env = []v1alpha3.RuntimeEnvVar{{Name: "OPENAI_API_KEY", Value: CredentialPlaceholder}}
	policy, err := ResolvePolicy(input, nil, nil)
	require.NoError(t, err)
	_, err = policy.RenderEnvironment([]corev1.EnvVar{{Name: "OPENAI_API_KEY", Value: CredentialPlaceholder}})
	require.NoError(t, err, "a literal value must not create an authentication requirement")
	input.Harness.Spec.Env = nil
	input.Harness.Spec.Codex = &v1alpha3.CodexHarness{}
	_, err = ResolvePolicy(input, requiredModelCredentials{v1alpha3.ModelProviderOpenAI}, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
		require.True(t, slices.ContainsFunc(draft.Requirements, func(r egressext.AuthenticationRequirement) bool { return r.Required }))
		return draft, nil
	}))
	require.ErrorContains(t, err, "requires a gateway credential")
}

func TestPolicyRetainsDestinationsWithoutCredentialRequirements(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Ollama: &v1alpha3.OllamaConfig{Host: "daemon.example.com:11434"}})
	policy, err := ResolvePolicy(input, nil, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
		require.Equal(t, []string{"http://daemon.example.com:11434"}, draft.Destinations)
		require.Empty(t, draft.Requirements)
		return draft, nil
	}))
	require.NoError(t, err)
	require.Equal(t, []string{"http://daemon.example.com:11434"}, policy.access.Destinations)
}

func TestPolicyModelDestinations(t *testing.T) {
	for _, test := range []struct {
		name string
		spec v1alpha3.ModelConfigSpec
		want []string
	}{
		{"Mistral default", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderMistral}, []string{"https://api.mistral.ai:443"}},
		{"Mistral override", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderMistral, Mistral: &v1alpha3.MistralConfig{BaseURL: new("https://mistral.example.com/v1")}}, []string{"https://mistral.example.com:443"}},
		{"Ollama bare host", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Ollama: &v1alpha3.OllamaConfig{Host: "daemon.example.com:11434"}}, []string{"http://daemon.example.com:11434"}},
		{"Ollama explicit scheme", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Ollama: &v1alpha3.OllamaConfig{Host: "http://daemon.example.com:11434"}}, []string{"http://daemon.example.com:11434"}},
		{"Ollama no host", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Ollama: &v1alpha3.OllamaConfig{}}, nil},
		{"Ollama cloud", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Model: "minimax-m3:cloud", APIKeySecret: "auth", APIKeySecretKey: "token", Ollama: &v1alpha3.OllamaConfig{}}, []string{"https://api.ollama.com:443"}},
		{"Ollama local with key", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2", APIKeySecret: "auth", APIKeySecretKey: "token", Ollama: &v1alpha3.OllamaConfig{Host: "daemon.example.com:11434"}}, []string{"http://daemon.example.com:11434"}},
		{"SAP endpoints", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderSAPAICore, SAPAICore: &v1alpha3.SAPAICoreConfig{BaseURL: "https://sap.example.com/v1", AuthURL: "https://auth.example.com/token"}}, []string{"https://auth.example.com:443", "https://sap.example.com:443"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := credentialInput(test.spec)
			policy, err := ResolvePolicy(input, nil, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
				require.Equal(t, test.want, draft.Destinations)
				return draft, nil
			}))
			require.NoError(t, err)
			require.Equal(t, test.want, policy.access.Destinations)
		})
	}
}

func TestPolicyPreservesExistingBindingClientBehavior(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic})
	input.Root.MCPTools = []ResolvedMCPTool{
		{Server: &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://api.anthropic.com/mcp", HeadersFrom: []v1alpha3.ValueRef{
			{Name: "x-api-key", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "auth", Key: "token"}},
		}}}},
		{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://api.anthropic.com/other"}}},
	}
	policy, err := ResolvePolicy(input, nil, nil)
	require.NoError(t, err)
	environment, err := policy.RenderEnvironment(nil)
	require.NoError(t, err)
	require.Empty(t, environment, "existing bindings do not select SDK authentication")
	headers, err := policy.MCPHeaders("https://api.anthropic.com/other", nil)
	require.NoError(t, err)
	require.Empty(t, headers, "existing bindings do not add client headers")
	unchanged, err := ResolvePolicy(input, nil, policyTranslatorFunc(func(_ types.NamespacedName, draft egressext.PolicyDraft) (egressext.PolicyDraft, error) {
		return draft, nil
	}))
	require.NoError(t, err)
	require.Equal(t, policy, unchanged)
}

type requiredModelCredentials []v1alpha3.ModelProvider

func (r requiredModelCredentials) ModelRequirements(model *ResolvedModelConfig) ModelRequirements {
	return ModelRequirements{CredentialRequired: slices.Contains(r, model.Config.Spec.Provider)}
}
func (requiredModelCredentials) Compile(context.Context, *HarnessInput) (*CompileResult, error) {
	panic("policy resolution must not render a client")
}

func TestMergeRequirements(t *testing.T) {
	const origin, other = "https://models.example.com:443", "https://tools.example.com:443"
	merged := mergeRequirements([]egressext.AuthenticationRequirement{
		{Origin: other},
		{Origin: origin, Header: "authorization", Prefix: "Bearer ", Required: true},
		{Origin: origin, Preserve: true},
		{Origin: origin, Header: "authorization", Prefix: "Bearer "},
		{Origin: origin, Header: "x-api-key"},
	})
	require.Equal(t, []egressext.AuthenticationRequirement{
		{Origin: origin, Preserve: true},
		{Origin: origin, Header: "authorization", Prefix: "Bearer ", Preserve: true, Required: true},
		{Origin: origin, Header: "x-api-key", Preserve: true},
		{Origin: other},
	}, merged)
}
