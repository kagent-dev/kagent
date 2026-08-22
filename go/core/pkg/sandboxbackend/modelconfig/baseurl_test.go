package modelconfig

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestExplicitBaseURL(t *testing.T) {
	tests := []struct {
		name string
		mc   *v1alpha3.ModelConfig
		want string
	}{
		{name: "nil", mc: nil, want: ""},
		{
			name: "openai explicit",
			mc: &v1alpha3.ModelConfig{Spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOpenAI,
				OpenAI:   &v1alpha3.OpenAIConfig{BaseURL: " https://api.example/v1 "},
			}},
			want: "https://api.example/v1",
		},
		{
			name: "openai unset",
			mc: &v1alpha3.ModelConfig{Spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOpenAI,
			}},
			want: "",
		},
		{
			name: "anthropic explicit",
			mc: &v1alpha3.ModelConfig{Spec: v1alpha3.ModelConfigSpec{
				Provider:  v1alpha3.ModelProviderAnthropic,
				Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "https://anthropic.example"},
			}},
			want: "https://anthropic.example",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExplicitBaseURL(tt.mc); got != tt.want {
				t.Fatalf("ExplicitBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
