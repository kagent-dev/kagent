package translator

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

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
			target := modelCredentialTarget(resolved)
			require.Equal(t, tt.wantName, target.Environment)
			require.Equal(t, tt.wantEndpoint, target.Endpoint)
		})
	}
}
