package translator

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

func TestResolvedPolicyCredentialDestinations(t *testing.T) {
	for _, test := range []struct {
		name                      string
		spec                      v1alpha3.ModelConfigSpec
		env, host, header, prefix string
	}{
		{"OpenAI", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI}, "OPENAI_API_KEY", "api.openai.com", "authorization", "Bearer "},
		{"OpenAI override", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://models.example.com/v1"}}, "OPENAI_API_KEY", "models.example.com", "authorization", "Bearer "},
		{"Anthropic", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic}, "ANTHROPIC_API_KEY", "api.anthropic.com", "x-api-key", ""},
		{"Azure", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAzureOpenAI, AzureOpenAI: &v1alpha3.AzureOpenAIConfig{Endpoint: "https://team.openai.azure.com"}}, "AZURE_OPENAI_API_KEY", "team.openai.azure.com", "api-key", ""},
		{"Gemini", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderGemini}, "GOOGLE_API_KEY", "generativelanguage.googleapis.com", "x-goog-api-key", ""},
		{"Foundry OpenAI", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderFoundry, Foundry: &v1alpha3.FoundryConfig{Endpoint: "https://team.services.ai.azure.com"}}, "FOUNDRY_API_KEY", "team.services.ai.azure.com", "api-key", ""},
		{"Foundry Anthropic", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderFoundry, Foundry: &v1alpha3.FoundryConfig{Endpoint: "https://team.services.ai.azure.com", APIFormat: v1alpha3.FoundryAPIFormatAnthropic}}, "FOUNDRY_API_KEY", "team.services.ai.azure.com", "x-api-key", ""},
		{"Mistral", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderMistral}, "MISTRAL_API_KEY", "api.mistral.ai", "authorization", "Bearer "},
		{"Mistral override", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderMistral, Mistral: &v1alpha3.MistralConfig{BaseURL: new("https://mistral.example.com/v1")}}, "MISTRAL_API_KEY", "mistral.example.com", "authorization", "Bearer "},
		{"Bedrock bearer", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"}}, "AWS_BEARER_TOKEN_BEDROCK", "bedrock-runtime.us-east-1.amazonaws.com", "authorization", "Bearer "},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.spec.APIKeySecret, test.spec.APIKeySecretKey = "auth", "token"
			if test.spec.Provider == v1alpha3.ModelProviderBedrock {
				test.spec.APIKeySecretKey = test.env
			}
			input := credentialInput(test.spec)
			environment := []corev1.EnvVar{credentialEnv(test.env, "auth", test.spec.APIKeySecretKey)}
			got, bindings, err := renderTestCredentials(input, environment)
			require.NoError(t, err)
			require.Equal(t, CredentialPlaceholder, got[0].Value)
			require.Nil(t, got[0].ValueFrom)
			require.NotNil(t, environment[0].ValueFrom, "compilation must not mutate inputs")
			require.Len(t, bindings, 1)
			require.Equal(t, "https://"+test.host+":443", bindings[0].Origin)
			require.Equal(t, test.header, bindings[0].Header)
			require.Equal(t, test.prefix, bindings[0].Prefix)
			require.Equal(t, "ate-secret://k8s.io/default/team/auth/"+test.spec.APIKeySecretKey, bindings[0].URI)
		})
	}
}

func TestResolvedPolicyRejectsConflictingSharedModels(t *testing.T) {
	spec := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "root", APIKeySecretKey: "token"}
	input := credentialInput(spec)
	spec.APIKeySecret = "child"
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(spec).Root}}
	// The ADK environment is deduplicated, but every model still needs its own binding.
	environment, _, err := renderTestCredentials(input, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "child", "token")})
	require.ErrorContains(t, err, "conflicting credentials")
	require.Nil(t, environment)
	input.Root.Shared[0].Agent.ResolvedModelConfig.Config.Spec.OpenAI = &v1alpha3.OpenAIConfig{BaseURL: "https://child.example.com/v1"}
	_, bindings, err := renderTestCredentials(input, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "child", "token")})
	require.NoError(t, err)
	require.Len(t, bindings, 2)
}

