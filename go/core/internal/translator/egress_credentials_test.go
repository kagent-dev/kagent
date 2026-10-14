package translator

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egressext"
)

type credentialBinderFunc func([]egressext.CredentialTarget, []egressext.Credential) ([]egressext.Credential, error)

func (f credentialBinderFunc) Bindings(targets []egressext.CredentialTarget, existing []egressext.Credential) ([]egressext.Credential, error) {
	return f(targets, existing)
}

var _ egressext.CredentialBinder = credentialBinderFunc(nil)

func TestEgressCredentialBinderTargets(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "model-key", APIKeySecretKey: "token"})
	input.Root.MCPTools = []ResolvedMCPTool{
		{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://MCP.Example.com./tools"}}},
		{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "http://local.example.com/tools"}}},
	}
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic}).Root}}
	binder := credentialBinderFunc(func(targets []egressext.CredentialTarget, existing []egressext.Credential) ([]egressext.Credential, error) {
		require.Equal(t, []egressext.CredentialTarget{
			{Origin: "https://api.anthropic.com:443", Header: "x-api-key"},
			{Origin: "https://api.openai.com:443", Header: "authorization", Prefix: "Bearer "},
			{Origin: "https://mcp.example.com:443"},
		}, targets)
		require.Len(t, existing, 1)
		require.Equal(t, egressext.Credential{
			Origin: "https://api.openai.com:443", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/model-key/token",
		}, existing[0])
		// A binder cannot mutate the bindings used by the compiler.
		existing[0].URI = "ate-secret://provider.example/changed"
		return []egressext.Credential{{Origin: targets[0].Origin, Header: targets[0].Header, URI: "ate-secret://provider.example/token"}}, nil
	})
	environment, bindings, added, _, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "model-key", "token")}, binder)
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	require.Equal(t, []egress.Credential{{
		Origin: "https://api.anthropic.com:443", Header: "x-api-key", URI: "ate-secret://provider.example/token",
	}}, added)
	require.Equal(t, "ate-secret://k8s.io/default/team/model-key/token", bindings[1].URI)
	require.Contains(t, environment, corev1.EnvVar{Name: "ANTHROPIC_API_KEY", Value: CredentialPlaceholder})
}

func TestEgressCredentialBinderPreservesExplicitAuthentication(t *testing.T) {
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
			input.Root.MCPTools = []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://api.openai.com/mcp", HeadersFrom: []v1alpha3.ValueRef{{Name: "Authorization", Value: "literal"}}}}}}
		}},
		{"ConfigMap MCP header", func(input *HarnessInput) {
			input.Root.MCPTools = []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://api.openai.com/mcp", HeadersFrom: []v1alpha3.ValueRef{{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.ConfigMapValueSource, Name: "auth", Key: "token"}}}}}}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI})
			test.configure(input)
			binder := credentialBinderFunc(func(targets []egressext.CredentialTarget, _ []egressext.Credential) ([]egressext.Credential, error) {
				require.Empty(t, targets)
				return nil, nil
			})
			_, bindings, _, _, err := CompileCredentials(input, nil, nil, binder)
			require.NoError(t, err)
			require.Empty(t, bindings)
			binder = credentialBinderFunc(func([]egressext.CredentialTarget, []egressext.Credential) ([]egressext.Credential, error) {
				return []egressext.Credential{{Origin: "https://api.openai.com", Header: "authorization", URI: "ate-secret://provider.example/token"}}, nil
			})
			_, bindings, _, _, err = CompileCredentials(input, nil, nil, binder)
			require.ErrorContains(t, err, "not an eligible application target")
			require.Empty(t, bindings)
		})
	}
}

