package translator

import (
	"encoding/json"
	"strings"
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
			got, bindings, err := CompileCredentials(input, nil, environment)
			require.NoError(t, err)
			require.Equal(t, CredentialPlaceholder, got[0].Value)
			require.Nil(t, got[0].ValueFrom)
			require.NotNil(t, environment[0].ValueFrom, "compilation must not mutate inputs")
			require.Len(t, bindings, 1)
			require.Equal(t, test.host, bindings[0].Hostname)
			require.Equal(t, test.header, bindings[0].Header)
			require.Equal(t, test.prefix, bindings[0].Prefix)
			require.Equal(t, "ate-secret://k8s.io/default/team/auth/"+test.spec.APIKeySecretKey, bindings[0].URI)
		})
	}
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

func privateSkillTemplate(skills ...v1alpha3.AgentTemplateSkill) *TemplateConfiguration {
	return &TemplateConfiguration{Name: "reviewer", Namespace: "team", Spec: v1alpha3.AgentTemplateSpec{Skills: skills}}
}

func privateGitSkill(name, url, secret string) v1alpha3.AgentTemplateSkill {
	return v1alpha3.AgentTemplateSkill{Name: name, Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{
		URL: url, Commit: strings.Repeat("a", 40),
		AuthorizationFrom: &v1alpha3.SecretKeyReference{Name: secret, Key: "authorization"},
	}}}
}

func TestCompilePrivateGitSkillBindsGatewayCredentialAndEgress(t *testing.T) {
	template := privateSkillTemplate(
		privateGitSkill("review", "https://github.com/acme/private-skills", "skills-auth"),
		v1alpha3.AgentTemplateSkill{Name: "public", Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{
			URL: "https://gitlab.example.com/acme/public-skills", Commit: strings.Repeat("b", 40),
		}}},
	)
	resources, destinations, err := CompileSkillResources(template)
	require.NoError(t, err)
	require.Equal(t, []string{"https://github.com:443", "https://gitlab.example.com:443"}, destinations)
	require.True(t, resources.Skills[0].Source.Git.GatewayAuthorization)
	require.False(t, resources.Skills[1].Source.Git.GatewayAuthorization)
	raw, err := json.Marshal(resources)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "skills-auth", "the runtime contract must not name the Secret")

	input := credentialInput(v1alpha3.ModelConfigSpec{})
	input.Root.Template = template
	environment, bindings, err := CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Empty(t, environment, "a git credential needs no runtime environment")
	require.Equal(t, []egress.Credential{{
		Hostname: "github.com", Header: "authorization", URI: "ate-secret://k8s.io/default/team/skills-auth/authorization",
	}}, bindings)
}

func TestCompilePrivateGitSkillCredentialsFromSharedChild(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	child := credentialInput(v1alpha3.ModelConfigSpec{}).Root
	child.Template = privateSkillTemplate(privateGitSkill("review", "https://git.example.com/acme/skills", "child-auth"))
	child.Template.Namespace = "child-team"
	input.Root.Shared = []AgentInputBinding{{Agent: child}}
	_, bindings, err := CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, "ate-secret://k8s.io/default/child-team/child-auth/authorization", bindings[0].URI)
}

func TestCompilePrivateGitSkillRejectsConflictsAndPlugins(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	input.Root.Template = privateSkillTemplate(
		privateGitSkill("one", "https://github.com/acme/one", "first"),
		privateGitSkill("two", "https://github.com/acme/two", "second"),
	)
	_, _, err := CompileCredentials(input, nil, nil)
	require.ErrorContains(t, err, "conflicting credentials")

	input.Root.Template = privateSkillTemplate(privateGitSkill("one", "https://user@github.com/acme/one", "first"))
	_, _, err = CompileCredentials(input, nil, nil)
	require.ErrorContains(t, err, "without user information")

	template := &TemplateConfiguration{Namespace: "team", Spec: v1alpha3.AgentTemplateSpec{Plugins: []v1alpha3.PluginBundle{{
		Source: privateGitSkill("unused", "https://github.com/acme/plugin", "auth").Source,
	}}}}
	_, _, err = CompileSkillResources(template)
	require.ErrorContains(t, err, "supported only for skills")
}

func credentialInput(spec v1alpha3.ModelConfigSpec) *HarnessInput {
	resolved := &ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}, Spec: spec}}
	if spec.Foundry != nil {
		resolved.FoundryEndpoint = spec.Foundry.Endpoint
	}
	return &HarnessInput{
		Harness: &HarnessConfiguration{Name: "", Namespace: "team", Source: &metav1.ObjectMeta{Namespace: "team"}},
		Root:    &AgentInput{Template: &TemplateConfiguration{}, ResolvedModelConfig: resolved},
	}
}

func credentialEnv(name, secret, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: key}}}
}

// A cloud-tagged Ollama model reaches api.ollama.com with the key, which is a
// gateway-injected credential like any other — without this binding the
// translator rejects OLLAMA_API_KEY as an unsupported local secret and the
// AgentTemplate goes Compatible=False.
func TestModelCredentialTargetOllamaCloud(t *testing.T) {
	secret := v1alpha3.ModelConfigSpec{
		Provider:     v1alpha3.ModelProviderOllama,
		Model:        "deepseek-v4-flash:0731-cloud",
		APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
		Ollama: &v1alpha3.OllamaConfig{},
	}

	tests := []struct {
		name         string
		spec         v1alpha3.ModelConfigSpec
		wantName     string
		wantEndpoint string
	}{
		{
			name:         "cloud model with a secret binds the cloud",
			spec:         secret,
			wantName:     "OLLAMA_API_KEY",
			wantEndpoint: "https://api.ollama.com",
		},
		{
			name: "cloud model with passthrough binds the cloud",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "minimax-m3:cloud",
				APIKeyPassthrough: true, Ollama: &v1alpha3.OllamaConfig{},
			},
			wantName:     "OLLAMA_API_KEY",
			wantEndpoint: "https://api.ollama.com",
		},
		{
			// A local model must declare no binding: no request leaves for the
			// cloud, so there is nothing to inject a header into.
			name: "local model declares no binding",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{},
			},
		},
		{
			// An explicit host wins over the cloud route, so the key stays a
			// plain secret reference rather than a gateway binding.
			name: "explicit host declares no binding",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "minimax-m3:cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{Host: "host.docker.internal:11434"},
			},
		},
		{
			name: "cloud model without any key declares no binding",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "minimax-m3:cloud",
				Ollama: &v1alpha3.OllamaConfig{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
					Spec:       tt.spec,
				},
			}
			name, endpoint, _, _ := modelCredentialTarget(resolved)
			require.Equal(t, tt.wantName, name)
			require.Equal(t, tt.wantEndpoint, endpoint)
		})
	}
}
