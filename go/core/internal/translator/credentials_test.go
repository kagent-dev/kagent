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

func harnessCredentialEnv(name, url, header, prefix string) v1alpha3.RuntimeEnvVar {
	return v1alpha3.RuntimeEnvVar{Name: name, CredentialRef: &v1alpha3.RuntimeCredentialRef{Name: "api-auth", Key: "token", URL: url, Header: header, Prefix: prefix}}
}

func TestCompileCredentialsBindsHarnessCredentialRef(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	variable := harnessCredentialEnv("TICKETS_API_TOKEN", "https://Tickets.Example.com:8443/v2", "Authorization", "Bearer ")
	input.Harness.Spec.Env = []v1alpha3.RuntimeEnvVar{variable, {Name: "LANG", Value: "C.UTF-8"}}
	environment := []corev1.EnvVar{HarnessEnvVar(input.Harness.Spec.Env[0]), HarnessEnvVar(input.Harness.Spec.Env[1])}
	require.Equal(t, credentialEnv("TICKETS_API_TOKEN", "api-auth", "token"), environment[0])
	require.Equal(t, corev1.EnvVar{Name: "LANG", Value: "C.UTF-8"}, environment[1])

	got, bindings, err := CompileCredentials(input, nil, environment)
	require.NoError(t, err)
	require.Equal(t, []corev1.EnvVar{{Name: "TICKETS_API_TOKEN", Value: CredentialPlaceholder}, {Name: "LANG", Value: "C.UTF-8"}}, got)
	require.Equal(t, []egress.Credential{{Hostname: "tickets.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/api-auth/token"}}, bindings)

	destinations, err := HarnessCredentialDestinations(input.Harness)
	require.NoError(t, err)
	require.Equal(t, []string{"https://tickets.example.com:8443"}, destinations)
}

func TestCompileCredentialsRejectsInvalidHarnessCredentialRef(t *testing.T) {
	for _, test := range []struct {
		name, url, header, want string
		model                   v1alpha3.ModelConfigSpec
	}{
		{name: "IP destination", url: "https://10.0.0.1/api", header: "authorization", want: "exact DNS hostname"},
		{name: "relative URL", url: "/api", header: "authorization", want: "absolute HTTP(S) URL"},
		{name: "conflicts with model key", url: "https://api.openai.com/v1", header: "authorization", want: "conflicting credentials",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "model-auth", APIKeySecretKey: "key"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := credentialInput(test.model)
			input.Harness.Spec.Env = []v1alpha3.RuntimeEnvVar{harnessCredentialEnv("TOKEN", test.url, test.header, "")}
			environment := []corev1.EnvVar{HarnessEnvVar(input.Harness.Spec.Env[0])}
			if test.model.APIKeySecret != "" {
				environment = append(environment, credentialEnv("OPENAI_API_KEY", "model-auth", "key"))
			}
			_, _, err := CompileCredentials(input, nil, environment)
			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.ErrorContains(t, err, test.want)
		})
	}
}

// A Secret reference that no binding claims is still rejected: credentialRef
// only authorizes the variable it is declared on.
func TestCompileCredentialsRejectsUnboundHarnessSecretReference(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	input.Harness.Spec.Env = []v1alpha3.RuntimeEnvVar{harnessCredentialEnv("TOKEN", "https://api.example.com", "authorization", "")}
	_, _, err := CompileCredentials(input, nil, []corev1.EnvVar{HarnessEnvVar(input.Harness.Spec.Env[0]), credentialEnv("OTHER", "api-auth", "token")})
	require.ErrorContains(t, err, `environment credential "OTHER" cannot use gateway header injection`)
}
