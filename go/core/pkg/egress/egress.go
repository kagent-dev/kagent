// Package egress holds a URL rewrite that adapts RemoteMCPServer tool/dial URLs
// for a plaintext egress hop.
//
// When a deployment routes outbound MCP traffic through an egress proxy that
// terminates TLS itself, the hop between the workload and that proxy is
// plaintext HTTP: a TLS handshake from the workload would reach the proxy as
// bytes it cannot parse as HTTP. Operators still author RemoteMCPServer.spec.url
// in its natural upstream form (`https://api.example.com/...`, or scheme-less
// `host:port/...`); this package rewrites the workload-side URL to
// `http://host:<effective-port>/...` so the workload opens a plaintext TCP
// connection while the proxy originates TLS upstream.
//
// Both entry points resolve a given spec.url to the SAME plaintext URL, using
// the RMS's effective (tls-aware) port, so the agent's tool calls and the
// controller's discovery probe dial an identical endpoint. It is gated by a
// feature flag and used in two places: the agent translator calls
// RewriteConfigForAgent in its config-phase, before the config Secret is
// serialized; the controller's tool-discovery dial uses RewriteDialURL on its
// probe URL.
package egress

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RewriteConfigForAgent rewrites every RemoteMCPServer-backed tool URL in cfg
// to its plaintext form. Called inline by the agent translator's
// config-phase (before the config Secret is serialized and the config-hash is
// computed) when the egress feature flag is on — the caller checks the flag,
// so this function itself applies unconditionally. Skill-only / MCPServer-only
// / Service-only agents resolve to an empty RMS set and are a no-op.
func RewriteConfigForAgent(ctx context.Context, c client.Client, agent v1alpha2.AgentObject, cfg *adk.AgentConfig) error {
	if cfg == nil {
		return nil
	}
	rmsHosts, err := collectRMSHosts(ctx, c, agent)
	if err != nil {
		return fmt.Errorf("collect RemoteMCPServer hosts for agent %s/%s: %w",
			agent.GetNamespace(), agent.GetName(), err)
	}
	if len(rmsHosts) == 0 {
		return nil
	}
	rewriteConfig(cfg, rmsHosts)
	return nil
}

// rewriteConfig rewrites RMS-matched tool URLs to their plaintext form in place
// on cfg.HttpTools/SseTools. URLs that don't appear in rmsHosts are left
// untouched.
//
// When --proxy-url is also set, the translator redirects in-cluster RMS tool
// URLs to the global proxy host (see applyProxyURL) during CompileAgent —
// before this config-phase rewrite runs — so by the time this sees
// the cfg, those tool URLs no longer equal any RMS spec.url, the lookup misses,
// and they are intentionally left alone: proxy routing takes precedence for
// internal-k8s URLs. External https upstreams — the egress target — are never
// proxied, so they still match and get rewritten.
func rewriteConfig(cfg *adk.AgentConfig, rmsHosts map[string]string) {
	if cfg == nil {
		return
	}
	for i := range cfg.HttpTools {
		cfg.HttpTools[i].Params.Url = rewriteHTTPSForEgress(cfg.HttpTools[i].Params.Url, rmsHosts)
	}
	for i := range cfg.SseTools {
		cfg.SseTools[i].Params.Url = rewriteHTTPSForEgress(cfg.SseTools[i].Params.Url, rmsHosts)
	}
}

// collectRMSHosts maps every RemoteMCPServer referenced by the agent's MCP tool
// refs to its plaintext-rewrite target. The map key is the RMS's spec.url
// string verbatim — which equals the tool URL the translator emits into the
// agent config (see translateStreamableHttpTool / translateSseHttpTool), so the
// rewrite's lookup is an exact string match. The value is the RMS's effective,
// tls-aware host:port (see normalizedHostPort).
//
// Keying on the full spec.url means each RMS owns its own entry; two RMSs that
// share a hostname but differ in path or scheme don't collide. The only way to
// reach a collision is to author two distinct RMS objects with byte-identical
// spec.url strings, which is itself a misconfiguration of the RMS resources
// rather than something the rewrite needs to police.
//
// Refs that fail to resolve (NotFound) are skipped — RMS presence is validated
// upstream, so reaching this code with a missing RMS means a transient mismatch
// that reconverges on the next reconcile. Other client errors propagate so the
// caller re-tries.
func collectRMSHosts(ctx context.Context, c client.Client, agent v1alpha2.AgentObject) (map[string]string, error) {
	out := map[string]string{}
	spec := agent.GetAgentSpec()
	if spec == nil || spec.Declarative == nil {
		return out, nil
	}
	for _, tool := range spec.Declarative.Tools {
		if tool == nil || tool.McpServer == nil {
			continue
		}
		if !isRemoteMCPServerKind(tool.McpServer.GroupKind()) {
			continue
		}
		ref := tool.McpServer.NamespacedName(agent.GetNamespace())
		rms := &v1alpha2.RemoteMCPServer{}
		if err := c.Get(ctx, ref, rms); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("get RemoteMCPServer %s: %w", ref, err)
		}
		effective := normalizedHostPort(rms)
		if effective == "" {
			continue
		}
		out[rms.Spec.URL] = effective
	}
	return out, nil
}

// isRemoteMCPServerKind reports whether the GroupKind names a RemoteMCPServer.
// The empty group is accepted because the translator treats it as a shorthand
// for the kagent.dev group (see translateMCPServerTarget).
func isRemoteMCPServerKind(gk schema.GroupKind) bool {
	if gk.Kind != "RemoteMCPServer" {
		return false
	}
	return gk.Group == "" || gk.Group == "kagent.dev"
}

