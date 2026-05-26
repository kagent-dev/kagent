package hermes_test

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/hermes"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBaselineHermesSandboxPolicy(t *testing.T) {
	pol := hermes.BaselineHermesSandboxPolicy()
	require.NotNil(t, pol)
	net := pol.GetNetworkPolicies()
	require.Contains(t, net, hermes.NetworkPolicyKeyNVIDIA)
	require.Contains(t, net, hermes.NetworkPolicyKeyNousResearch)
	require.Contains(t, net, hermes.NetworkPolicyKeyPyPI)
	require.NotContains(t, net, "clawhub")

	fs := pol.GetFilesystem()
	require.True(t, fs.GetIncludeWorkdir())
	require.Contains(t, fs.GetReadWrite(), hermes.HermesConfigDir)
	require.Contains(t, fs.GetReadOnly(), "/opt/hermes")
}

func TestChannelNetworkPolicyFragment_Slack(t *testing.T) {
	ah := &v1alpha2.AgentHarness{
		Spec: v1alpha2.AgentHarnessSpec{
			Channels: []v1alpha2.AgentHarnessChannel{
				{
					Name: "sl",
					Type: v1alpha2.AgentHarnessChannelTypeSlack,
					Slack: &v1alpha2.AgentHarnessSlackChannelSpec{
						BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "xoxb-bot"},
						AppToken: v1alpha2.AgentHarnessChannelCredential{Value: "xapp-app"},
					},
				},
			},
		},
	}
	frag := hermes.ChannelNetworkPolicyFragment(ah)
	require.NotNil(t, frag)
	slack := frag.GetNetworkPolicies()[hermes.NetworkPolicyKeySlack]
	require.NotNil(t, slack)
	var restHosts, wssHosts int
	for _, ep := range slack.GetEndpoints() {
		switch ep.GetHost() {
		case "slack.com", "api.slack.com", "hooks.slack.com":
			require.True(t, ep.GetRequestBodyCredentialRewrite())
			restHosts++
		case "wss-primary.slack.com", "wss-backup.slack.com":
			require.Equal(t, "websocket", ep.GetProtocol())
			require.True(t, ep.GetWebsocketCredentialRewrite())
			wssHosts++
		}
	}
	require.Equal(t, 3, restHosts)
	require.Equal(t, 2, wssHosts)
}

func TestChannelNetworkPolicyFragment_Telegram(t *testing.T) {
	ah := &v1alpha2.AgentHarness{
		Spec: v1alpha2.AgentHarnessSpec{
			Channels: []v1alpha2.AgentHarnessChannel{
				{
					Name: "tg",
					Type: v1alpha2.AgentHarnessChannelTypeTelegram,
					Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
						BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "tok"},
					},
				},
			},
		},
	}
	frag := hermes.ChannelNetworkPolicyFragment(ah)
	require.NotNil(t, frag)
	require.Contains(t, frag.GetNetworkPolicies(), hermes.NetworkPolicyKeyTelegram)
}

func TestIsHermesSandboxBackend(t *testing.T) {
	require.True(t, hermes.IsHermesSandboxBackend(v1alpha2.AgentHarnessBackendHermes))
	require.False(t, hermes.IsHermesSandboxBackend(v1alpha2.AgentHarnessBackendOpenClaw))
	_ = metav1.ObjectMeta{}
}
