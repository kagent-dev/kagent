package adkconfig

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRenderBedrockCredentialsFromReferences(t *testing.T) {
	secret := types.NamespacedName{Namespace: "test", Name: "credentials"}
	tests := []struct {
		name       string
		references []translator.ModelConfigReference
		want       []string
	}{
		{
			name:       "bearer",
			references: []translator.ModelConfigReference{{NamespacedName: secret, Kind: "Secret", Key: env.AWSBearerTokenBedrock.Name()}},
			want:       []string{env.AWSRegion.Name(), env.AWSBearerTokenBedrock.Name()},
		},
		{
			name: "IAM with session token",
			references: []translator.ModelConfigReference{
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSAccessKeyID.Name()},
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSSecretAccessKey.Name()},
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSSessionToken.Name()},
			},
			want: []string{env.AWSRegion.Name(), env.AWSAccessKeyID.Name(), env.AWSSecretAccessKey.Name(), env.AWSSessionToken.Name()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &translator.ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: secret.Namespace},
					Spec: v1alpha3.ModelConfigSpec{
						Provider: v1alpha3.ModelProviderBedrock, Model: "claude", APIKeySecret: secret.Name,
						Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"},
					},
				},
				References: tt.references,
			}
			_, data, err := translateModel(resolved)
			require.NoError(t, err)
			names := make([]string, 0, len(data.EnvVars))
			for _, variable := range data.EnvVars {
				names = append(names, variable.Name)
			}
			require.ElementsMatch(t, tt.want, names)
		})
	}
}

// A Vertex AI credential is exchanged by the egress gateway: the runtime is
// told to skip Google authentication of its own and gets neither the key nor a
// credential file.
func TestTranslateVertexAIEnvironment(t *testing.T) {
	vertex := v1alpha3.BaseVertexAIConfig{ProjectID: "project", Location: "us-east5"}
	for _, tt := range []struct {
		name string
		spec v1alpha3.ModelConfigSpec
		want []string
	}{
		{
			name: "Gemini with a key",
			spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderGeminiVertexAI, Model: "gemini-2.5-pro", APIKeySecret: "vertex-auth", APIKeySecretKey: "credentials.json", GeminiVertexAI: &v1alpha3.GeminiVertexAIConfig{BaseVertexAIConfig: vertex}},
			want: []string{env.GoogleGenAIUseVertexAI.Name(), env.GoogleCloudProject.Name(), env.GoogleCloudLocation.Name(), env.SkipVertexAuth.Name()},
		},
		{
			name: "Anthropic with a key",
			spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, Model: "claude", APIKeySecret: "vertex-auth", APIKeySecretKey: "credentials.json", AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: vertex}},
			want: []string{env.GoogleCloudProject.Name(), env.GoogleCloudLocation.Name(), env.SkipVertexAuth.Name()},
		},
		{
			name: "Anthropic without a key",
			spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, Model: "claude", AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: vertex}},
			want: []string{env.GoogleCloudProject.Name(), env.GoogleCloudLocation.Name()},
		},
		{
			name: "Anthropic passthrough",
			spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, Model: "claude", APIKeySecret: "vertex-auth", APIKeySecretKey: "credentials.json", APIKeyPassthrough: true, AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: vertex}},
			want: []string{env.GoogleCloudProject.Name(), env.GoogleCloudLocation.Name()},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "test"}, Spec: tt.spec}}
			_, data, err := translateModel(resolved)
			require.NoError(t, err)
			names := make([]string, 0, len(data.EnvVars))
			for _, variable := range data.EnvVars {
				require.Nil(t, variable.ValueFrom, "%s reads a Secret", variable.Name)
				names = append(names, variable.Name)
				if variable.Name == env.SkipVertexAuth.Name() {
					require.Equal(t, "true", variable.Value)
				}
			}
			require.ElementsMatch(t, tt.want, names)
		})
	}
}

