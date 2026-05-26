package hermes

import (
	"maps"

	sandboxv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/sandboxv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// Network policy map keys for Hermes sandbox egress. Source of truth:
// NemoClaw agents/hermes/policy-additions.yaml
const (
	NetworkPolicyKeyNVIDIA       = "nvidia"
	NetworkPolicyKeyGitHub       = "github"
	NetworkPolicyKeyNousResearch = "nous_research"
	NetworkPolicyKeyPyPI         = "pypi"
	NetworkPolicyKeyTelegram     = "telegram"
	NetworkPolicyKeySlack        = "slack"
)

const (
	endpointProtocolREST = "rest"
	endpointEnforcement  = "enforce"
	endpointAccessFull   = "full"
	endpointTLSSkip      = "skip"
)

var hermesCoreBinaries = []*sandboxv1.NetworkBinary{
	{Path: "/usr/local/bin/hermes"},
	{Path: "/usr/bin/python3*"},
	{Path: "/opt/hermes/.venv/bin/python"},
}

var hermesMessagingBinaries = []*sandboxv1.NetworkBinary{
	{Path: "/usr/local/bin/node"},
	{Path: "/usr/bin/python3*"},
	{Path: "/opt/hermes/.venv/bin/python"},
}

var nvidiaInferenceRules = []*sandboxv1.L7Rule{
	{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "/v1/chat/completions"}},
	{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "/v1/completions"}},
	{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "/v1/embeddings"}},
	{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/v1/models"}},
	{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/v1/models/**"}},
}

var nousResearchWildcardRules = []*sandboxv1.L7Rule{
	{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/**"}},
	{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "/**"}},
}

var pypiGETRules = []*sandboxv1.L7Rule{
	{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/**"}},
}

var telegramHermesRules = []*sandboxv1.L7Rule{
	{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/bot*/**"}},
	{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "/bot*/**"}},
	{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/file/bot*/**"}},
}

var slackRESTRules = []*sandboxv1.L7Rule{
	{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/**"}},
	{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "/**"}},
}

var slackWssRules = []*sandboxv1.L7Rule{
	{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/**"}},
	{Allow: &sandboxv1.L7Allow{Method: "WEBSOCKET_TEXT", Path: "/**"}},
}

func restEndpoint(host string, rules []*sandboxv1.L7Rule) *sandboxv1.NetworkEndpoint {
	return &sandboxv1.NetworkEndpoint{
		Host:        host,
		Ports:       []uint32{443},
		Protocol:    endpointProtocolREST,
		Enforcement: endpointEnforcement,
		Rules:       rules,
	}
}

func restEndpointFullAccess(host string) *sandboxv1.NetworkEndpoint {
	return &sandboxv1.NetworkEndpoint{
		Host:        host,
		Ports:       []uint32{443},
		Protocol:    endpointProtocolREST,
		Enforcement: endpointEnforcement,
		Access:      endpointAccessFull,
	}
}

func restEndpointSlack(host string) *sandboxv1.NetworkEndpoint {
	ep := restEndpoint(host, slackRESTRules)
	ep.RequestBodyCredentialRewrite = true
	return ep
}

func slackWssEndpoint(host string) *sandboxv1.NetworkEndpoint {
	return &sandboxv1.NetworkEndpoint{
		Host:                       host,
		Ports:                      []uint32{443},
		Protocol:                   "websocket",
		Enforcement:                endpointEnforcement,
		WebsocketCredentialRewrite: true,
		Rules:                      slackWssRules,
	}
}

// IsHermesSandboxBackend reports backends that use the Hermes sandbox baseline.
func IsHermesSandboxBackend(b v1alpha2.AgentHarnessBackendType) bool {
	return b == v1alpha2.AgentHarnessBackendHermes
}

func defaultHermesFilesystemPolicy() *sandboxv1.FilesystemPolicy {
	return &sandboxv1.FilesystemPolicy{
		IncludeWorkdir: true,
		ReadOnly: []string{
			"/usr",
			"/lib",
			"/opt/hermes",
			"/proc",
			"/dev/urandom",
			"/app",
			"/etc",
			"/var/log",
		},
		ReadWrite: []string{
			"/sandbox",
			"/tmp",
			"/dev/null",
			HermesConfigDir,
		},
	}
}

func defaultHermesLandlockPolicy() *sandboxv1.LandlockPolicy {
	return &sandboxv1.LandlockPolicy{Compatibility: "best_effort"}
}

func defaultHermesProcessPolicy() *sandboxv1.ProcessPolicy {
	return &sandboxv1.ProcessPolicy{
		RunAsUser:  "sandbox",
		RunAsGroup: "sandbox",
	}
}

func defaultHermesNetworkPolicies() map[string]*sandboxv1.NetworkPolicyRule {
	nousHosts := []string{
		"nousresearch.com",
		"hermes-agent.nousresearch.com",
		"inference-api.nousresearch.com",
		"portal.nousresearch.com",
		"browser-use-gateway.nousresearch.com",
		"modal-gateway.nousresearch.com",
		"openai-audio-gateway.nousresearch.com",
		"fal-queue-gateway.nousresearch.com",
		"firecrawl-gateway.nousresearch.com",
		"tool-gateway.nousresearch.com",
	}
	nousEndpoints := make([]*sandboxv1.NetworkEndpoint, 0, len(nousHosts))
	for _, h := range nousHosts {
		nousEndpoints = append(nousEndpoints, restEndpoint(h, nousResearchWildcardRules))
	}

	return map[string]*sandboxv1.NetworkPolicyRule{
		NetworkPolicyKeyNVIDIA: {
			Name: "nvidia",
			Endpoints: []*sandboxv1.NetworkEndpoint{
				restEndpoint("integrate.api.nvidia.com", nvidiaInferenceRules),
				restEndpoint("inference-api.nvidia.com", nvidiaInferenceRules),
			},
			Binaries: hermesCoreBinaries,
		},
		NetworkPolicyKeyGitHub: {
			Name: "github",
			Endpoints: []*sandboxv1.NetworkEndpoint{
				restEndpointFullAccess("github.com"),
				restEndpointFullAccess("api.github.com"),
			},
			Binaries: []*sandboxv1.NetworkBinary{
				{Path: "/usr/bin/git"},
				{Path: "/opt/hermes/.venv/bin/python"},
			},
		},
		NetworkPolicyKeyNousResearch: {
			Name:      "nous_research",
			Endpoints: nousEndpoints,
			Binaries:  hermesCoreBinaries,
		},
		NetworkPolicyKeyPyPI: {
			Name: "pypi",
			Endpoints: []*sandboxv1.NetworkEndpoint{
				restEndpoint("pypi.org", pypiGETRules),
				restEndpoint("files.pythonhosted.org", pypiGETRules),
			},
			Binaries: []*sandboxv1.NetworkBinary{
				{Path: "/usr/local/bin/pip3"},
				{Path: "/usr/bin/python3*"},
				{Path: "/opt/hermes/.venv/bin/python"},
			},
		},
	}
}

func hermesTelegramNetworkPolicyRule() *sandboxv1.NetworkPolicyRule {
	return &sandboxv1.NetworkPolicyRule{
		Name:      "telegram",
		Endpoints: []*sandboxv1.NetworkEndpoint{restEndpoint("api.telegram.org", telegramHermesRules)},
		Binaries:  hermesMessagingBinaries,
	}
}

func hermesSlackNetworkPolicyRule() *sandboxv1.NetworkPolicyRule {
	return &sandboxv1.NetworkPolicyRule{
		Name: "slack",
		Endpoints: []*sandboxv1.NetworkEndpoint{
			restEndpointSlack("slack.com"),
			restEndpointSlack("api.slack.com"),
			restEndpointSlack("hooks.slack.com"),
			slackWssEndpoint("wss-primary.slack.com"),
			slackWssEndpoint("wss-backup.slack.com"),
		},
		Binaries: hermesCoreBinaries,
	}
}

func channelSpecPresent(ch v1alpha2.AgentHarnessChannel) bool {
	switch ch.Type {
	case v1alpha2.AgentHarnessChannelTypeTelegram:
		return ch.Telegram != nil
	case v1alpha2.AgentHarnessChannelTypeSlack:
		return ch.Slack != nil
	default:
		return false
	}
}

func sandboxHasChannelType(ah *v1alpha2.AgentHarness, typ v1alpha2.AgentHarnessChannelType) bool {
	if ah == nil {
		return false
	}
	for _, ch := range ah.Spec.Channels {
		if ch.Type == typ && channelSpecPresent(ch) {
			return true
		}
	}
	return false
}

// ApplyHermesBaselinePolicies adds Hermes fixed network rules plus filesystem / landlock / process policies.
func ApplyHermesBaselinePolicies(net map[string]*sandboxv1.NetworkPolicyRule) (fs *sandboxv1.FilesystemPolicy, landlock *sandboxv1.LandlockPolicy, process *sandboxv1.ProcessPolicy) {
	maps.Copy(net, defaultHermesNetworkPolicies())
	return defaultHermesFilesystemPolicy(), defaultHermesLandlockPolicy(), defaultHermesProcessPolicy()
}

// ApplyChannelNetworkPolicies adds Telegram / Slack egress when channels are configured.
func ApplyChannelNetworkPolicies(ah *v1alpha2.AgentHarness, net map[string]*sandboxv1.NetworkPolicyRule) {
	if sandboxHasChannelType(ah, v1alpha2.AgentHarnessChannelTypeTelegram) {
		net[NetworkPolicyKeyTelegram] = hermesTelegramNetworkPolicyRule()
	}
	if sandboxHasChannelType(ah, v1alpha2.AgentHarnessChannelTypeSlack) {
		net[NetworkPolicyKeySlack] = hermesSlackNetworkPolicyRule()
	}
}

// SandboxPolicyVersion is OpenShell SandboxPolicy.version for Hermes fragments.
const SandboxPolicyVersion = 1

// BaselineHermesSandboxPolicy returns the fixed Hermes baseline (nvidia, github, nous_research, pypi, filesystem, landlock, process).
func BaselineHermesSandboxPolicy() *sandboxv1.SandboxPolicy {
	net := map[string]*sandboxv1.NetworkPolicyRule{}
	fs, landlock, process := ApplyHermesBaselinePolicies(net)
	return &sandboxv1.SandboxPolicy{
		Version:         SandboxPolicyVersion,
		NetworkPolicies: net,
		Filesystem:      fs,
		Landlock:        landlock,
		Process:         process,
	}
}

// ChannelNetworkPolicyFragment returns Telegram/Slack egress as a network-only policy fragment when channels are configured, or nil.
func ChannelNetworkPolicyFragment(ah *v1alpha2.AgentHarness) *sandboxv1.SandboxPolicy {
	if ah == nil {
		return nil
	}
	net := map[string]*sandboxv1.NetworkPolicyRule{}
	ApplyChannelNetworkPolicies(ah, net)
	if len(net) == 0 {
		return nil
	}
	return &sandboxv1.SandboxPolicy{Version: SandboxPolicyVersion, NetworkPolicies: net}
}

// AllowedDomainsBinaries returns executables allowed to use kagent_allowed_domains for Hermes harnesses.
func AllowedDomainsBinaries() []*sandboxv1.NetworkBinary {
	return []*sandboxv1.NetworkBinary{
		{Path: "/usr/local/bin/hermes"},
		{Path: "/usr/bin/python3*"},
		{Path: "/opt/hermes/.venv/bin/python"},
		{Path: "/usr/bin/curl"},
		{Path: "/usr/bin/git"},
		{Path: "/sandbox/**"},
	}
}
