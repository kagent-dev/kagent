package openshell

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildOpenshellCreateRequest_AllowedDomainsPolicy(t *testing.T) {
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns"},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendOpenClaw,
			Network: &v1alpha2.AgentHarnessNetwork{
				AllowedDomains: []string{
					"api.openai.com",
					"https://api.anthropic.com/v1",
					"*.slack.com",
					"api.openai.com",
				},
			},
		},
	}
	req, unsupported := buildOpenshellCreateRequest(sbx)
	require.Empty(t, unsupported)
	pol := req.GetSpec().GetPolicy()
	require.NotNil(t, pol)
	require.Equal(t, uint32(1), pol.GetVersion())
	net := pol.GetNetworkPolicies()
	require.Len(t, net, 5)
	require.Contains(t, net, openClawNetworkPolicyKeyClawhub)
	require.Contains(t, net, openClawNetworkPolicyKeyAPI)
	require.Contains(t, net, openClawNetworkPolicyKeyDocs)
	require.Contains(t, net, openClawNetworkPolicyKeyNPMYarn)
	npm := net[openClawNetworkPolicyKeyNPMYarn]
	require.Equal(t, "npm_yarn", npm.GetName())
	require.Len(t, npm.GetEndpoints(), 2)
	require.Equal(t, "registry.npmjs.org", npm.GetEndpoints()[0].GetHost())
	require.Equal(t, "skip", npm.GetEndpoints()[0].GetTls())
	require.Equal(t, "registry.yarnpkg.com", npm.GetEndpoints()[1].GetHost())

	clawhub := net[openClawNetworkPolicyKeyClawhub]
	require.Len(t, clawhub.GetEndpoints(), 1)
	require.Equal(t, openClawRegistryHostClawhub, clawhub.GetEndpoints()[0].GetHost())
	require.Equal(t, []uint32{443}, clawhub.GetEndpoints()[0].GetPorts())
	require.Len(t, clawhub.GetEndpoints()[0].GetRules(), 2)

	fs := pol.GetFilesystem()
	require.NotNil(t, fs)
	require.True(t, fs.GetIncludeWorkdir())
	require.ElementsMatch(t, []string{"/tmp", "/dev/null", "/sandbox/.openclaw", "/sandbox/.nemoclaw"}, fs.GetReadWrite())
	require.ElementsMatch(t, []string{"/usr", "/lib", "/proc", "/dev/urandom", "/app", "/etc", "/var/log"}, fs.GetReadOnly())
	require.NotNil(t, pol.GetLandlock())
	require.Equal(t, "best_effort", pol.GetLandlock().GetCompatibility())
	require.NotNil(t, pol.GetProcess())
	require.Equal(t, "sandbox", pol.GetProcess().GetRunAsUser())
	require.Equal(t, "sandbox", pol.GetProcess().GetRunAsGroup())

	rule := net[kagentAllowedDomainsNetworkPolicyKey]
	require.NotNil(t, rule)
	require.Equal(t, kagentAllowedDomainsNetworkPolicyKey, rule.GetName())
	require.Len(t, rule.GetEndpoints(), 3)
	paths := make([]string, 0, len(rule.GetBinaries()))
	for _, b := range rule.GetBinaries() {
		paths = append(paths, b.GetPath())
	}
	require.Contains(t, paths, "/usr/bin/curl")

	hosts := make([]string, 0, len(rule.GetEndpoints()))
	for _, ep := range rule.GetEndpoints() {
		require.Equal(t, []uint32{443, 80}, ep.GetPorts())
		require.Equal(t, "rest", ep.GetProtocol())
		require.Equal(t, "enforce", ep.GetEnforcement())
		require.Equal(t, "full", ep.GetAccess())
		hosts = append(hosts, ep.GetHost())
	}
	require.ElementsMatch(t, []string{"api.openai.com", "api.anthropic.com", "*.slack.com"}, hosts)
}

func TestBuildOpenshellCreateRequest_NoNetwork_NoPolicy(t *testing.T) {
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns"},
		Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendOpenshell},
	}
	req, _ := buildOpenshellCreateRequest(sbx)
	require.Nil(t, req.GetSpec().GetPolicy())
}

