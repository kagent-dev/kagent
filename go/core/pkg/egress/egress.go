// Package egress holds a URL rewrite that adapts a RemoteMCPServer's URL for a
// plaintext egress hop.
//
// When a deployment routes outbound MCP traffic through an egress proxy that
// terminates TLS itself, the hop between the workload and that proxy is
// plaintext HTTP: a TLS handshake from the workload would reach the proxy as
// bytes it cannot parse as HTTP. Operators still author RemoteMCPServer.spec.url
// in its natural upstream form (`https://api.example.com/...`, or scheme-less
// `host:port/...`); RewriteURL rewrites it to `http://host:<effective-port>/...`
// so the workload opens a plaintext TCP connection while the proxy originates
// TLS upstream.
//
// The single entry point RewriteURL is called from two places, both gated on
// the egress feature flag: the agent translator emits it as a tool URL during
// config translation, and the controller's tool-discovery dial uses it for its
// probe. Because both call the same function on the same RMS, the agent's tool
// calls and the controller's probe resolve to an identical endpoint.
package egress

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

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

// RewriteURL rewrites an RMS's spec.url to its plaintext form: the RMS's
// effective, tls-aware host:port (see normalizedHostPort) with an http:// scheme.
// Callers gate on the egress feature flag; given the flag is on, the rewrite
// itself applies unconditionally.
//
// Both the agent translator (emitting a tool URL) and the controller's
// tool-discovery dial call this on the same RMS, so they resolve to an identical
// endpoint by construction. spec.urls that can't be parsed or carry a
// non-http(s) scheme pass through unchanged.
func RewriteURL(rms *v1alpha2.RemoteMCPServer) string {
	effective := normalizedHostPort(rms)
	if effective == "" {
		return rms.Spec.URL
	}
	return rewriteTo(rms.Spec.URL, effective)
}
