package egress

import (
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/http/httpguts"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Credential binds a Secret reference to an exact destination and HTTP header.
// Values are fetched by the gateway and never enter a runtime revision.
type Credential struct {
	Hostname string `json:"hostname"`
	Header   string `json:"header"`
	Prefix   string `json:"prefix,omitempty"`
	URI      string `json:"uri"`
}

// CanonicalCredentials validates and orders bindings. Substrate matches only
// hostnames, so two credentials for the same host and header cannot coexist.
func CanonicalCredentials(bindings []Credential) ([]Credential, error) {
	result := slices.Clone(bindings)
	for i := range result {
		c := &result[i]
		c.Hostname = strings.TrimSuffix(strings.ToLower(c.Hostname), ".")
		c.Header = strings.ToLower(c.Header)
		_, ipErr := netip.ParseAddr(c.Hostname)
		if ipErr == nil || len(validation.IsDNS1123Subdomain(c.Hostname)) != 0 {
			return nil, fmt.Errorf("credential injection requires an exact DNS hostname, got %q", c.Hostname)
		}
		if !httpguts.ValidHeaderFieldName(c.Header) || !httpguts.ValidHeaderFieldValue(c.Prefix) {
			return nil, fmt.Errorf("invalid credential injection header %q", c.Header)
		}
		u, err := url.Parse(c.URI)
		if err != nil || u.Scheme != "ate-secret" || u.Host != "kubernetes.io" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("invalid Kubernetes credential URI")
		}
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) != 3 || len(validation.IsDNS1123Label(parts[0])) != 0 || len(validation.IsDNS1123Subdomain(parts[1])) != 0 || parts[2] == "" || len(validation.IsConfigMapKey(parts[2])) != 0 {
			return nil, fmt.Errorf("credential URI must identify a namespace, Secret and key")
		}
	}
	slices.SortFunc(result, func(a, b Credential) int {
		return strings.Compare(a.Hostname+"\x00"+a.Header, b.Hostname+"\x00"+b.Header)
	})
	for i := 1; i < len(result); i++ {
		a, b := result[i-1], result[i]
		if a.Hostname == b.Hostname && a.Header == b.Header && a != b {
			return nil, fmt.Errorf("conflicting credentials for %s header %s", a.Hostname, a.Header)
		}
	}
	result = slices.Compact(result)
	counts := map[string]int{}
	for _, c := range result {
		counts[c.Hostname]++
		if counts[c.Hostname] > 16 {
			return nil, fmt.Errorf("credential injection supports at most 16 headers per hostname")
		}
	}
	return result, nil
}
