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
)

// Origin retains the protocol and destination port needed by Substrate's
// egress rules, without including a URL's path, credentials, query, or fragment.
func Origin(u *url.URL) string {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	}
	return (&url.URL{Scheme: u.Scheme, Host: host}).String()
}

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
}

// CanonicalAccess merges equivalent origins and validates destinations and
// credentials, including conflicts with canonical caller-token origins.
func CanonicalAccess(destinations []string, credentials []Credential, tokenPassthroughOrigins []string) (Access, error) {
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
	for _, binding := range result.Credentials {
		if !allowed[binding.Origin] {
			return Access{}, fmt.Errorf("credential destination %q is not explicitly allowed", binding.Origin)
		}
		if slices.Contains(tokenPassthroughOrigins, binding.Origin) {
			return Access{}, fmt.Errorf("destination %q cannot combine caller-token passthrough with gateway credentials", binding.Origin)
		}
	}
	return result, nil
}
