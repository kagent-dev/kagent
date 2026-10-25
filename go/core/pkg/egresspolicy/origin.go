package egresspolicy

import (
	"net"
	"net/url"
	"strconv"
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
		if number, err := strconv.ParseUint(port, 10, 16); err == nil {
			port = strconv.FormatUint(number, 10)
		}
		host = net.JoinHostPort(host, port)
	}
	return (&url.URL{Scheme: u.Scheme, Host: host}).String()
}
