package egress

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// ParseOrigin validates an HTTP(S) DNS origin and makes its port explicit.
// Wildcards replace one leftmost label and cannot cover an entire top-level domain.
func ParseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" || u.Path != "" || u.ForceQuery || u.RawQuery != "" || strings.Contains(raw, "#") || strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("invalid egress origin %q: expected scheme, DNS hostname and optional port", raw)
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	dns := strings.TrimPrefix(host, "*.")
	_, ipErr := netip.ParseAddr(dns)
	if ipErr == nil || len(validation.IsDNS1123Subdomain(dns)) != 0 || (strings.HasPrefix(host, "*.") && !strings.Contains(dns, ".")) {
		return nil, fmt.Errorf("invalid egress origin %q: expected a DNS name or leftmost-label wildcard", raw)
	}
	portText := u.Port()
	if portText == "" {
		portText = "80"
		if u.Scheme == "https" {
			portText = "443"
		}
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return nil, fmt.Errorf("invalid egress port in %q", raw)
	}
	u.Host = net.JoinHostPort(host, strconv.FormatUint(port, 10))
	return u, nil
}

// Access is a compiled network grant. It contains references,
// never Secret values.
type Access struct {
	Destinations []string     `json:"destinations,omitempty"`
	Credentials  []Credential `json:"credentials,omitempty"`
	// TokenPassthroughTargets bind invocation credentials to destination headers.
	TokenPassthroughTargets []egresspolicy.GatewayTarget `json:"tokenPassthroughTargets,omitempty"`
}

// CanonicalAccess merges equivalent origins and validates destinations,
// credentials and tokenPassthrough targets. One origin/header has one source.
func CanonicalAccess(destinations []string, credentials []Credential, targets []egresspolicy.GatewayTarget) (Access, error) {
	result := Access{}
	protocols := map[string]string{}
	allowed := map[string]bool{}
	for _, raw := range destinations {
		u, err := ParseOrigin(raw)
		if err != nil {
			return Access{}, err
		}
		if scheme, exists := protocols[u.Host]; exists && scheme != u.Scheme {
			return Access{}, fmt.Errorf("egress destination %q uses both HTTP and HTTPS on the same port", u.Host)
		}
		protocols[u.Host] = u.Scheme
		allowed[u.String()] = true
	}
	for origin := range allowed {
		result.Destinations = append(result.Destinations, origin)
	}
	slices.Sort(result.Destinations)
	if len(result.Destinations) > 256 {
		return Access{}, fmt.Errorf("egress policy exceeds 256 rules")
	}
	var err error
	result.Credentials, err = CanonicalCredentials(credentials)
	if err != nil {
		return Access{}, err
	}
	result.TokenPassthroughTargets, err = CanonicalGatewayTargets(targets)
	if err != nil {
		return Access{}, err
	}
	reserved := map[string]bool{}
	for _, target := range result.TokenPassthroughTargets {
		if !allowed[target.Origin] {
			return Access{}, fmt.Errorf("passthrough destination %q is not explicitly allowed", target.Origin)
		}
		reserved[target.Origin+"\x00"+target.Header] = true
	}
	for _, binding := range result.Credentials {
		if !allowed[binding.Origin] {
			return Access{}, fmt.Errorf("credential destination %q is not explicitly allowed", binding.Origin)
		}
		if reserved[binding.Origin+"\x00"+binding.Header] {
			return Access{}, fmt.Errorf("destination %q cannot use two credential sources for the same header", binding.Origin)
		}
	}
	return result, nil
}
