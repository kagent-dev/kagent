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

// SchemeTLS names a TLS passthrough destination: TLS the gateway forwards
// without decryption, matched on the SNI server name and port.
const SchemeTLS = "tls"

// Origin retains the protocol and destination port needed by Substrate's
// egress rules, without including a URL's path, credentials, query, or fragment.
// Only HTTP(S) services are inferred: a URL of another scheme yields an opaque
// origin that ParseOrigin refuses, so a configured URL never grants TLS passthrough.
func Origin(u *url.URL) string {
	if u.Scheme != "http" && u.Scheme != "https" {
		return u.Scheme + ":" + u.Host
	}
	return canonicalOrigin(u)
}

// canonicalOrigin is scheme://host:port with a lowercase host and the scheme's
// default port made explicit.
func canonicalOrigin(u *url.URL) string {
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
// "https://proxy.golang.org" or "https://*.githubusercontent.com", or a TLS
// passthrough destination, such as "tls://db.example.com:5432", and returns
// its canonical form. Unlike Origin, it refuses anything beyond scheme, host
// and port, because a declared path or credential would silently widen to the
// whole origin. A tls:// origin requires its port; HTTP(S) defaults to 80 and 443.
func ParseOrigin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != SchemeTLS) || u.Opaque != "" || u.User != nil || u.Hostname() == "" ||
		u.Path != "" || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid egress origin %q: expected http(s)://host[:port] or tls://host:port", value)
	}
	if u.Scheme == SchemeTLS && u.Port() == "" {
		return "", fmt.Errorf("invalid egress origin %q: a tls:// origin requires a port", value)
	}
	if !ValidHostPattern(strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")) {
		return "", fmt.Errorf("invalid egress origin %q: the host must be a DNS name, optionally with \"*\" as its leftmost label", value)
	}
	if port := u.Port(); port != "" {
		if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
			return "", fmt.Errorf("invalid egress origin %q: invalid port", value)
		}
	}
	return canonicalOrigin(u), nil
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
