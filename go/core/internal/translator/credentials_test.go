package translator

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompileCredentialDestinations(t *testing.T) {
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
		{"Bedrock bearer", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"}}, "AWS_BEARER_TOKEN_BEDROCK", "bedrock-runtime.us-east-1.amazonaws.com", "authorization", "Bearer "},
		{"Anthropic Vertex AI", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: v1alpha3.BaseVertexAIConfig{ProjectID: "project", Location: "us-east5"}}}, "GOOGLE_APPLICATION_CREDENTIALS", "us-east5-aiplatform.googleapis.com", "authorization", "Bearer "},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.spec.APIKeySecret, test.spec.APIKeySecretKey = "auth", "token"
			if test.spec.Provider == v1alpha3.ModelProviderBedrock {
				test.spec.APIKeySecretKey = test.env
			}
			input := credentialInput(test.spec)
			environment := []corev1.EnvVar{credentialEnv(test.env, "auth", test.spec.APIKeySecretKey)}
			got, bindings, err := CompileCredentials(input, nil, environment)
			require.NoError(t, err)
			require.Equal(t, CredentialPlaceholder, got[0].Value)
			require.Nil(t, got[0].ValueFrom)
			require.NotNil(t, environment[0].ValueFrom, "compilation must not mutate inputs")
			require.Len(t, bindings, 1)
			require.Equal(t, test.host, bindings[0].Hostname)
			require.Equal(t, test.header, bindings[0].Header)
			require.Equal(t, test.prefix, bindings[0].Prefix)
			authority := egress.KubernetesSecretAuthority
			if test.spec.Provider == v1alpha3.ModelProviderAnthropicVertexAI {
				authority = egress.GoogleAccessTokenAuthority
			}
			require.Equal(t, egress.CredentialURI(authority, "team", "auth", test.spec.APIKeySecretKey), bindings[0].URI)
		})
	}
}

func TestCompileCredentialsBindsVertexWithoutEnvironment(t *testing.T) {
	// The Claude compiler renders no variable for the key: Substrate mints the
	// token and the runtime sends the request unauthenticated.
	spec := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, APIKeySecret: "auth", APIKeySecretKey: "credentials.json",
		AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: v1alpha3.BaseVertexAIConfig{ProjectID: "project", Location: "global"}}}
	environment := []corev1.EnvVar{{Name: "CLOUD_ML_REGION", Value: "global"}}
	got, bindings, err := CompileCredentials(credentialInput(spec), nil, environment)
	require.NoError(t, err)
	require.Equal(t, environment, got)
	require.Equal(t, []egress.Credential{{
		Hostname: "aiplatform.googleapis.com", Header: "authorization", Prefix: "Bearer ",
		URI: "ate-secret://google-access-token.kubernetes.io/team/auth/credentials.json",
	}}, bindings)
}

func TestCompileCredentialsRejectsConflictingSharedModels(t *testing.T) {
	spec := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "root", APIKeySecretKey: "token"}
	input := credentialInput(spec)
	spec.APIKeySecret = "child"
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(spec).Root}}
	// The ADK environment is deduplicated, but every model still needs its own binding.
	_, _, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "child", "token")})
	require.ErrorContains(t, err, "conflicting credentials")
	input.Root.Shared[0].Agent.ResolvedModelConfig.Config.Spec.OpenAI = &v1alpha3.OpenAIConfig{BaseURL: "https://child.example.com/v1"}
	_, bindings, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "child", "token")})
	require.NoError(t, err)
	require.Len(t, bindings, 2)
}

func TestCompileCredentialsRejectsLocalSecrets(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	_, _, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("AWS_SECRET_ACCESS_KEY", "auth", "token")})
	require.ErrorContains(t, err, "cannot use gateway header injection")
	input.Harness.Spec.Env = []v1alpha3.HarnessEnvVar{{Name: "CUSTOM", CredentialRef: credentialEnv("CUSTOM", "auth", "token").ValueFrom.SecretKeyRef}}
	_, _, err = CompileCredentials(input, nil, nil)
	require.ErrorContains(t, err, "arbitrary credentialRef")
}

func TestCompileCredentialsPreservesPassthrough(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeyPassthrough: true})
	_, bindings, err := CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Empty(t, bindings)
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "auth", APIKeySecretKey: "token",
	}).Root}}
	_, _, err = CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "auth", "token")})
	require.ErrorContains(t, err, "cannot combine caller-token passthrough")
}

func credentialInput(spec v1alpha3.ModelConfigSpec) *HarnessInput {
	resolved := &ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}, Spec: spec}}
	if spec.Foundry != nil {
		resolved.FoundryEndpoint = spec.Foundry.Endpoint
	}
	return &HarnessInput{
		Harness: &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}},
		Root:    &AgentInput{Template: &v1alpha3.AgentTemplate{}, ResolvedModelConfig: resolved},
	}
}

func credentialEnv(name, secret, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: key}}}
}
