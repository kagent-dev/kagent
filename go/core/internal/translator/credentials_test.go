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
			test.spec.Headers = []v1alpha3.Header{{Name: test.header, Prefix: &test.prefix, ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"}}}}
			input := credentialInput(test.spec)
			environment := []corev1.EnvVar{{Name: test.env, Value: CredentialPlaceholder}}
			got, err := CompileCredentials(input, nil, environment)
			require.NoError(t, err)
			bindings := got.Credentials
			require.Len(t, bindings, 1)
			require.Equal(t, "https://"+test.host+":443", bindings[0].Origin)
			require.Equal(t, test.header, bindings[0].Header)
			require.Equal(t, test.prefix, bindings[0].Prefix)
			require.Equal(t, "ate-secret://k8s.io/default/team/auth/"+test.spec.Headers[0].ValueFrom.SecretKeyRef.Key, bindings[0].URI)
		})
	}
}

func TestCompileCredentialsRejectsConflictingSharedModels(t *testing.T) {
	spec := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.

		// The ADK environment is deduplicated, but every model still needs its own binding.
		LocalObjectReference{Name: "root"}, Key: "token"}}}}}
	input := credentialInput(spec)
	spec = *spec.DeepCopy()
	spec.Headers[0].ValueFrom.SecretKeyRef.Name = "child"
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(spec).Root}}

	compiled, err := CompileCredentials(input, nil, []corev1.EnvVar{{Name: "OPENAI_API_KEY", Value: CredentialPlaceholder}})
	require.ErrorContains(t, err, "conflicting credentials")
	require.Nil(t, compiled)
	input.Root.Shared[0].Agent.ResolvedModelConfig.Config.Spec.OpenAI = &v1alpha3.OpenAIConfig{BaseURL: "https://child.example.com/v1"}
	compiled, err = CompileCredentials(input, nil, []corev1.EnvVar{{Name: "OPENAI_API_KEY", Value: CredentialPlaceholder}})
	require.NoError(t, err)
	require.Len(t, compiled.Credentials, 2)
}

func TestCompileCredentialsRejectsLocalSecrets(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	compiled, err := CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("AWS_SECRET_ACCESS_KEY", "auth", "token")})
	require.ErrorContains(t, err, "cannot use gateway header injection")
	require.Nil(t, compiled)
}

func TestCompileCredentialsPreservesPassthrough(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}})
	compiled, err := CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Empty(t, compiled.Credentials)
	input.Root.Shared = []AgentInputBinding{{Agent: credentialInput(v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderOpenAI, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"}}}},
	}).Root}}
	compiled, err = CompileCredentials(input, nil, []corev1.EnvVar{credentialEnv("OPENAI_API_KEY", "auth", "token")})
	require.ErrorContains(t, err, "mixes tokenPassthrough with clients that do not use it")
	require.Nil(t, compiled)
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

func TestModelCredentialTargetOllama(t *testing.T) {
	for _, tc := range []struct {
		name, model, host, endpoint string
		auth                        bool
	}{
		{"cloud", "minimax-m3:cloud", "", "https://api.ollama.com", true},
		{"explicit host", "minimax-m3:cloud", "models.example.com:11434", "http://models.example.com:11434", true},
		{"local model", "llama3", "", "http://localhost:11434", true},
		{"keyless cloud model", "minimax-m3:cloud", "", "http://localhost:11434", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Model: tc.model, Ollama: &v1alpha3.OllamaConfig{Host: tc.host}}
			if tc.auth {
				spec.Headers = []v1alpha3.Header{{Name: "Authorization", ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}
			}
			_, endpoint, _, _ := modelCredentialTarget(credentialInput(spec).Root.ResolvedModelConfig)
			require.Equal(t, tc.endpoint, endpoint)
		})
	}
}