// ParseRemoteMCPServerURL parses spec.url accepting an explicit http(s)://
// scheme or no scheme at all. Scheme-less inputs are reparsed as
// `//host[:port]/path` so net/url's host/port extraction works without the
// operator having to guess a scheme prefix.
func ParseRemoteMCPServerURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("empty url")
	}
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	return url.Parse(raw)
}

// EffectiveScheme returns "http" or "https" — the scheme the controller (and
// any TLS-originating proxy) should use for this RMS.
//
// A non-nil spec.tls is the primary signal that the operator opted into TLS;
// an empty struct (`spec.tls: {}`) counts as opt-in (system trust defaults)
// and is treated identically to an absent spec.tls when the URL itself
// declares https. Explicit https:// in spec.url is honored as a separate TLS
// signal. Anything else — http:// URL with nil tls, scheme-less with nil tls,
// or unparseable — returns "http".
//
// Contract: only the http:// URL with non-nil spec.tls combination is rejected
// by CRD validation. https:// with nil/empty spec.tls is admitted (defaults to
// system trust on the agent side); the URL scheme alone carries the TLS
// signal here.
func EffectiveScheme(rms *v1alpha2.RemoteMCPServer) string {
	if rms.Spec.TLS != nil {
		return "https"
	}
	parsed, err := ParseRemoteMCPServerURL(rms.Spec.URL)
	if err == nil && parsed.Scheme == "https" {
		return "https"
	}
	return "http"
}

// EffectivePort returns the upstream port for an RMS as an int32 (the width
// typed k8s API port fields use). An explicit port in spec.url wins; otherwise the default is
// 443 when EffectiveScheme is "https", else 80. Returns 0 when spec.url can't
// be parsed, has no host, or carries an out-of-range / non-numeric port.
func EffectivePort(rms *v1alpha2.RemoteMCPServer) int32 {
	parsed, err := ParseRemoteMCPServerURL(rms.Spec.URL)
	if err != nil || parsed.Hostname() == "" {
		return 0
	}
	if p := parsed.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return 0
		}
		return int32(n)
	}
	if EffectiveScheme(rms) == "https" {
		return 443
	}
	return 80
}

// normalizedHostPort returns the RemoteMCPServer's "host:port" with the port
// resolved via EffectivePort (tls-aware). Returns "" when spec.url can't be
// parsed, has no host, or carries a non-http(s) scheme. Scheme-less spec.url
// values (e.g. `host.docker.internal:13443/mcp`) are accepted.
func normalizedHostPort(rms *v1alpha2.RemoteMCPServer) string {
	parsed, err := ParseRemoteMCPServerURL(rms.Spec.URL)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		// spec.url is only length-validated (MinLength=1), not scheme-checked,
		// at admission — so guard against a non-http(s) scheme here rather than
		// assume it was rejected upstream.
		return ""
	}
	host := parsed.Hostname()
	if host == "" {
		return ""
	}
	port := EffectivePort(rms)
	if port == 0 {
		return ""
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port)))
}

// rewriteTo parses raw and returns it rewritten to `http://<hostPort>/path?…#frag`,
// preserving everything but the scheme and host. Returns raw unchanged when it
// can't be parsed or has no host.
func rewriteTo(raw, hostPort string) string {
	parsed, err := ParseRemoteMCPServerURL(raw)
	if err != nil || parsed.Hostname() == "" {
		return raw
	}
	out := *parsed
	out.Scheme = "http"
	out.Host = hostPort
	return out.String()
}

// rewriteHTTPSForEgress rewrites an agent tool URL to its plaintext form when
// it matches an RMS-backed entry in rmsHosts.
//
// rmsHosts is keyed on the RMS's spec.url string (which equals the tool URL
// emitted by the translator) and valued with the RMS's effective, tls-aware
// host:port. Lookup is exact string match: each RMS owns its own entry, so two
// RMSs that share a hostname but differ in path or scheme are rewritten
// independently to their own effective ports.
//
// URLs that don't appear in rmsHosts are returned unchanged. This is also how
// proxy-rewritten tool URLs (see applyProxyURL) end up unchanged — their host
// is the global proxy host, not any RMS spec.url, so the lookup misses.
func rewriteHTTPSForEgress(raw string, rmsHosts map[string]string) string {
	effective, matched := rmsHosts[raw]
	if !matched {
		return raw
	}
	return rewriteTo(raw, effective)
}

// RewriteDialURL rewrites an RMS's spec.url to its plaintext form for the
// controller's tool-discovery dial. Unlike rewriteHTTPSForEgress it is not
// allowlist-gated — the caller already holds the RemoteMCPServer, so spec.url is
// the RMS's own URL by construction.
//
// The rewrite targets the RMS's effective, tls-aware host:port (see
// normalizedHostPort) — the same value collectRMSHosts maps the agent's tool URL
// to — so the controller dials exactly the URL the agent config uses. spec.urls
// that can't be parsed or carry a non-http(s) scheme pass through unchanged.
func RewriteDialURL(rms *v1alpha2.RemoteMCPServer) string {
	effective := normalizedHostPort(rms)
	if effective == "" {
		return rms.Spec.URL
	}
	return rewriteTo(rms.Spec.URL, effective)
}
