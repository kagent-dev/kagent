package translator

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestCompileActorJWTUnsupportedProviders(t *testing.T) {
	for _, spec := range []v1alpha3.ModelConfigSpec{
		{Provider: v1alpha3.ModelProviderBedrock, Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"}},
		{Provider: v1alpha3.ModelProviderGemini},
		{Provider: v1alpha3.ModelProviderOllama, Ollama: &v1alpha3.OllamaConfig{}},
	} {
		t.Run(string(spec.Provider), func(t *testing.T) {
			spec.Model = "test:cloud"
			spec.ActorJWT = &v1alpha3.ActorJWT{Audiences: []string{"model"}}
			environment, bindings, passthrough, err := CompileCredentials(credentialInput(spec), nil, nil)
			require.ErrorContains(t, err, "does not support actorJwt")
			require.Empty(t, environment)
			require.Empty(t, bindings)
			require.Empty(t, passthrough)
		})
	}
}

func TestCompileActorJWTCredentials(t *testing.T) {
	token := &v1alpha3.ActorJWT{Audiences: []string{"service"}}
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, ActorJWT: token})
	input.Root.MCPTools = []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://mcp.example.com/tools", ActorJWT: token}}}}
	placeholder, err := ActorJWTEnvironment(input.Root.ResolvedModelConfig)
	require.NoError(t, err)
	environment, bindings, passthrough, err := CompileCredentials(input, nil, []corev1.EnvVar{placeholder})
	require.NoError(t, err)
	require.Empty(t, passthrough)
	require.Equal(t, []corev1.EnvVar{{Name: "ANTHROPIC_API_KEY", Value: CredentialPlaceholder}}, environment)
	require.Len(t, bindings, 2)
	require.Equal(t, "x-api-key", bindings[0].Header)
	require.Empty(t, bindings[0].Prefix)
	require.Equal(t, "authorization", bindings[1].Header)
	require.Equal(t, "Bearer ", bindings[1].Prefix)
	for _, binding := range bindings {
		require.Empty(t, binding.URI)
		require.Equal(t, []string{"service"}, binding.ActorJWT.Audiences)
		require.Equal(t, int64(900), binding.ActorJWT.ExpirationSeconds)
	}
	policy := &v1alpha3.EgressPolicy{Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://api.example.com", Authentication: &v1alpha3.EgressAuthentication{Header: "X-Actor", ActorJWT: token}}}}}
	access, err := compileEgressPolicy("team", policy, func(string) (*corev1.Secret, error) { t.Fatal("Actor JWT must not resolve a Secret"); return nil, nil })
	require.NoError(t, err)
	require.Equal(t, "x-actor", access.Credentials[0].Header)
	require.Equal(t, bindings[0].ActorJWT, access.Credentials[0].ActorJWT)
	input.Root.ResolvedModelConfig.Config.Spec.DefaultHeaders = map[string]string{"X-Api-Key": "conflict"}
	environment, bindings, passthrough, err = CompileCredentials(input, nil, nil)
	require.Empty(t, environment)
	require.Empty(t, bindings)
	require.Empty(t, passthrough)
	require.ErrorContains(t, err, "defaultHeaders")
	input.Root.ResolvedModelConfig.Config.Spec.DefaultHeaders = nil
	input.Root.MCPTools[0].Server.Spec.HeadersFrom = []v1alpha3.ValueRef{{Name: "AUTHORIZATION", Value: "conflict"}}
	environment, bindings, passthrough, err = CompileCredentials(input, nil, nil)
	require.Empty(t, environment)
	require.Empty(t, bindings)
	require.Empty(t, passthrough)
	require.ErrorContains(t, err, "headersFrom")
}
