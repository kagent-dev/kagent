package egress

import (
	"net"
	"net/url"
	"strings"
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