func TestResolvedPolicyRejectsLocalSecrets(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	environment, _, err := renderTestCredentials(input, []corev1.EnvVar{credentialEnv("AWS_SECRET_ACCESS_KEY", "auth", "token")})
	require.ErrorContains(t, err, "cannot use gateway header injection")
	require.Nil(t, environment)
}

func TestResolvedPolicyPreservesPassthrough(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeyPassthrough: true})
	_, bindings, err := renderTestCredentials(input, nil)
	require.NoError(t, err)
	require.Empty(t, bindings)
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "auth", APIKeySecretKey: "token",
	}).Root}}
	environment, _, err := renderTestCredentials(input, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "auth", "token")})
	require.ErrorContains(t, err, "cannot combine caller-token passthrough")
	require.Nil(t, environment)
}

func credentialInput(spec v1alpha3.ModelConfigSpec) *HarnessInput {
	resolved := &ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}, Spec: spec}}
	if spec.Provider == v1alpha3.ModelProviderBedrock && spec.APIKeySecretKey == "AWS_BEARER_TOKEN_BEDROCK" {
		resolved.References = []ModelConfigReference{{Kind: "Secret", NamespacedName: types.NamespacedName{Namespace: "team", Name: spec.APIKeySecret}, Key: spec.APIKeySecretKey}}
	}
	if spec.Foundry != nil {
		resolved.FoundryEndpoint = spec.Foundry.Endpoint
	}
	return &HarnessInput{
		Harness: &HarnessConfiguration{Name: "", Namespace: "team", Source: metav1.ObjectMeta{Namespace: "team"}},
		Root:    &AgentInput{Template: &TemplateConfiguration{}, ResolvedModelConfig: resolved},
	}
}

func credentialEnv(name, secret, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: key}}}
}

func TestPassthroughCredentialConflictUsesCanonicalOrigin(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeyPassthrough: true, OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://API.OPENAI.COM.:0443/v1"}})
	input.Root.MCPTools = []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL: "https://api.openai.com/mcp", HeadersFrom: []v1alpha3.ValueRef{{Name: "authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "auth", Key: "token"}}},
	}}}}
	environment, _, err := renderTestCredentials(input, nil)
	require.ErrorContains(t, err, "cannot combine caller-token passthrough")
	require.Nil(t, environment)
	input.Root.ResolvedModelConfig.Config.Spec.OpenAI.BaseURL = "https://api.openai.com:8443/v1"
	policy, err := ResolvePolicy(input, nil, nil)
	require.NoError(t, err, "a different port is a distinct credential scope")
	require.Equal(t, []string{"https://api.openai.com:8443"}, policy.tokenPassthroughOrigins)
}

func TestResolvedPolicyRejectsInvalidOrigins(t *testing.T) {
	for _, endpoint := range []string{
		"ftp://api.example.com", "https://192.0.2.1",
		"https://user@api.example.com", "https://api.example.com/#fragment",
	} {
		t.Run(endpoint, func(t *testing.T) {
			for _, passthrough := range []bool{false, true} {
				spec := v1alpha3.ModelConfigSpec{
					Provider: v1alpha3.ModelProviderOpenAI, APIKeyPassthrough: passthrough,
					OpenAI: &v1alpha3.OpenAIConfig{BaseURL: endpoint},
				}
				if !passthrough {
					spec.APIKeySecret, spec.APIKeySecretKey = "auth", "token"
				}
				_, bindings, err := renderTestCredentials(credentialInput(spec), nil)
				require.Error(t, err)
				require.Empty(t, bindings)
			}
		})
	}
}

func renderTestCredentials(input *HarnessInput, environment []corev1.EnvVar) ([]corev1.EnvVar, []egress.Credential, error) {
	policy, err := ResolvePolicy(input, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	rendered, err := policy.RenderEnvironment(environment)
	if err != nil {
		return nil, nil, err
	}
	return rendered, policy.access.Credentials, nil
}
