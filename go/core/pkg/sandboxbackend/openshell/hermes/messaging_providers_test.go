package hermes

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/channels"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMessagingProviderDefsFromChannels(t *testing.T) {
	ns := "default"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tg", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("123:ABC")},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(secret).Build()
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "mybot", Namespace: ns},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendHermes,
			Channels: []v1alpha2.AgentHarnessChannel{
				{
					Name: "tg",
					Type: v1alpha2.AgentHarnessChannelTypeTelegram,
					Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
						BotToken: v1alpha2.AgentHarnessChannelCredential{
							ValueFrom: &v1alpha2.ValueSource{
								Type: v1alpha2.SecretValueSource,
								Name: "tg",
								Key:  "token",
							},
						},
					},
				},
			},
		},
	}
	resolved, err := channels.Resolve(context.Background(), kube, ns, ah.Spec.Backend, ah.Spec.Channels)
	require.NoError(t, err)
	defs := channels.MessagingProviderDefs("default-mybot", resolved.Secrets, resolved)
	require.Len(t, defs, 1)
	require.Equal(t, "default-mybot-telegram-TG", defs[0].Name)
	require.Equal(t, "123:ABC", defs[0].Credentials[channels.TelegramBotTokenEnvKey("tg")])
}
