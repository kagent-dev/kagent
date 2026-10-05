package substrate

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

// ActorEgressPolicy compiles HTTP(S) origins into an actor's default allowlist.
// Credential bindings are already canonicalized by the store.
func ActorEgressPolicy(atespace string, destinations []string, credentials []egress.Credential) (*ateapipb.EgressPolicy, error) {
	type originTarget struct {
		scheme, hostname string
		port             int32
	}
	origins := map[string]originTarget{}
	protocols := map[string]string{}
	hosts := map[string]bool{}
	for _, destination := range destinations {
		if u, err := url.Parse(destination); err == nil {
			if _, err := netip.ParseAddr(strings.TrimSuffix(u.Hostname(), ".")); err == nil {
				return nil, fmt.Errorf("egress destination %q requires a DNS name: Substrate does not support IP allowlists", destination)
			}
		}
		origin, err := egress.ParseOrigin(destination)
		if err != nil {
			return nil, fmt.Errorf("invalid egress destination: %w", err)
		}
		// ParseOrigin returns scheme://host:port with a valid port; the port is
		// re-encoded so "0443" and "443" are one origin.
		u, _ := url.Parse(origin)
		host := u.Hostname()
		port, _ := strconv.ParseUint(u.Port(), 10, 16)
		u.Host = net.JoinHostPort(host, strconv.FormatUint(port, 10))
		if protocol, found := protocols[u.Host]; found && protocol != u.Scheme {
			return nil, fmt.Errorf("egress destination %q uses both HTTP and HTTPS on the same port", u.Host)
		}
		protocols[u.Host] = u.Scheme
		hosts[host] = true
		origins[u.String()] = originTarget{scheme: u.Scheme, hostname: host, port: int32(port)}
	}
	effects := map[string]*ateapipb.HttpRuleEffects{}
	for _, binding := range credentials {
		if !hosts[binding.Hostname] {
			return nil, fmt.Errorf("credential destination %q is not allowed", binding.Hostname)
		}
		if effects[binding.Hostname] == nil {
			effects[binding.Hostname] = &ateapipb.HttpRuleEffects{}
		}
		effects[binding.Hostname].ReplaceHeaders = append(effects[binding.Hostname].ReplaceHeaders, &ateapipb.CredentialHeader{Header: binding.Header, Prefix: binding.Prefix, CredentialUri: binding.URI})
	}
	names := make([]string, 0, len(origins))
	for origin := range origins {
		names = append(names, origin)
	}
	slices.Sort(names)
	policy := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: "default"}}
	for _, origin := range names {
		target := origins[origin]
		ports := &ateapipb.Ports{Numbers: []int32{target.port}}
		// Keep exactly one rule per host pattern and port. A wildcard rule can
		// overlap an exact one, but Substrate decides on the exact hostname
		// first (EgressPolicy in proto/ateapi.proto), and credentials bind only
		// to exact hostnames, so a wildcard cannot bypass credential replacement.
		if target.scheme == "https" {
			policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Https: &ateapipb.HTTPSRule{Hostnames: []string{target.hostname}, Ports: ports, Effects: effects[target.hostname]}})
		} else {
			policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Http: &ateapipb.HTTPRule{Hostnames: []string{target.hostname}, Ports: ports, Effects: effects[target.hostname]}})
		}
	}
	if len(policy.Rules) > 256 {
		return nil, fmt.Errorf("egress policy exceeds 256 rules")
	}
	return policy, nil
}
