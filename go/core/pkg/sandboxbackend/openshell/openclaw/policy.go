package openclaw

import (
	"maps"
	"strings"

	sandboxv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/sandboxv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
)

// Network policy map keys for OpenClaw / NemoClaw sandbox egress (OpenShell SandboxPolicy.network_policies).
const (
	NetworkPolicyKeyClawhub     = "clawhub"
	NetworkPolicyKeyAPI         = "openclaw_api"
	NetworkPolicyKeyDocs        = "openclaw_docs"
	NetworkPolicyKeyTelegramBot = "telegram_bot"
	NetworkPolicyKeySlack       = "slack"
	NetworkPolicyKeyNPMYarn     = "npm_yarn"

	RegistryHostClawhub = "clawhub.ai"
	RegistryHostAPI     = "openclaw.ai"
	RegistryHostDocs    = "docs.openclaw.ai"
)

// L7 REST settings for fixed claw endpoints; see
// https://docs.nvidia.com/openshell/reference/policy-schema (network_policies, endpoints).
const (
	endpointProtocolREST = "rest"
	endpointEnforcement  = "enforce"
	endpointAccessFull   = "full"
	endpointTLSSkip      = "skip"
)

var (
	openClawCLIAndNodeBinaries = []*sandboxv1.NetworkBinary{
		{Path: "/usr/local/bin/openclaw"},
		{Path: "/usr/local/bin/node"},
	}
	openClawCLIBinariesOnly = []*sandboxv1.NetworkBinary{
		{Path: "/usr/local/bin/openclaw"},
	}
)