func TestPassthroughCredentialUsesCanonicalOrigin(t *testing.T) {
	input := credentialInput(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://API.OPENAI.COM.:0443/v1"}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}})
	compiled, err := CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Empty(t, compiled.Credentials)
	require.Equal(t, []egresspolicy.GatewayTarget{{Origin: "https://api.openai.com:443", Header: "authorization", Prefix: "Bearer "}}, compiled.TokenPassthroughTargets)
}

func TestCompileCredentialsMCPTokenPassthrough(t *testing.T) {
	passthrough := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL: "https://mcp.example.com/mcp", Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}},
	}}
	input := credentialInput(v1alpha3.ModelConfigSpec{})
	input.Root.MCPTools = []ResolvedMCPTool{{Server: passthrough}}
	compiled, err := CompileCredentials(input, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []egresspolicy.GatewayTarget{{Origin: "https://mcp.example.com:443", Header: "authorization", Prefix: "Bearer "}}, compiled.TokenPassthroughTargets)

	// A gateway credential on the same origin would overwrite the caller's token.
	keyed := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "keyed", Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL:     "https://mcp.example.com/other",
		Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "mcp"}, Key: "token"}}}},
	}}
	input.Root.MCPTools = append(input.Root.MCPTools, ResolvedMCPTool{Server: keyed})
	compiled, err = CompileCredentials(input, nil, nil)
	require.ErrorContains(t, err, "mixes tokenPassthrough with clients that do not use it")
	require.Nil(t, compiled)
}

func TestCompileCredentialsTokenPassthroughTargets(t *testing.T) {
	server := func(name, url string, passthrough bool) ResolvedMCPTool {
		spec := v1alpha3.RemoteMCPServerSpec{URL: url}
		if passthrough {
			spec.Headers = []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}
		}
		return ResolvedMCPTool{Server: &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team"}, Spec: spec}}
	}
	model := func(spec v1alpha3.ModelConfigSpec) *ResolvedModelConfig {
		return credentialInput(spec).Root.ResolvedModelConfig
	}
	passthroughOpenAI := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}}
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
			name: "auxiliary model", extra: []*ResolvedModelConfig{model(v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderMistral, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}})},
			want: []egresspolicy.GatewayTarget{{Origin: "https://api.mistral.ai:443", Header: "authorization", Prefix: "Bearer "}},
		},
		{
			name: "Bedrock", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}},
			want: []egresspolicy.GatewayTarget{{Origin: "https://bedrock-runtime.us-east-1.amazonaws.com:443", Header: "authorization", Prefix: "Bearer "}},
		},
		{
			name: "Secret header on the passthrough origin",
			tools: []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
				URL:     "https://mcp.example.com/mcp",
				Headers: []v1alpha3.Header{{Name: "X-Tenant-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "mcp"}, Key: "token"}}}, {Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}},
			}}}},
			want: []egresspolicy.GatewayTarget{{Origin: "https://mcp.example.com:443", Header: "authorization", Prefix: "Bearer "}},
		},
		{name: "HTTP origin", tools: []ResolvedMCPTool{server("user", "http://mcp.example.com/mcp", true)}, want: []egresspolicy.GatewayTarget{{Origin: "http://mcp.example.com:80", Header: "authorization", Prefix: "Bearer "}}},
		{name: "no gateway target", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderSAPAICore, SAPAICore: &v1alpha3.SAPAICoreConfig{BaseURL: "https://sap.example.com"}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}}, wantErr: "has no credential target"},
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
			name: "explicit target header", spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, DefaultHeaders: map[string]string{"Authorization": "Bearer static"}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}},
			wantErr: "explicit \"authorization\" header",
		},
		{
			name: "explicit header on another client", spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOpenAI, OpenAI: &v1alpha3.OpenAIConfig{BaseURL: "https://same.example.com"},
				DefaultHeaders: map[string]string{"X-API-Key": "static"},
				Headers:        []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}},
			},
			tools: []ResolvedMCPTool{{Server: &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "team"}, Spec: v1alpha3.RemoteMCPServerSpec{
				URL: "https://SAME.example.com.:0443/mcp", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}},
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
			compiled, err := CompileCredentials(input, extra, test.environment)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				var validation *ValidationError
				require.ErrorAs(t, err, &validation)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, compiled.TokenPassthroughTargets)
		})
	}
}
