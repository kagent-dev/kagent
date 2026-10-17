package translator_test

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The ChatGPT login refresher rewrites the login's Secret, so a ModelConfig
// may name only a Secret that opts in; any other is a reference failure the
// ModelConfig reports in ResolvedRefs.
func TestResolveModelConfigRequiresAnOptedInChatGPTLogin(t *testing.T) {
	model := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "codex"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.1-codex", APIKeySecret: "login",
			OpenAI: &v1alpha3.OpenAIConfig{CredentialType: v1alpha3.OpenAICredentialTypeChatGPTOAuth},
		},
	}
	for _, test := range []struct {
		name   string
		labels map[string]string
		reason string
	}{
		{"labelled", map[string]string{v1alpha3.ChatGPTLoginLabel: "true"}, ""},
		{"unlabelled", nil, "ChatGPTLoginNotOptedIn"},
		{"labelled otherwise", map[string]string{v1alpha3.ChatGPTLoginLabel: "false"}, "ChatGPTLoginNotOptedIn"},
	} {
		t.Run(test.name, func(t *testing.T) {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "login", Labels: test.labels}, Data: map[string][]byte{
				v1alpha3.ChatGPTAccessTokenKey: []byte("a"), v1alpha3.ChatGPTRefreshTokenKey: []byte("r"), v1alpha3.ChatGPTAccountIDKey: []byte("id"),
			}}
			resolved, err := translator.ResolveModelConfig(krt.TestingDummyContext{}, mockCollections(t, secret), model)
			require.NoError(t, err)
			if test.reason == "" {
				require.Empty(t, resolved.ReferenceFailures)
				return
			}
			require.Len(t, resolved.ReferenceFailures, 1)
			require.Equal(t, test.reason, resolved.ReferenceFailures[0].Reason)
			require.Contains(t, resolved.ReferenceFailures[0].Message, v1alpha3.ChatGPTLoginLabel+"=true")
		})
	}
}