func TestBuildOpenshellCreateRequest_OpenClaw_NoAllowedDomains_HasRegistryPolicies(t *testing.T) {
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns"},
		Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendOpenClaw},
	}
	req, _ := buildOpenshellCreateRequest(sbx)
	policy := req.GetSpec().GetPolicy()
	require.NotNil(t, policy.GetFilesystem())
	net := policy.GetNetworkPolicies()
	require.Len(t, net, 4)
	require.Contains(t, net, openClawNetworkPolicyKeyClawhub)
	require.Contains(t, net, openClawNetworkPolicyKeyNPMYarn)
	require.Contains(t, net, openClawNetworkPolicyKeyAPI)
	require.Contains(t, net, openClawNetworkPolicyKeyDocs)
	require.NotContains(t, net, kagentAllowedDomainsNetworkPolicyKey)
	require.Equal(t, "best_effort", policy.GetLandlock().GetCompatibility())
	require.Equal(t, "sandbox", policy.GetProcess().GetRunAsUser())

	docs := net[openClawNetworkPolicyKeyDocs]
	require.Len(t, docs.GetEndpoints()[0].GetRules(), 1)
	require.Equal(t, "GET", docs.GetEndpoints()[0].GetRules()[0].GetAllow().GetMethod())
	require.Len(t, docs.GetBinaries(), 1)
}

func TestBuildOpenshellCreateRequest_Openshell_AllowedDomains_NoOpenClawDefaults(t *testing.T) {
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns"},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendOpenshell,
			Network: &v1alpha2.AgentHarnessNetwork{AllowedDomains: []string{"example.com"}},
		},
	}
	req, _ := buildOpenshellCreateRequest(sbx)
	policy := req.GetSpec().GetPolicy()
	require.Nil(t, policy.GetFilesystem())
	net := policy.GetNetworkPolicies()
	require.Len(t, net, 1)
	require.Contains(t, net, kagentAllowedDomainsNetworkPolicyKey)
	require.NotContains(t, net, openClawNetworkPolicyKeyClawhub)
}

func TestBuildOpenshellCreateRequest_OpenClaw_Telegram_HasTelegramBotPolicy(t *testing.T) {
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns"},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendOpenClaw,
			Channels: []v1alpha2.AgentHarnessChannel{
				{
					Name: "tg1",
					Type: v1alpha2.AgentHarnessChannelTypeTelegram,
					Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
						BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "token"},
					},
				},
			},
		},
	}
	req, _ := buildOpenshellCreateRequest(sbx)
	net := req.GetSpec().GetPolicy().GetNetworkPolicies()
	require.Len(t, net, 5)
	tgPol := net[openClawNetworkPolicyKeyTelegramBot]
	require.NotNil(t, tgPol)
	require.Equal(t, "telegram_bot", tgPol.GetName())
	require.Len(t, tgPol.GetEndpoints(), 1)
	ep := tgPol.GetEndpoints()[0]
	require.Equal(t, "api.telegram.org", ep.GetHost())
	require.Equal(t, []uint32{443}, ep.GetPorts())
	require.Len(t, ep.GetRules(), 3)
	require.Equal(t, "GET", ep.GetRules()[0].GetAllow().GetMethod())
	require.Equal(t, "/bot*/**", ep.GetRules()[0].GetAllow().GetPath())
	require.Equal(t, "POST", ep.GetRules()[1].GetAllow().GetMethod())
	require.Equal(t, "/bot*/**", ep.GetRules()[1].GetAllow().GetPath())
	require.Equal(t, "GET", ep.GetRules()[2].GetAllow().GetMethod())
	require.Equal(t, "/file/bot*/**", ep.GetRules()[2].GetAllow().GetPath())
	paths := make([]string, 0, len(tgPol.GetBinaries()))
	for _, b := range tgPol.GetBinaries() {
		paths = append(paths, b.GetPath())
	}
	require.ElementsMatch(t, []string{"/usr/local/bin/node", "/usr/bin/node", "/usr/bin/curl"}, paths)
}