// The Ollama key reaches the agent as an environment variable read from a
// Secret, never serialized into the agent config. The host is the operator's,
// or the cloud endpoint for a model the routing predicate sends there, since
// the Python runtime does no routing of its own.
func TestTranslateOllamaEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		spec      v1alpha3.ModelConfigSpec
		wantNames []string
		noEnv     string
		wantHost  string
	}{
		{
			name: "cloud model mounts the key from a Secret",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "deepseek-v4-flash:0731-cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{},
			},
			wantNames: []string{env.OllamaAPIBase.Name(), env.OllamaAPIKey.Name()},
			wantHost:  "https://api.ollama.com",
		},
		{
			name: "an explicit host is passed through",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				Ollama: &v1alpha3.OllamaConfig{Host: "host.docker.internal:11434"},
			},
			wantNames: []string{env.OllamaAPIBase.Name()},
			noEnv:     env.OllamaAPIKey.Name(),
		},
		{
			name: "local model with no credential sets neither",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				Ollama: &v1alpha3.OllamaConfig{},
			},
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
		},
		{
			// Passthrough carries the caller's own token, so a stale secret must
			// not be mounted alongside it.
			name: "passthrough does not mount a key",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "deepseek-v4-flash:0731-cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				APIKeyPassthrough: true,
				Ollama:            &v1alpha3.OllamaConfig{},
			},
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
		},
		{
			// The chart ships a default host, so this is the ordinary case for
			// an operator who follows the values.yaml comment introducing
			// apiKeySecretRef. The request goes to that host, so no credential
			// binding exists, and mounting the key made CompileCredentials
			// reject the whole revision ("cannot use gateway header injection")
			// instead of simply using the daemon.
			name: "an explicit host does not mount the key even for a cloud model",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "deepseek-v4-flash:0731-cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{Host: "host.docker.internal:11434"},
			},
			wantNames: []string{env.OllamaAPIBase.Name()},
			noEnv:     env.OllamaAPIKey.Name(),
		},
		{
			// The cloud endpoint written out longhand is the cloud route, not an
			// operator host: the runtime takes its bearer token from
			// IsOllamaCloudEndpoint, so the key has to be mounted here or the
			// request goes out unauthenticated.
			name: "a host that is the cloud endpoint mounts the key",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "deepseek-v4-flash:0731-cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{Host: "api.ollama.com"},
			},
			wantNames: []string{env.OllamaAPIBase.Name(), env.OllamaAPIKey.Name()},
		},
		{
			// ...but it must not promote a local model to the cloud.
			name: "the cloud endpoint does not mount the key for a local model",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{Host: "api.ollama.com"},
			},
			wantNames: []string{env.OllamaAPIBase.Name()},
			noEnv:     env.OllamaAPIKey.Name(),
		},
		{
			// Same problem without a host: a local model has no cloud binding,
			// so mounting the Secret breaks compilation for a config that was
			// previously fine.
			name: "a local model with a secret does not mount the key",
			spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{},
			},
			wantNames: []string{},
			noEnv:     env.OllamaAPIKey.Name(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &translator.ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
					Spec:       tt.spec,
				},
			}
			_, data, err := translateModel(resolved)
			require.NoError(t, err)
			names := make([]string, 0, len(data.EnvVars))
			for _, variable := range data.EnvVars {
				names = append(names, variable.Name)
			}
			require.ElementsMatch(t, tt.wantNames, names)
			require.NotContains(t, names, tt.noEnv)
			if tt.wantHost != "" {
				require.Contains(t, data.EnvVars, corev1.EnvVar{Name: env.OllamaAPIBase.Name(), Value: tt.wantHost})
			}
		})
	}
}

