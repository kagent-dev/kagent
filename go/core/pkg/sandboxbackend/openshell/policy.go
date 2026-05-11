package openshell

import (
	"strings"

	sandboxv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/sandboxv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/openclaw"
	"google.golang.org/protobuf/proto"
)

// Key used in SandboxPolicy.network_policies for spec.network.allowedDomains → SSRF/network rules.
const kagentAllowedDomainsNetworkPolicyKey = "kagent_allowed_domains"

// L7 REST settings for allowedDomains entries; see
// https://docs.nvidia.com/openshell/reference/policy-schema (network_policies, endpoints).
const (
	allowedDomainsEndpointProtocol    = "rest"
	allowedDomainsEndpointEnforcement = "enforce"
	allowedDomainsEndpointAccess      = "full" // all HTTP methods and paths
)

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

// allowedDomainsNetworkPolicyRule builds one NetworkPolicyRule from CR allowedDomains.
func allowedDomainsNetworkPolicyRule(domains []string) *sandboxv1.NetworkPolicyRule {
	endpoints := make([]*sandboxv1.NetworkEndpoint, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, raw := range domains {
		host, ok := sandboxbackend.NormalizeAllowedDomainHost(raw)
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

func extractAllowedDomains(sbx *v1alpha2.AgentHarness) []string {
	if sbx == nil || sbx.Spec.Network == nil {
		return nil
	}
	return sbx.Spec.Network.AllowedDomains
}

// applyAllowedDomainsPolicy merges spec.network.allowedDomains into kagent_allowed_domains (deduped hosts).
// For Claw backends, npm/yarn registry hosts are omitted because npm_yarn covers them.
func applyAllowedDomainsPolicy(sbx *v1alpha2.AgentHarness, net map[string]*sandboxv1.NetworkPolicyRule) {
	domainList := extractAllowedDomains(sbx)
	if sbx != nil && openclaw.IsClawSandboxBackend(sbx.Spec.Backend) {
		domainList = openclaw.OmitNPMPresetRegistryHosts(domainList)
	}
	if rule := allowedDomainsNetworkPolicyRule(domainList); rule != nil {
		net[kagentAllowedDomainsNetworkPolicyKey] = rule
	}
}

// mergeOpenShellSandboxPolicies merges two OpenShell SandboxPolicy fragments for AgentHarness provisioning.
// Network policy rules: overlay keys replace base keys. Filesystem, landlock, and process: overlay replaces
// base only when the overlay sets a non-nil value (for future user-defined CRD policy).
func mergeOpenShellSandboxPolicies(base, overlay *sandboxv1.SandboxPolicy) *sandboxv1.SandboxPolicy {
	if base == nil {
		return cloneSandboxPolicy(overlay)
	}
	if overlay == nil {
		return cloneSandboxPolicy(base)
	}
	out := proto.Clone(base).(*sandboxv1.SandboxPolicy)
	if out.NetworkPolicies == nil {
		out.NetworkPolicies = map[string]*sandboxv1.NetworkPolicyRule{}
	}
	for k, v := range overlay.GetNetworkPolicies() {
		out.NetworkPolicies[k] = proto.Clone(v).(*sandboxv1.NetworkPolicyRule)
	}
	if overlay.GetFilesystem() != nil {
		out.Filesystem = proto.Clone(overlay.Filesystem).(*sandboxv1.FilesystemPolicy)
	}
	if overlay.GetLandlock() != nil {
		out.Landlock = proto.Clone(overlay.Landlock).(*sandboxv1.LandlockPolicy)
	}
	if overlay.GetProcess() != nil {
		out.Process = proto.Clone(overlay.Process).(*sandboxv1.ProcessPolicy)
	}
	return out
}

func cloneSandboxPolicy(p *sandboxv1.SandboxPolicy) *sandboxv1.SandboxPolicy {
	if p == nil {
		return nil
	}
	return proto.Clone(p).(*sandboxv1.SandboxPolicy)
}

// sandboxPolicyFragmentFromNetwork applies fill to a fresh network map and returns a versioned policy, or nil if no rules were added.
func sandboxPolicyFragmentFromNetwork(ah *v1alpha2.AgentHarness, fill func(*v1alpha2.AgentHarness, map[string]*sandboxv1.NetworkPolicyRule)) *sandboxv1.SandboxPolicy {
	if ah == nil {
		return nil
	}
	net := map[string]*sandboxv1.NetworkPolicyRule{}
	fill(ah, net)
	if len(net) == 0 {
		return nil
	}
	return &sandboxv1.SandboxPolicy{Version: openclaw.SandboxPolicyVersion, NetworkPolicies: net}
}

// openShellSandboxPolicyForAgentHarness builds the effective OpenShell SandboxPolicy from AgentHarness spec:
// claw/nemoclaw get static baseline + npm/registry nets; any backend may merge Telegram/Slack egress when
// channels are set (CRD validation restricts channels to claw types, but translation stays liberal).
// User allowedDomains are merged last. OpenClaw-specific fragments live in package openclaw.
func openShellSandboxPolicyForAgentHarness(ah *v1alpha2.AgentHarness) *sandboxv1.SandboxPolicy {
	if ah == nil {
		return nil
	}
	var pol *sandboxv1.SandboxPolicy
	if openclaw.IsClawSandboxBackend(ah.Spec.Backend) {
		pol = mergeOpenShellSandboxPolicies(openclaw.BaselineSandboxPolicy(), openclaw.ChannelNetworkPolicyFragment(ah))
	} else {
		pol = openclaw.ChannelNetworkPolicyFragment(ah)
	}
	pol = mergeOpenShellSandboxPolicies(pol, sandboxPolicyFragmentFromNetwork(ah, applyAllowedDomainsPolicy))
	if pol == nil {
		return nil
	}
	if len(pol.GetNetworkPolicies()) == 0 && pol.GetFilesystem() == nil && pol.GetLandlock() == nil && pol.GetProcess() == nil {
		return nil
	}
	return pol
}