// Immutable L7 rule slices reused across policy rules (safe to share; not mutated).
var (
	l7WildcardGETPOST = []*sandboxv1.L7Rule{
		{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "**"}},
		{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "**"}},
	}
	l7WildcardGETOnly = []*sandboxv1.L7Rule{
		{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "**"}},
	}
	telegramBotHTTPRules = []*sandboxv1.L7Rule{
		{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/bot*/**"}},
		{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "/bot*/**"}},
		{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/file/bot*/**"}},
	}
	slackRESTRules = []*sandboxv1.L7Rule{
		{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/**"}},
		{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "/**"}},
	}
)

// restNetworkEndpoint is HTTPS:443 with protocol rest + enforce and explicit L7 rules (OpenShell policy schema).
func restNetworkEndpoint(host string, rules []*sandboxv1.L7Rule) *sandboxv1.NetworkEndpoint {
	return &sandboxv1.NetworkEndpoint{
		Host:        host,
		Ports:       []uint32{443},
		Protocol:    endpointProtocolREST,
		Enforcement: endpointEnforcement,
		Rules:       rules,
	}
}

// messengerChannelNodeBinaries for Telegram / Slack OpenClaw channel egress (Node runtime).
var messengerChannelNodeBinaries = []*sandboxv1.NetworkBinary{
	{Path: "/usr/local/bin/node"},
	{Path: "/usr/bin/node"},
}

// telegramBotPolicyBinaries adds curl so probes/scripts hitting api.telegram.org match telegram_bot
// (OpenShell denies unless the executable is listed; otherwise OPA may attribute traffic to clawhub).
var telegramBotPolicyBinaries = append(messengerChannelNodeBinaries,
	&sandboxv1.NetworkBinary{Path: "/usr/bin/curl"},
)

// wssTunnelEndpoint is L4 TLS passthrough for WebSocket gateways (OpenShell tls: skip + access: full).
func wssTunnelEndpoint(host string) *sandboxv1.NetworkEndpoint {
	return &sandboxv1.NetworkEndpoint{
		Host:   host,
		Ports:  []uint32{443},
		Access: endpointAccessFull,
		Tls:    endpointTLSSkip,
	}
}

// npmYarnRegistryHosts are covered by npm_yarn (L4 CONNECT / undici); omit from kagent_allowed_domains for claw sandboxes.
var npmYarnRegistryHosts = map[string]struct{}{
	"registry.npmjs.org":   {},
	"registry.yarnpkg.com": {},
}

var npmYarnBinaries = []*sandboxv1.NetworkBinary{
	{Path: "/usr/local/bin/npm*"},
	{Path: "/usr/local/bin/npx*"},
	{Path: "/usr/local/bin/node*"},
	{Path: "/usr/local/bin/yarn*"},
	{Path: "/usr/bin/npm*"},
	{Path: "/usr/bin/node*"},
}

func npmYarnNetworkPolicyRule() *sandboxv1.NetworkPolicyRule {
	return &sandboxv1.NetworkPolicyRule{
		Name: "npm_yarn",
		Endpoints: []*sandboxv1.NetworkEndpoint{
			wssTunnelEndpoint("registry.npmjs.org"),
			wssTunnelEndpoint("registry.yarnpkg.com"),
		},
		Binaries: npmYarnBinaries,
	}
}

// OmitNPMPresetRegistryHosts drops registry hosts handled by npm_yarn when merging user allowedDomains (claw only).
func OmitNPMPresetRegistryHosts(domains []string) []string {
	if len(domains) == 0 {
		return domains
	}
	out := make([]string, 0, len(domains))
	for _, raw := range domains {
		host, ok := sandboxbackend.NormalizeAllowedDomainHost(raw)
		if !ok {
			continue
		}
		if _, skip := npmYarnRegistryHosts[strings.ToLower(host)]; skip {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func telegramBotNetworkPolicyRule() *sandboxv1.NetworkPolicyRule {
	return &sandboxv1.NetworkPolicyRule{
		Name:      "telegram_bot",
		Endpoints: []*sandboxv1.NetworkEndpoint{restNetworkEndpoint("api.telegram.org", telegramBotHTTPRules)},
		Binaries:  telegramBotPolicyBinaries,
	}
}

func slackNetworkPolicyRule() *sandboxv1.NetworkPolicyRule {
	return &sandboxv1.NetworkPolicyRule{
		Name: "slack",
		Endpoints: []*sandboxv1.NetworkEndpoint{
			restNetworkEndpoint("slack.com", slackRESTRules),
			restNetworkEndpoint("api.slack.com", slackRESTRules),
			restNetworkEndpoint("hooks.slack.com", slackRESTRules),
			wssTunnelEndpoint("wss-primary.slack.com"),
			wssTunnelEndpoint("wss-backup.slack.com"),
		},
		Binaries: messengerChannelNodeBinaries,
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

func sandboxHasChannelType(sbx *v1alpha2.AgentHarness, typ v1alpha2.AgentHarnessChannelType) bool {
	if sbx == nil {
		return false
	}
	for _, ch := range sbx.Spec.Channels {
		if ch.Type == typ && channelSpecPresent(ch) {
			return true
		}
	}
	return false
}

func defaultOpenClawNetworkPolicies() map[string]*sandboxv1.NetworkPolicyRule {
	return map[string]*sandboxv1.NetworkPolicyRule{
		NetworkPolicyKeyClawhub: {
			Name: "clawhub",
			Endpoints: []*sandboxv1.NetworkEndpoint{
				restNetworkEndpoint(RegistryHostClawhub, l7WildcardGETPOST),
			},
			Binaries: openClawCLIAndNodeBinaries,
		},
		NetworkPolicyKeyAPI: {
			Name: "openclaw_api",
			Endpoints: []*sandboxv1.NetworkEndpoint{
				restNetworkEndpoint(RegistryHostAPI, l7WildcardGETPOST),
			},
			Binaries: openClawCLIAndNodeBinaries,
		},
		NetworkPolicyKeyDocs: {
			Name: "openclaw_docs",
			Endpoints: []*sandboxv1.NetworkEndpoint{
				restNetworkEndpoint(RegistryHostDocs, l7WildcardGETOnly),
			},
			Binaries: openClawCLIBinariesOnly,
		},
	}
}

// IsClawSandboxBackend reports backends that use the OpenClaw-style sandbox baseline (OpenClaw / NemoClaw).
func IsClawSandboxBackend(b v1alpha2.AgentHarnessBackendType) bool {
	return b == v1alpha2.AgentHarnessBackendOpenClaw || b == v1alpha2.AgentHarnessBackendNemoClaw
}

// defaultClawFilesystemPolicy mirrors openclaw-sandbox.yaml (OpenShell rejects live changes to
// include_workdir and read_only removals). Workdir is included read-write in addition to paths below.
func defaultClawFilesystemPolicy() *sandboxv1.FilesystemPolicy {
	return &sandboxv1.FilesystemPolicy{
		IncludeWorkdir: true,
		ReadWrite: []string{
			"/tmp",
			"/dev/null",
			"/sandbox/.openclaw",
			"/sandbox/.nemoclaw",
		},
		ReadOnly: []string{
			"/usr",
			"/lib",
			"/proc",
			"/dev/urandom",
			"/app",
			"/etc",
			"/var/log",
		},
	}
}

func defaultClawLandlockPolicy() *sandboxv1.LandlockPolicy {
	return &sandboxv1.LandlockPolicy{
		Compatibility: "best_effort",
	}
}

func defaultClawProcessPolicy() *sandboxv1.ProcessPolicy {
	return &sandboxv1.ProcessPolicy{
		RunAsUser:  "sandbox",
		RunAsGroup: "sandbox",
	}
}

// ApplyClawBaselinePolicies adds OpenClaw/NemoClaw fixed network rules plus filesystem / landlock / process policies.
func ApplyClawBaselinePolicies(
	net map[string]*sandboxv1.NetworkPolicyRule,
) (fs *sandboxv1.FilesystemPolicy, landlock *sandboxv1.LandlockPolicy, process *sandboxv1.ProcessPolicy) {
	maps.Copy(net, defaultOpenClawNetworkPolicies())
	net[NetworkPolicyKeyNPMYarn] = npmYarnNetworkPolicyRule()
	return defaultClawFilesystemPolicy(), defaultClawLandlockPolicy(), defaultClawProcessPolicy()
}

// ApplyChannelNetworkPolicies adds Telegram / Slack egress when channels are configured.
func ApplyChannelNetworkPolicies(sbx *v1alpha2.AgentHarness, net map[string]*sandboxv1.NetworkPolicyRule) {
	if sandboxHasChannelType(sbx, v1alpha2.AgentHarnessChannelTypeTelegram) {
		net[NetworkPolicyKeyTelegramBot] = telegramBotNetworkPolicyRule()
	}
	if sandboxHasChannelType(sbx, v1alpha2.AgentHarnessChannelTypeSlack) {
		net[NetworkPolicyKeySlack] = slackNetworkPolicyRule()
	}
}

// SandboxPolicyVersion is OpenShell SandboxPolicy.version for fragments produced here and merged by openshell.
const SandboxPolicyVersion = 1

// BaselineSandboxPolicy returns the fixed OpenClaw/NemoClaw baseline (clawhub, API, docs, npm/yarn, filesystem, landlock, process).
func BaselineSandboxPolicy() *sandboxv1.SandboxPolicy {
	net := map[string]*sandboxv1.NetworkPolicyRule{}
	fs, landlock, process := ApplyClawBaselinePolicies(net)
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