func TestEgressCredentialBinderRejectsInvalidBindings(t *testing.T) {
	base := egressext.Credential{Origin: "https://api.openai.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://provider.example/token"}
	for _, test := range []struct {
		name   string
		change func(*egressext.Credential)
		want   string
	}{
		{"unknown origin", func(c *egressext.Credential) { c.Origin = "https://unrelated.example.com" }, "not an eligible application target"},
		{"different port", func(c *egressext.Credential) { c.Origin = "https://api.openai.com:8443" }, "not an eligible application target"},
		{"HTTP", func(c *egressext.Credential) { c.Origin = "http://api.openai.com" }, "not an eligible application target"},
		{"wildcard", func(c *egressext.Credential) { c.Origin = "https://*.openai.com" }, "exact HTTP(S) DNS origin"},
		{"routing header", func(c *egressext.Credential) { c.Header = "Host" }, "invalid credential injection header"},
		{"invalid prefix", func(c *egressext.Credential) { c.Prefix = "Bearer\n" }, "invalid credential injection header"},
		{"invalid URI", func(c *egressext.Credential) { c.URI = "https://provider.example/token" }, "invalid credential URI"},
		{"model header mismatch", func(c *egressext.Credential) { c.Header = "x-api-key" }, "must match a model header and prefix"},
		{"model prefix mismatch", func(c *egressext.Credential) { c.Prefix = "Basic " }, "must match a model header and prefix"},
		{"conflict", func(c *egressext.Credential) {}, "conflicting credentials"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "auth", APIKeySecretKey: "token"})
			binding := base
			test.change(&binding)
			binder := credentialBinderFunc(func([]egressext.CredentialTarget, []egressext.Credential) ([]egressext.Credential, error) {
				return []egressext.Credential{binding}, nil
			})
			_, bindings, _, _, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "auth", "token")}, binder)
			require.ErrorContains(t, err, test.want)
			require.Empty(t, bindings)
		})
	}
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI})
	failure := errors.New("binder unavailable")
	_, bindings, _, _, err := CompileCredentials(input, nil, nil, credentialBinderFunc(func([]egressext.CredentialTarget, []egressext.Credential) ([]egressext.Credential, error) {
		return nil, failure
	}))
	require.ErrorIs(t, err, failure)
	require.Empty(t, bindings)
}

func TestEgressCredentialBinderExtraModels(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI})
	embedding := &ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "embedding", Namespace: "team"}, Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://embeddings.example.com/v1"}}}}
	binder := credentialBinderFunc(func(targets []egressext.CredentialTarget, _ []egressext.Credential) ([]egressext.Credential, error) {
		require.Equal(t, []egressext.CredentialTarget{{Origin: "https://api.openai.com:443", Header: "authorization", Prefix: "Bearer "}, {Origin: "https://embeddings.example.com:443", Header: "authorization", Prefix: "Bearer "}}, targets)
		return nil, nil
	})
	_, bindings, _, _, err := CompileCredentials(input, []*ResolvedModelConfig{input.Root.ResolvedModelConfig, embedding}, nil, binder)
	require.NoError(t, err)
	require.Empty(t, bindings)
}

func TestEgressCredentialBinderGeminiTargets(t *testing.T) {
	for _, secret := range []string{"", "model-key"} {
		t.Run("secret="+secret, func(t *testing.T) {
			input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderGemini, APIKeySecret: secret})
			input.Root.MCPTools = []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://generativelanguage.googleapis.com/mcp"}}}}
			targets := eligibleCredentialTargets(input, []*ResolvedModelConfig{input.Root.ResolvedModelConfig})
			if secret == "" {
				require.Empty(t, targets, "MCP targets must not bypass the keyless Gemini exclusion")
			} else {
				require.Equal(t, []egressext.CredentialTarget{{Origin: "https://generativelanguage.googleapis.com:443", Header: "x-goog-api-key"}}, targets)
			}
		})
	}
}

func TestEgressCredentialBinderCannotMutateValidationTargets(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*egressext.CredentialTarget)
		want   string
	}{
		{"origin", func(target *egressext.CredentialTarget) { target.Origin = "https://other.example.com:443" }, "not an eligible application target"},
		{"header", func(target *egressext.CredentialTarget) { target.Header = "x-other-key" }, "must match a model header and prefix"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI})
			binder := credentialBinderFunc(func(targets []egressext.CredentialTarget, _ []egressext.Credential) ([]egressext.Credential, error) {
				require.Len(t, targets, 1)
				test.change(&targets[0])
				return []egressext.Credential{{Origin: targets[0].Origin, Header: targets[0].Header, Prefix: targets[0].Prefix, URI: "ate-secret://provider.example/token"}}, nil
			})
			_, bindings, _, _, err := CompileCredentials(input, nil, nil, binder)
			require.ErrorContains(t, err, test.want)
			require.Empty(t, bindings)
		})
	}
}