func TestBuildOpenshellCreateRequest_Openshell_TelegramOnly_HasTelegramBotPolicy(t *testing.T) {
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns"},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendOpenshell,
			Channels: []v1alpha2.AgentHarnessChannel{
				{
					Name: "tg",
					Type: v1alpha2.AgentHarnessChannelTypeTelegram,
					Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
						BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "x"},
					},
				},
			},
		},
	}
	req, _ := buildOpenshellCreateRequest(sbx)
	policy := req.GetSpec().GetPolicy()
	require.NotNil(t, policy)
	require.Nil(t, policy.GetFilesystem())
	net := policy.GetNetworkPolicies()
	require.Len(t, net, 1)
	require.Contains(t, net, openClawNetworkPolicyKeyTelegramBot)
}

func TestBuildOpenshellCreateRequest_OpenClaw_Slack_HasSlackPolicy(t *testing.T) {
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns"},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendOpenClaw,
			Channels: []v1alpha2.AgentHarnessChannel{
				{
					Name: "s1",
					Type: v1alpha2.AgentHarnessChannelTypeSlack,
					Slack: &v1alpha2.AgentHarnessSlackChannelSpec{
						BotToken:      v1alpha2.AgentHarnessChannelCredential{Value: "b"},
						AppToken:      v1alpha2.AgentHarnessChannelCredential{Value: "a"},
						ChannelAccess: v1alpha2.AgentHarnessChannelAccessOpen,
					},
				},
			},
		},
	}
	req, _ := buildOpenshellCreateRequest(sbx)
	net := req.GetSpec().GetPolicy().GetNetworkPolicies()
	s := net[openClawNetworkPolicyKeySlack]
	require.NotNil(t, s)
	require.Equal(t, "slack", s.GetName())
	require.Len(t, s.GetEndpoints(), 5)
	require.Equal(t, "slack.com", s.GetEndpoints()[0].GetHost())
	require.Equal(t, "api.slack.com", s.GetEndpoints()[1].GetHost())
	require.Equal(t, "hooks.slack.com", s.GetEndpoints()[2].GetHost())
	require.Equal(t, "wss-primary.slack.com", s.GetEndpoints()[3].GetHost())
	require.Equal(t, "skip", s.GetEndpoints()[3].GetTls())
	require.Equal(t, "wss-backup.slack.com", s.GetEndpoints()[4].GetHost())
	require.Equal(t, "skip", s.GetEndpoints()[4].GetTls())
}

func TestBuildOpenshellCreateRequest_OpenClaw_AllowedDomains_OmitsNPMPresetHosts(t *testing.T) {
	sbx := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns"},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend: v1alpha2.AgentHarnessBackendOpenClaw,
			Network: &v1alpha2.AgentHarnessNetwork{
				AllowedDomains: []string{
					"registry.npmjs.org",
					"registry.npmjs.org:443",
					"registry.yarnpkg.com",
					"api.openai.com",
				},
			},
		},
	}
	req, _ := buildOpenshellCreateRequest(sbx)
	net := req.GetSpec().GetPolicy().GetNetworkPolicies()
	require.Contains(t, net, openClawNetworkPolicyKeyNPMYarn)
	rule := net[kagentAllowedDomainsNetworkPolicyKey]
	require.NotNil(t, rule)
	hosts := make([]string, 0, len(rule.GetEndpoints()))
	for _, ep := range rule.GetEndpoints() {
		hosts = append(hosts, ep.GetHost())
	}
	require.ElementsMatch(t, []string{"api.openai.com"}, hosts)
}

func TestSandboxPolicyFromAllowedDomains_AllInvalidReturnsNil(t *testing.T) {
	require.Nil(t, sandboxPolicyFromAllowedDomains([]string{"", "https://", "   "}))
}

func TestSandboxPolicyFromAllowedDomains_GlobsPreserved(t *testing.T) {
	pol := sandboxPolicyFromAllowedDomains([]string{"**.example.com"})
	require.NotNil(t, pol)
	rule := pol.GetNetworkPolicies()[kagentAllowedDomainsNetworkPolicyKey]
	require.Len(t, rule.GetEndpoints(), 1)
	ep0 := rule.GetEndpoints()[0]
	require.Equal(t, "**.example.com", ep0.GetHost())
	require.Equal(t, "rest", ep0.GetProtocol())
	require.Equal(t, "full", ep0.GetAccess())
}
