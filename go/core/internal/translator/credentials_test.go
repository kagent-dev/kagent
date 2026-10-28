package translator

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
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
			got, bindings, _, err := CompileCredentials(input, nil, environment)
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

func TestCompileCredentialsRejectsConflictingSharedModels(t *testing.T) {
	spec := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "root", APIKeySecretKey: "token"}
	input := credentialInput(spec)
	spec.APIKeySecret = "child"
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(spec).Root}}
	// The ADK environment is deduplicated, but every model still needs its own binding.
	environment, _, _, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "child", "token")})
	require.ErrorContains(t, err, "conflicting credentials")
	require.Nil(t, environment)
	input.Root.Shared[0].Agent.ResolvedModelConfig.Config.Spec.OpenAI = &v1alpha3.OpenAIConfig{BaseURL: "https://child.example.com/v1"}
	_, bindings, _, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "child", "token")})
	require.NoError(t, err)
	require.Len(t, bindings, 2)
}

func TestCompileCredentialsRejectsLocalSecrets(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	environment, _, _, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("AWS_SECRET_ACCESS_KEY", "auth", "token")})
	require.ErrorContains(t, err, "cannot use gateway header injection")
	require.Nil(t, environment)
}

func TestCompileCredentialsPreservesPassthrough(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, TokenPassthrough: new(true)})
	_, bindings, _, err := CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Empty(t, bindings)
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderOpenAI, APIKeySecret: "auth", APIKeySecretKey: "token",
	}).Root}}
	environment, _, _, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "auth", "token")})
	require.ErrorContains(t, err, "mixes tokenPassthrough with clients that do not use it")
	require.Nil(t, environment)
}

func credentialInput(spec v1alpha3.ModelConfigSpec) *HarnessInput {
	resolved := &ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}, Spec: spec}}
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
				TokenPassthrough: new(true), Ollama: &v1alpha3.OllamaConfig{},
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

func TestPassthroughCredentialUsesCanonicalOrigin(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, TokenPassthrough: new(true), OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://API.OPENAI.COM.:0443/v1"}})
	environment, credentials, targets, err := CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Empty(t, environment)
	require.Empty(t, credentials)
	require.Equal(t, []egresspolicy.GatewayTarget{{Origin: "https://api.openai.com:443", Header: "authorization", Prefix: "Bearer "}}, targets)
}

func TestCompileCredentialsMCPTokenPassthrough(t *testing.T) {
	passthrough := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL: "https://mcp.example.com/mcp", TokenPassthrough: new(true),
	}}
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	input.Root.MCPTools = []ResolvedMCPTool{{Server: passthrough}}
	_, _, targets, err := CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []egresspolicy.GatewayTarget{{Origin: "https://mcp.example.com:443", Header: "authorization", Prefix: "Bearer "}}, targets)

	// A gateway credential on the same origin would overwrite the caller's token.
	keyed := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "keyed", Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL: "https://mcp.example.com/other",
		HeadersFrom: []v1alpha3.ValueRef{{Name: "X-API-Key", ValueFrom: &v1alpha3.ValueSource{
			Type: v1alpha3.SecretValueSource, Name: "mcp", Key: "token",
		}}},
	}}
	input.Root.MCPTools = append(input.Root.MCPTools, ResolvedMCPTool{Server: keyed})
	environment, _, _, err := CompileCredentials(input, nil, nil)
	require.ErrorContains(t, err, "mixes tokenPassthrough with clients that do not use it")
	require.Nil(t, environment)
}

