package egress

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
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

// ParseOrigin validates a declared HTTP(S) origin, such as
// "https://proxy.golang.org" or "https://*.githubusercontent.com", and returns
// its canonical form. Unlike Origin, it refuses anything beyond scheme, host
// and port, because a declared path or credential would silently widen to the
// whole origin.
func ParseOrigin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.User != nil || u.Hostname() == "" ||
		u.Path != "" || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid egress origin %q: expected http(s)://host[:port]", value)
	}
	if !ValidHostPattern(strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")) {
		return "", fmt.Errorf("invalid egress origin %q: the host must be a DNS name, optionally with \"*\" as its leftmost label", value)
	}
	if port := u.Port(); port != "" {
		if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
			return "", fmt.Errorf("invalid egress origin %q: invalid port", value)
		}
	}
	return Origin(u), nil
}

// ValidHostPattern reports whether a normalized host is a DNS name, or a DNS
// name with "*" as its leftmost label, which is what Substrate's hostname
// rules match. An IP address and "*" alone are not patterns, and a wildcard
// needs two labels under it: "*.com" would open a whole top-level domain.
func ValidHostPattern(host string) bool {
	name, wildcard := strings.CutPrefix(host, "*.")
	if _, err := netip.ParseAddr(name); err == nil {
		return false
	}
	if wildcard && !strings.Contains(name, ".") {
		return false
	}
	return len(validation.IsDNS1123Subdomain(name)) == 0
}
