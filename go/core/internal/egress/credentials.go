package egress

import (
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/http/httpguts"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Substrate credential provider names, the authority of an ate-secret:// URI.
const (
	// KubernetesSecretAuthority injects a Secret value unchanged.
	KubernetesSecretAuthority = "k8s.io"
	// GoogleAccessTokenAuthority reads a Google service account key Secret and
	// injects a short-lived access token minted from it, so the key itself never
	// reaches the gateway or the runtime.
	GoogleAccessTokenAuthority = "google-access-token.k8s.io"
)

// CredentialURI names one Secret data entry of the default locator for a
// Substrate credential provider.
func CredentialURI(authority, namespace, name, key string) string {
	return "ate-secret://" + authority + "/default/" + namespace + "/" + name + "/" + key
}

// ScopeGolden scopes a binding to the golden boot of a revision's
// ActorTemplate: the actors of its sessions are compiled without it.
const ScopeGolden = "golden"

// ActorJWTLifetime is how long each actor JWT the gateway mints for an
// Audience binding lives, within Substrate's 300 to 3600 seconds.
const ActorJWTLifetime = 15 * time.Minute

// Credential binds a credential to an exact destination and HTTP header: a
// Secret reference (URI), or the Actor's own JWT bound to Audience, which the
// gateway mints for the Actor that sent the request. Values are fetched or
// minted by the gateway and never enter a runtime revision or the actor.
// PathPrefix narrows the binding to the request paths under it; of one host's
// bindings for a header, the gateway applies the longest matching prefix and
// the one without a prefix otherwise. Scope names the actors the binding is
// for: empty for every actor, ScopeGolden for the golden boot only.
type Credential struct {
	Hostname   string `json:"hostname"`
	Header     string `json:"header"`
	Prefix     string `json:"prefix,omitempty"`
	URI        string `json:"uri,omitempty"`
	Audience   string `json:"audience,omitempty"`
	PathPrefix string `json:"pathPrefix,omitempty"`
	Scope      string `json:"scope,omitempty"`
}

// ValidPathPrefix reports whether prefix names a subtree of request paths the
// gateway matches unambiguously: a path below the root starting and ending
// with "/", printable ASCII without a query, fragment, backslash or
// percent-encoding, and without an empty or dot segment.
func ValidPathPrefix(prefix string) bool {
	if len(prefix) < 3 || len(prefix) > 1024 || prefix[0] != '/' || prefix[len(prefix)-1] != '/' {
		return false
	}
	for _, r := range prefix {
		if r <= ' ' || r > '~' || strings.ContainsRune(`?#\%`, r) {
			return false
		}
	}
	for segment := range strings.SplitSeq(prefix[1:len(prefix)-1], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// ForActors returns the bindings of a session's actors: every binding without
// a scope. A scoped binding's destination stays allowed, without the header.
func ForActors(bindings []Credential) []Credential {
	return slices.DeleteFunc(slices.Clone(bindings), func(c Credential) bool { return c.Scope != "" })
}

// CanonicalCredentials validates and orders bindings. Substrate matches a
// hostname and a path prefix, so two credentials for the same host, header and
// path prefix cannot coexist.
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
		if c.PathPrefix != "" && !ValidPathPrefix(c.PathPrefix) {
			return nil, fmt.Errorf("invalid credential path prefix %q for %s", c.PathPrefix, c.Hostname)
		}
		if c.Scope != "" && c.Scope != ScopeGolden {
			return nil, fmt.Errorf("unknown credential scope %q", c.Scope)
		}
		if c.Audience != "" {
			if c.URI != "" || len(c.Audience) > 512 {
				return nil, fmt.Errorf("credential for %s header %s needs either a URI or an actor JWT audience of at most 512 characters", c.Hostname, c.Header)
			}
			continue
		}
		u, err := url.Parse(c.URI)
		if err != nil || u.Scheme != "ate-secret" || (u.Host != KubernetesSecretAuthority && u.Host != GoogleAccessTokenAuthority) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("invalid credential URI")
		}
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) != 4 || parts[0] != "default" || len(validation.IsDNS1123Label(parts[1])) != 0 || len(validation.IsDNS1123Subdomain(parts[2])) != 0 || parts[3] == "" || len(validation.IsConfigMapKey(parts[3])) != 0 {
			return nil, fmt.Errorf("credential URI must identify the default locator, namespace, Secret and key")
		}
	}
	slices.SortFunc(result, func(a, b Credential) int {
		return strings.Compare(a.Hostname+"\x00"+a.Header+"\x00"+a.PathPrefix, b.Hostname+"\x00"+b.Header+"\x00"+b.PathPrefix)
	})
	for i := 1; i < len(result); i++ {
		a, b := result[i-1], result[i]
		if a.Hostname == b.Hostname && a.Header == b.Header && a.PathPrefix == b.PathPrefix && a != b {
			return nil, fmt.Errorf("conflicting credentials for %s%s header %s", a.Hostname, a.PathPrefix, a.Header)
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
