package openshell

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	sandboxv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/sandboxv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Key used in SandboxPolicy.network_policies for spec.network.allowedDomains → SSRF/network rules.
const kagentAllowedDomainsNetworkPolicyKey = "kagent_allowed_domains"

// Default OpenClaw registry / docs egress (HTTPS-only); merged for openclaw/nemoclaw sandboxes.
const (
	openClawNetworkPolicyKeyClawhub     = "clawhub"
	openClawNetworkPolicyKeyAPI         = "openclaw_api"
	openClawNetworkPolicyKeyDocs        = "openclaw_docs"
	openClawNetworkPolicyKeyTelegramBot = "telegram_bot"
	openClawNetworkPolicyKeyDiscord     = "discord"
	openClawNetworkPolicyKeySlack       = "slack"
	openClawNetworkPolicyKeyNPMYarn     = "npm_yarn"

	openClawRegistryHostClawhub = "clawhub.ai"
	openClawRegistryHostAPI     = "openclaw.ai"
	openClawRegistryHostDocs    = "docs.openclaw.ai"
)

// L7 REST settings for allowedDomains entries; see
// https://docs.nvidia.com/openshell/reference/policy-schema (network_policies, endpoints).
const (
	allowedDomainsEndpointProtocol    = "rest"
	allowedDomainsEndpointEnforcement = "enforce"
	allowedDomainsEndpointAccess      = "full" // all HTTP methods and paths
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
	discordRESTRules = []*sandboxv1.L7Rule{
		{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/**"}},
		{Allow: &sandboxv1.L7Allow{Method: "POST", Path: "/**"}},
		{Allow: &sandboxv1.L7Allow{Method: "PUT", Path: "/**"}},
		{Allow: &sandboxv1.L7Allow{Method: "PATCH", Path: "/**"}},
		{Allow: &sandboxv1.L7Allow{Method: "DELETE", Path: "/api/v*/channels/*/messages/*"}},
		{Allow: &sandboxv1.L7Allow{Method: "DELETE", Path: "/api/v*/channels/*/messages/*/reactions/*/*"}},
	}
	discordCDNGETRules = []*sandboxv1.L7Rule{
		{Allow: &sandboxv1.L7Allow{Method: "GET", Path: "/**"}},
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
		Protocol:    allowedDomainsEndpointProtocol,
		Enforcement: allowedDomainsEndpointEnforcement,
		Rules:       rules,
	}
}

// messengerChannelNodeBinaries for Telegram / Discord / Slack OpenClaw channel egress (Node runtime).
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
		Access: allowedDomainsEndpointAccess,
		Tls:    "skip",
	}
}

// Hosts covered by npmYarnNetworkPolicyRule (L4 CONNECT / undici); omit from kagent_allowed_domains for claw sandboxes.
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

// omitNPMPresetRegistryHosts drops registry hosts handled by npm_yarn when merging user allowedDomains (claw only).
func omitNPMPresetRegistryHosts(domains []string) []string {
	if len(domains) == 0 {
		return domains
	}
	out := make([]string, 0, len(domains))
	for _, raw := range domains {
		host, ok := normalizeAllowedDomainHost(raw)
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

// telegramBotNetworkPolicyRule egress for Telegram Bot API when spec.channels includes telegram.
func telegramBotNetworkPolicyRule() *sandboxv1.NetworkPolicyRule {
	return &sandboxv1.NetworkPolicyRule{
		Name:      "telegram_bot",
		Endpoints: []*sandboxv1.NetworkEndpoint{restNetworkEndpoint("api.telegram.org", telegramBotHTTPRules)},
		Binaries:  telegramBotPolicyBinaries,
	}
}

func discordNetworkPolicyRule() *sandboxv1.NetworkPolicyRule {
	return &sandboxv1.NetworkPolicyRule{
		Name: "discord",
		Endpoints: []*sandboxv1.NetworkEndpoint{
			restNetworkEndpoint("discord.com", discordRESTRules),
			wssTunnelEndpoint("gateway.discord.gg"),
			restNetworkEndpoint("cdn.discordapp.com", discordCDNGETRules),
			restNetworkEndpoint("media.discordapp.net", discordCDNGETRules),
		},
		Binaries: messengerChannelNodeBinaries,
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

func channelSpecPresent(ch v1alpha2.SandboxChannel) bool {
	switch ch.Type {
	case v1alpha2.SandboxChannelTypeTelegram:
		return ch.Telegram != nil
	case v1alpha2.SandboxChannelTypeDiscord:
		return ch.Discord != nil
	case v1alpha2.SandboxChannelTypeSlack:
		return ch.Slack != nil
	default:
		return false
	}
}

func sandboxHasChannelType(sbx *v1alpha2.Sandbox, typ v1alpha2.SandboxChannelType) bool {
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

// openClawDefaultNetworkPolicies returns fixed egress rules for OpenClaw CLI (registry + docs).
func openClawDefaultNetworkPolicies() map[string]*sandboxv1.NetworkPolicyRule {
	return map[string]*sandboxv1.NetworkPolicyRule{
		openClawNetworkPolicyKeyClawhub: {
			Name: "clawhub",
			Endpoints: []*sandboxv1.NetworkEndpoint{
				restNetworkEndpoint(openClawRegistryHostClawhub, l7WildcardGETPOST),
			},
			Binaries: openClawCLIAndNodeBinaries,
		},
		openClawNetworkPolicyKeyAPI: {
			Name: "openclaw_api",
			Endpoints: []*sandboxv1.NetworkEndpoint{
				restNetworkEndpoint(openClawRegistryHostAPI, l7WildcardGETPOST),
			},
			Binaries: openClawCLIAndNodeBinaries,
		},
		openClawNetworkPolicyKeyDocs: {
			Name: "openclaw_docs",
			Endpoints: []*sandboxv1.NetworkEndpoint{
				restNetworkEndpoint(openClawRegistryHostDocs, l7WildcardGETOnly),
			},
			Binaries: openClawCLIBinariesOnly,
		},
	}
}

func isClawSandboxBackend(b v1alpha2.SandboxBackendType) bool {
	return b == v1alpha2.SandboxBackendOpenClaw || b == v1alpha2.SandboxBackendNemoClaw
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

// Processes allowed to use the allowedDomains endpoints (NetworkPolicyRule.binaries is required).
// Paths support * / ** globs per policy schema.
//
// OpenShell denies egress unless the executable matches (e.g. curl must be listed explicitly;
// npm/node alone does not cover manual curl tests).
var defaultAllowedDomainsBinaries = []*sandboxv1.NetworkBinary{
	{Path: "/usr/bin/node"},
	{Path: "/usr/local/bin/node"},
	{Path: "/usr/bin/npm"},
	{Path: "/usr/bin/npx"},
	{Path: "/usr/bin/curl"},
	{Path: "/usr/bin/wget"},
	{Path: "/usr/bin/git"},
	{Path: "/sandbox/**"},
}

// sandboxName is the deterministic name used on the gateway. Format:
// "<namespace>-<name>". Collisions across clusters sharing one gateway are
// a known limitation.
func sandboxName(sbx *v1alpha2.Sandbox) string {
	return fmt.Sprintf("%s-%s", sbx.Namespace, sbx.Name)
}

// sandboxBackendHandleID is ObjectMeta.name — the canonical lookup key for
// GetSandbox / DeleteSandbox (same string as CreateSandboxRequest.Name).
func sandboxBackendHandleID(sb *openshellv1.Sandbox) string {
	if sb == nil || sb.GetMetadata() == nil {
		return ""
	}
	return strings.TrimSpace(sb.GetMetadata().GetName())
}

// normalizeAllowedDomainHost trims a CR entry into a hostname/glob suitable for
// sandbox.v1.NetworkEndpoint.host (see sandbox.proto). URLs and host:port forms are accepted.
func normalizeAllowedDomainHost(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			return "", false
		}
		return u.Hostname(), true
	}
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
		if s == "" {
			return "", false
		}
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}

// allowedDomainsNetworkPolicyRule builds one NetworkPolicyRule from CR allowedDomains.
func allowedDomainsNetworkPolicyRule(domains []string) *sandboxv1.NetworkPolicyRule {
	endpoints := make([]*sandboxv1.NetworkEndpoint, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, raw := range domains {
		host, ok := normalizeAllowedDomainHost(raw)
		if !ok {
			continue
		}
		key := strings.ToLower(host)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		endpoints = append(endpoints, &sandboxv1.NetworkEndpoint{
			Host: host,
			// HTTPS APIs and occasional HTTP redirects.
			Ports: []uint32{443, 80},
			// L7 REST policy: method/path space defaults to full allow via `access`
			// (mutually exclusive with explicit rules in the schema).
			Protocol:    allowedDomainsEndpointProtocol,
			Enforcement: allowedDomainsEndpointEnforcement,
			Access:      allowedDomainsEndpointAccess,
		})
	}
	if len(endpoints) == 0 {
		return nil
	}
	return &sandboxv1.NetworkPolicyRule{
		Name:      kagentAllowedDomainsNetworkPolicyKey,
		Endpoints: endpoints,
		Binaries:  defaultAllowedDomainsBinaries,
	}
}

// sandboxPolicyFromAllowedDomains builds a policy containing only the user allowedDomains rule (tests).
func sandboxPolicyFromAllowedDomains(domains []string) *sandboxv1.SandboxPolicy {
	rule := allowedDomainsNetworkPolicyRule(domains)
	if rule == nil {
		return nil
	}
	return &sandboxv1.SandboxPolicy{
		Version: 1,
		NetworkPolicies: map[string]*sandboxv1.NetworkPolicyRule{
			kagentAllowedDomainsNetworkPolicyKey: rule,
		},
	}
}

// sandboxPolicyForCreateRequest merges OpenClaw defaults (registry/docs, static sandbox policy) with optional allowedDomains.
func sandboxPolicyForCreateRequest(sbx *v1alpha2.Sandbox) *sandboxv1.SandboxPolicy {
	net := map[string]*sandboxv1.NetworkPolicyRule{}
	var fs *sandboxv1.FilesystemPolicy
	var landlock *sandboxv1.LandlockPolicy
	var process *sandboxv1.ProcessPolicy
	if sbx != nil && isClawSandboxBackend(sbx.Spec.Backend) {
		for k, r := range openClawDefaultNetworkPolicies() {
			net[k] = r
		}
		net[openClawNetworkPolicyKeyNPMYarn] = npmYarnNetworkPolicyRule()
		fs = defaultClawFilesystemPolicy()
		landlock = defaultClawLandlockPolicy()
		process = defaultClawProcessPolicy()
	}
	if sandboxHasChannelType(sbx, v1alpha2.SandboxChannelTypeTelegram) {
		net[openClawNetworkPolicyKeyTelegramBot] = telegramBotNetworkPolicyRule()
	}
	if sandboxHasChannelType(sbx, v1alpha2.SandboxChannelTypeDiscord) {
		net[openClawNetworkPolicyKeyDiscord] = discordNetworkPolicyRule()
	}
	if sandboxHasChannelType(sbx, v1alpha2.SandboxChannelTypeSlack) {
		net[openClawNetworkPolicyKeySlack] = slackNetworkPolicyRule()
	}
	domainList := extractAllowedDomains(sbx)
	if sbx != nil && isClawSandboxBackend(sbx.Spec.Backend) {
		domainList = omitNPMPresetRegistryHosts(domainList)
	}
	if rule := allowedDomainsNetworkPolicyRule(domainList); rule != nil {
		net[kagentAllowedDomainsNetworkPolicyKey] = rule
	}
	if len(net) == 0 && fs == nil {
		return nil
	}
	return &sandboxv1.SandboxPolicy{
		Version:         1,
		Filesystem:      fs,
		Landlock:        landlock,
		Process:         process,
		NetworkPolicies: net,
	}
}

// buildOpenshellCreateRequest maps a kagent Sandbox into an OpenShell
// CreateSandboxRequest. unsupported collects Sandbox fields the gateway
// cannot currently express so callers can surface them as events.
func buildOpenshellCreateRequest(sbx *v1alpha2.Sandbox) (*openshellv1.CreateSandboxRequest, []string) {
	unsupported := []string{}
	tpl := &openshellv1.SandboxTemplate{}
	env := map[string]string{}

	if sbx.Spec.Image != "" {
		tpl.Image = sbx.Spec.Image
	}
	for _, e := range sbx.Spec.Env {
		if e.ValueFrom != nil {
			unsupported = append(unsupported, "env."+e.Name+".valueFrom")
			continue
		}
		env[e.Name] = e.Value
	}
	spec := &openshellv1.SandboxSpec{
		Environment: env,
		Template:    tpl,
	}
	if pol := sandboxPolicyForCreateRequest(sbx); pol != nil {
		spec.Policy = pol
	}

	return &openshellv1.CreateSandboxRequest{
		Name: sandboxName(sbx),
		Spec: spec,
	}, unsupported
}

func extractAllowedDomains(sbx *v1alpha2.Sandbox) []string {
	if sbx == nil || sbx.Spec.Network == nil {
		return nil
	}
	return sbx.Spec.Network.AllowedDomains
}

// phaseToCondition maps OpenShell SandboxPhase + status message into a
// (Ready status, reason, message) triple for Sandbox.Status.
func phaseToCondition(sb *openshellv1.Sandbox) (metav1.ConditionStatus, string, string) {
	if sb == nil {
		return metav1.ConditionUnknown, "SandboxNotFound", "no sandbox returned by gateway"
	}
	msg := summarizeConditions(sb.GetStatus())
	switch sb.GetPhase() {
	case openshellv1.SandboxPhase_SANDBOX_PHASE_READY:
		return metav1.ConditionTrue, "SandboxReady", msg
	case openshellv1.SandboxPhase_SANDBOX_PHASE_PROVISIONING:
		return metav1.ConditionFalse, "SandboxProvisioning", msg
	case openshellv1.SandboxPhase_SANDBOX_PHASE_ERROR:
		return metav1.ConditionFalse, "SandboxError", msg
	case openshellv1.SandboxPhase_SANDBOX_PHASE_DELETING:
		return metav1.ConditionFalse, "SandboxDeleting", msg
	case openshellv1.SandboxPhase_SANDBOX_PHASE_UNKNOWN, openshellv1.SandboxPhase_SANDBOX_PHASE_UNSPECIFIED:
		return metav1.ConditionUnknown, "SandboxPhaseUnknown", msg
	default:
		return metav1.ConditionUnknown, "SandboxPhaseUnrecognized", fmt.Sprintf("unrecognized phase %s", sb.GetPhase())
	}
}

func summarizeConditions(s *openshellv1.SandboxStatus) string {
	if s == nil {
		return ""
	}
	parts := make([]string, 0, len(s.GetConditions()))
	for _, c := range s.GetConditions() {
		if c.GetMessage() != "" {
			parts = append(parts, fmt.Sprintf("%s=%s: %s", c.GetType(), c.GetStatus(), c.GetMessage()))
		}
	}
	return strings.Join(parts, "; ")
}

// endpointFor returns a connection hint surfaced on Sandbox.Status.Connection.
// For OpenShell the gateway URL itself is the addressable endpoint — clients
// use it together with the sandbox name to Exec/SSH in.
func endpointFor(gatewayURL, sandboxID string) string {
	if gatewayURL == "" {
		return ""
	}
	return fmt.Sprintf("%s#%s", gatewayURL, sandboxID)
}
