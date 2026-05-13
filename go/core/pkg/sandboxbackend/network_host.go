package sandboxbackend

import (
	"net"
	"net/url"
	"strings"
)

// NormalizeAllowedDomainHost trims an AgentHarness allowedDomains entry into a hostname or glob
// suitable for sandbox.v1.NetworkEndpoint.host. URLs and host:port forms are accepted.
func NormalizeAllowedDomainHost(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			return "", false
		}
		return u.Hostname(), true
	}
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
		if s == "" {
			return "", false
		}
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}