func TestCompileCredentialsTokenPassthroughTargets(t *testing.T) {
	server := func(name, url string, passthrough bool) ResolvedMCPTool {
		return ResolvedMCPTool{Server: &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: url, TokenPassthrough: new(passthrough)}}}
	}
	model := func(spec v1alpha3.ModelConfigSpec) *ResolvedModelConfig {
		return credentialInput(spec).Root.ResolvedModelConfig
	}
	passthroughOpenAI := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, TokenPassthrough: new(true)}
	for _, test := range []struct {
		name        string
		spec        v1alpha3.ModelConfigSpec
		tools       []ResolvedMCPTool
		extra       []*ResolvedModelConfig
		environment []corev1.EnvVar
		want        []egresspolicy.GatewayTarget
		wantErr     string
	}{
		{
			name: "MCP and model", spec: passthroughOpenAI, tools: []ResolvedMCPTool{server("user", "https://MCP.Example.com.:0443/mcp", true)},
			want: []egresspolicy.GatewayTarget{
				{Origin: "https://api.openai.com:443", Header: "authorization", Prefix: "Bearer "},
				{Origin: "https://mcp.example.com:443", Header: "authorization", Prefix: "Bearer "},
			},
		},
		{
			name: "auxiliary model", extra: []*ResolvedModelConfig{model(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderMistral, TokenPassthrough: new(true)})},
			want: []egresspolicy.GatewayTarget{{Origin: "https://api.mistral.ai:443", Header: "authorization", Prefix: "Bearer "}},
		},
		{
			name: "Bedrock", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, TokenPassthrough: new(true), Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"}},
			want: []egresspolicy.GatewayTarget{{Origin: "https://bedrock-runtime.us-east-1.amazonaws.com:443", Header: "authorization", Prefix: "Bearer "}},
		},
		{
			name: "Secret header on the passthrough origin",
			tools: []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
				URL: "https://mcp.example.com/mcp", TokenPassthrough: new(true),
				HeadersFrom: []v1alpha3.ValueRef{{Name: "X-Tenant-Key", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "mcp", Key: "token"}}},
			}}}},
			environment: []corev1.EnvVar{credentialEnv("KAGENT_CREDENTIAL_TENANT", "mcp", "token")},
			wantErr:     "cannot combine caller-token passthrough",
		},
		{name: "HTTP origin", tools: []ResolvedMCPTool{server("user", "http://mcp.example.com/mcp", true)}, want: []egresspolicy.GatewayTarget{{Origin: "http://mcp.example.com:80", Header: "authorization", Prefix: "Bearer "}}},
		{name: "no gateway target", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderSAPAICore, TokenPassthrough: new(true), SAPAICore: &v1alpha3.SAPAICoreConfig{BaseURL: "https://sap.example.com"}}, wantErr: "has no credential target"},
		{
			name: "MCP without passthrough", tools: []ResolvedMCPTool{server("user", "https://mcp.example.com/mcp", true), server("shared", "https://mcp.example.com/other", false)},
			wantErr: "mixes tokenPassthrough",
		},
		{
			name: "keyless custom Ollama", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Model: "llama3", Ollama: &v1alpha3.OllamaConfig{Host: "https://mcp.example.com"}},
			environment: []corev1.EnvVar{{Name: "OLLAMA_API_BASE", Value: "https://mcp.example.com"}},
			tools:       []ResolvedMCPTool{server("user", "https://mcp.example.com/mcp", true)}, wantErr: "mixes tokenPassthrough",
		},
		{
			name: "keyless Vertex model", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderGeminiVertexAI, Model: "gemini-2.5-flash", GeminiVertexAI: &v1alpha3.GeminiVertexAIConfig{BaseVertexAIConfig: v1alpha3.BaseVertexAIConfig{ProjectID: "project", Location: "us-central1"}}},
			tools: []ResolvedMCPTool{server("user", "https://us-central1-aiplatform.googleapis.com/mcp", true)}, wantErr: "mixes tokenPassthrough",
		},
		{
			name: "Vertex token endpoint", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, Model: "claude-sonnet-4-5", AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: v1alpha3.BaseVertexAIConfig{ProjectID: "project", Location: "global"}}},
			tools: []ResolvedMCPTool{server("user", "https://oauth2.googleapis.com/mcp", true)}, wantErr: "mixes tokenPassthrough",
		},
		{
			name: "SAP auth endpoint", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderSAPAICore, SAPAICore: &v1alpha3.SAPAICoreConfig{BaseURL: "https://sap.example.com", AuthURL: "https://mcp.example.com/oauth/token"}},
			tools: []ResolvedMCPTool{server("user", "https://mcp.example.com/mcp", true)}, wantErr: "mixes tokenPassthrough",
		},
		{
			name: "keyless auxiliary model", tools: []ResolvedMCPTool{server("user", "https://mcp.example.com/mcp", true)},
			extra: []*ResolvedModelConfig{model(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://mcp.example.com/v1"}})}, wantErr: "mixes tokenPassthrough",
		},
		{
			name: "IP destination beside a target", tools: []ResolvedMCPTool{server("user", "https://mcp.example.com/mcp", true), server("ip", "https://192.0.2.1/mcp", false)},
			want: []egresspolicy.GatewayTarget{{Origin: "https://mcp.example.com:443", Header: "authorization", Prefix: "Bearer "}},
		},
		{
			name: "explicit target header", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, TokenPassthrough: new(true), DefaultHeaders: map[string]string{"Authorization": "Bearer static"}},
			wantErr: "explicit \"authorization\" header",
		},
		{
			name: "explicit header on another client", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, TokenPassthrough: new(true), Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "https://same.example.com"}},
			tools: []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
				URL: "https://SAME.example.com.:0443/mcp", TokenPassthrough: new(true), HeadersFrom: []v1alpha3.ValueRef{{Name: "X-API-Key", Value: "static"}},
			}}}},
			wantErr: "explicit \"x-api-key\" header",
		},
		{
			name: "keyless Ollama stays local beside cloud MCP", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Model: "minimax-m3:cloud", Ollama: &v1alpha3.OllamaConfig{}},
			tools: []ResolvedMCPTool{server("user", "https://api.ollama.com/mcp", true)},
			want:  []egresspolicy.GatewayTarget{{Origin: "https://api.ollama.com:443", Header: "authorization", Prefix: "Bearer "}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := credentialInput(test.spec)
			input.Root.MCPTools = test.tools
			extra := test.extra
			if extra != nil {
				extra = append(extra, input.Root.ResolvedModelConfig)
			}
			_, _, targets, err := CompileCredentials(input, extra, test.environment)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				var validation *ValidationError
				require.ErrorAs(t, err, &validation)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, targets)
		})
	}
}
