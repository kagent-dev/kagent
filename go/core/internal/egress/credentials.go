package egress

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/http/httpguts"
	"k8s.io/apimachinery/pkg/util/validation"
)

// KubernetesSecretAuthority is the Substrate credential provider class, the
// authority of an ate-secret:// URI, that injects a Secret value unchanged.
const KubernetesSecretAuthority = "k8s.io"

// ValidCredentialAuthority reports whether authority can name a Substrate
// credential provider class: a DNS-1123 subdomain without a port that is not
// an IP address.
func ValidCredentialAuthority(authority string) bool {
	return len(validation.IsDNS1123Subdomain(authority)) == 0 && net.ParseIP(authority) == nil
}

// CredentialURI names one Secret data entry of the default locator for a
// Substrate credential provider class.
func CredentialURI(authority, namespace, name, key string) string {
	return "ate-secret://" + authority + "/default/" + namespace + "/" + name + "/" + key
}

// Credential binds a Secret reference to an exact destination and HTTP header.
// Values are fetched by the gateway and never enter a runtime revision.
type Credential struct {
	Origin string `json:"origin"`
	Header string `json:"header"`
	Prefix string `json:"prefix,omitempty"`
	URI    string `json:"uri"`
}

// CanonicalCredentials validates and orders bindings by complete origin and header.
func CanonicalCredentials(bindings []Credential) ([]Credential, error) {
	result := slices.Clone(bindings)
	for i := range result {
		c := &result[i]
		u, err := ParseOrigin(c.Origin)
		if err != nil || strings.Contains(c.Origin, "*") {
			return nil, fmt.Errorf("credential injection requires an exact HTTP(S) DNS origin, got %q", c.Origin)
		}
		c.Origin = u.String()
		c.Header = strings.ToLower(c.Header)
		if !validCredentialHeader(c.Header) || !httpguts.ValidHeaderFieldValue(c.Prefix) {
			return nil, fmt.Errorf("invalid credential injection header %q", c.Header)
		}
		u, err = url.Parse(c.URI)
		if err != nil || u.Scheme != "ate-secret" || !ValidCredentialAuthority(u.Host) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("invalid credential URI")
		}
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) != 4 || parts[0] != "default" || len(validation.IsDNS1123Label(parts[1])) != 0 || len(validation.IsDNS1123Subdomain(parts[2])) != 0 || parts[3] == "" || len(validation.IsConfigMapKey(parts[3])) != 0 {
			return nil, fmt.Errorf("credential URI must identify the default locator, namespace, Secret and key")
		}
	}
	slices.SortFunc(result, func(a, b Credential) int {
		return strings.Compare(a.Origin+"\x00"+a.Header, b.Origin+"\x00"+b.Header)
	})
	for i := 1; i < len(result); i++ {
		a, b := result[i-1], result[i]
		if a.Origin == b.Origin && a.Header == b.Header && a != b {
			return nil, fmt.Errorf("conflicting credentials for %s header %s", a.Origin, a.Header)
		}
	}
	result = slices.Compact(result)
	counts := map[string]int{}
	for _, c := range result {
		counts[c.Origin]++
		if counts[c.Origin] > 16 {
			return nil, fmt.Errorf("credential injection supports at most 16 headers per origin")
		}
	}
	return result, nil
}

// validCredentialHeader excludes routing, framing, hop-by-hop and cookie headers.
func validCredentialHeader(header string) bool {
	if len(header) > 256 || !httpguts.ValidHeaderFieldName(header) {
		return false
	}
	switch strings.ToLower(header) {
	case "host", "connection", "content-length", "transfer-encoding", "te", "trailer", "upgrade", "keep-alive", "proxy-authorization", "proxy-authenticate", "cookie":
		return false
	}
	return true
}
