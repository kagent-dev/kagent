package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestBedrockProviderJSONRoundTrip(t *testing.T) {
	for _, raw := range []string{"", "{}", `{"thinking":{"budget_tokens":1024},"large":9007199254740993,"values":[0,false,null,0.25]}`} {
		t.Run(raw, func(t *testing.T) {
			bedrock := &v1alpha3.BedrockConfig{Region: "us-east-1"}
			if raw != "" {
				bedrock.AdditionalModelRequestFields = &apiextensionsv1.JSON{Raw: []byte(raw)}
			}
			model, _, err := translateModel(&translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{
				Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "claude", Bedrock: bedrock},
			}})
			require.NoError(t, err)
			encoded, err := runtimeconfig.Marshal(&configv1.ADKConfig{Agent: &configv1.ADKAgentConfig{Model: model}})
			require.NoError(t, err)
			var decoded configv1.ADKConfig
			require.NoError(t, runtimeconfig.Unmarshal(encoded, &decoded))
			require.Equal(t, raw, string(decoded.GetAgent().GetModel().GetBedrock().GetAdditionalModelRequestFields()))
			if raw == "" {
				require.NotContains(t, string(encoded), "additional_model_request_fields")
			}
		})
	}
}

func TestResolveModelCredentialHeaders(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec v1alpha3.ModelConfigSpec
		env  string
	}{
		{"Bedrock", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "claude", Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"}}, "AWS_BEARER_TOKEN_BEDROCK"},
		{"Ollama cloud", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Model: "minimax-m3:cloud", Ollama: &v1alpha3.OllamaConfig{}}, "OLLAMA_API_KEY"},
		{"Ollama explicit host", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOllama, Model: "llama3", Ollama: &v1alpha3.OllamaConfig{Host: "models.example.com:11434"}}, "OLLAMA_API_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.spec.Headers = []v1alpha3.Header{
				{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}},
				{Name: "X-Tenant-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tenant"}, Key: "key"}}},
			}
			got, err := resolveModel(&translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "test"}, Spec: tc.spec}})
			require.NoError(t, err)
			require.Equal(t, "Bearer "+translator.CredentialPlaceholder, got.Model.Headers["Authorization"])
			require.Equal(t, translator.CredentialPlaceholder, got.Model.Headers["X-Tenant-Key"])
			require.Equal(t, []*configv1.InvocationCredentialHeader{{Name: "Authorization", Prefix: "Bearer "}}, got.Model.PassthroughHeaders)
			require.Contains(t, got.Environment, corev1.EnvVar{Name: tc.env, Value: translator.CredentialPlaceholder})
			for _, variable := range got.Environment {
				require.Nil(t, variable.ValueFrom)
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
			anthropic := got.GetAnthropic()
			ok := anthropic != nil
			require.True(t, ok, "model is %T, want *configv1.ADKAnthropic", got)
			require.Equal(t, tt.wantCaching, anthropic.PromptCaching)
			require.Equal(t, tt.wantTTL, anthropic.CacheTtl)
		})
	}
}

func TestTranslateAzureOpenAIAPIFormat(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	got, _, err := translateModel(&translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: v1alpha3.ModelProviderAzureOpenAI, Model: "gpt-4.1-mini",
			AzureOpenAI: &v1alpha3.AzureOpenAIConfig{Endpoint: "https://team.openai.azure.com", APIFormat: &responses},
		},
	}})
	require.NoError(t, err)
	require.Equal(t, "responses", got.GetAzureOpenai().ApiFormat)
}