// A bare host defaults to http, which is right for a daemon but wrong for
// ollama.com's API: it serves HTTPS only, an http request is answered with a
// redirect, and Go turns a 301 into a GET, so POST /api/chat came back
// 405 Method Not Allowed. The runtime's own copy of this already preferred
// https for the cloud endpoint; this is the compiler agreeing with it.
func TestWithDefaultScheme(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		// The cloud endpoint is HTTPS-only, and a bare host is how it is written.
		{host: "api.ollama.com", want: "https://api.ollama.com"},
		{host: "ollama.com", want: "https://ollama.com"},
		// An explicit scheme is never rewritten, including http, which is a
		// deliberate choice by the operator rather than a default.
		{host: "https://api.ollama.com", want: "https://api.ollama.com"},
		{host: "http://api.ollama.com", want: "http://api.ollama.com"},
		// A daemon is plain http on a private address, and keeps that.
		{host: "host.docker.internal:11434", want: "http://host.docker.internal:11434"},
		{host: "gpu-box.lan:11434", want: "http://gpu-box.lan:11434"},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			require.Equal(t, tt.want, withDefaultScheme(tt.host))
		})
	}
}

func TestTranslateAnthropicPromptCaching(t *testing.T) {
	tests := []struct {
		name        string
		spec        *v1alpha3.AnthropicConfig
		wantCaching bool
		wantTTL     string
	}{
		{name: "no provider block", spec: nil},
		// cacheTTL is passed through as configured; the runtime only reads it when caching is on.
		{name: "disabled", spec: &v1alpha3.AnthropicConfig{CacheTTL: "5m"}, wantTTL: "5m"},
		{name: "enabled with the default TTL", spec: &v1alpha3.AnthropicConfig{PromptCaching: true, CacheTTL: "5m"}, wantCaching: true, wantTTL: "5m"},
		{name: "enabled with the 1h TTL", spec: &v1alpha3.AnthropicConfig{PromptCaching: true, CacheTTL: "1h"}, wantCaching: true, wantTTL: "1h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &translator.ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
					Spec: v1alpha3.ModelConfigSpec{
						Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-6",
						Anthropic: tt.spec,
					},
				},
			}
			got, _, err := translateModel(resolved)
			require.NoError(t, err)
			anthropic, ok := got.(*adk.Anthropic)
			require.True(t, ok, "model is %T, want *adk.Anthropic", got)
			require.Equal(t, tt.wantCaching, anthropic.PromptCaching)
			require.Equal(t, tt.wantTTL, anthropic.CacheTTL)
		})
	}
}

// A ChatGPT login is used against the ChatGPT Codex backend, which serves the
// Codex harness alone, so an ADK agent cannot run on it.
func TestTranslateOpenAIRejectsAChatGPTLogin(t *testing.T) {
	resolved := &translator.ResolvedModelConfig{
		Config: &v1alpha3.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
			Spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.1-codex", APIKeySecret: "chatgpt-login",
				OpenAI: &v1alpha3.OpenAIConfig{CredentialType: v1alpha3.OpenAICredentialTypeChatGPTOAuth},
			},
		},
	}
	_, _, err := translateModel(resolved)
	var validation *translator.ValidationError
	require.ErrorAs(t, err, &validation)
	require.ErrorContains(t, err, "Codex harness only")
}

func TestTranslateOllamaThink(t *testing.T) {
	off, on := false, true
	tests := []struct {
		name  string
		think *bool
	}{
		{name: "unset", think: nil},
		{name: "off", think: &off},
		{name: "on", think: &on},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &translator.ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
					Spec: v1alpha3.ModelConfigSpec{
						Provider: v1alpha3.ModelProviderOllama, Model: "qwen3.5:2b",
						Ollama: &v1alpha3.OllamaConfig{Host: "http://ollama:11434", Think: tt.think},
					},
				},
			}
			got, _, err := translateModel(resolved)
			require.NoError(t, err)
			ollama, ok := got.(*adk.Ollama)
			require.True(t, ok, "model is %T, want *adk.Ollama", got)
			require.Equal(t, tt.think, ollama.Think)
		})
	}
}
