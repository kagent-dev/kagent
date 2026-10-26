package egress

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/http/httpguts"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// Credential binds a provider reference to an exact destination and HTTP header.
// Values are fetched by the gateway and never enter a runtime revision.
type Credential struct {
	Origin     string      `json:"origin"`
	Header     string      `json:"header"`
	Prefix     string      `json:"prefix,omitempty"`
	URI        string      `json:"uri,omitempty"`
	ActorToken *ActorToken `json:"actorToken,omitempty"`
}

// ActorToken stores the resolved gateway token request without credential bytes.
type ActorToken struct {
	Audiences         []string `json:"audiences"`
	ExpirationSeconds int64    `json:"expirationSeconds"`
}

// Equal compares complete credential sources, including Actor token claims.
func (c Credential) Equal(other Credential) bool {
	if c.Origin != other.Origin || c.Header != other.Header || c.Prefix != other.Prefix || c.URI != other.URI {
		return false
	}
	if c.ActorToken == nil || other.ActorToken == nil {
		return c.ActorToken == other.ActorToken
	}
	return c.ActorToken.ExpirationSeconds == other.ActorToken.ExpirationSeconds && slices.Equal(c.ActorToken.Audiences, other.ActorToken.Audiences)
}

// CanonicalGatewayTargets validates, orders and deduplicates targets. One
// origin and header cannot carry two prefixes.
func CanonicalGatewayTargets(targets []egresspolicy.GatewayTarget) ([]egresspolicy.GatewayTarget, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	result := slices.Clone(targets)
	for i := range result {
		t := &result[i]
		u, err := ParseOrigin(t.Origin)
		if err != nil || strings.Contains(t.Origin, "*") {
			return nil, fmt.Errorf("gateway target requires an exact HTTP(S) DNS origin, got %q", t.Origin)
		}
		t.Origin = u.String()
		t.Header = strings.ToLower(t.Header)
		if !ValidCredentialHeader(t.Header) {
			return nil, fmt.Errorf("invalid gateway target header %q", t.Header)
		}
		if !httpguts.ValidHeaderFieldValue(t.Prefix) {
			return nil, fmt.Errorf("invalid gateway target prefix %q", t.Prefix)
		}
	}
	slices.SortFunc(result, func(a, b egresspolicy.GatewayTarget) int {
		return strings.Compare(a.Origin+"\x00"+a.Header+"\x00"+a.Prefix, b.Origin+"\x00"+b.Header+"\x00"+b.Prefix)
	})
	result = slices.Compact(result)
	for i := 1; i < len(result); i++ {
		if a, b := result[i-1], result[i]; a.Origin == b.Origin && a.Header == b.Header {
			return nil, fmt.Errorf("conflicting gateway targets for %s header %s", a.Origin, a.Header)
		}
	}
	return result, nil
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
		if !ValidCredentialHeader(c.Header) || !httpguts.ValidHeaderFieldValue(c.Prefix) {
			return nil, fmt.Errorf("invalid credential injection header %q", c.Header)
		}
		if (c.URI == "") == (c.ActorToken == nil) {
			return nil, fmt.Errorf("credential requires exactly one Secret or Actor token source")
		}
		if c.ActorToken != nil {
			c.ActorToken = &ActorToken{Audiences: slices.Clone(c.ActorToken.Audiences), ExpirationSeconds: c.ActorToken.ExpirationSeconds}
			if err := validateActorToken(c.ActorToken); err != nil {
				return nil, err
			}
			slices.Sort(c.ActorToken.Audiences)
		} else if err := validCredentialURI(c.URI); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(result, func(a, b Credential) int {
		return strings.Compare(a.Origin+"\x00"+a.Header, b.Origin+"\x00"+b.Header)
	})
	for i := 1; i < len(result); i++ {
		a, b := result[i-1], result[i]
		if a.Origin == b.Origin && a.Header == b.Header && !a.Equal(b) {
			return nil, fmt.Errorf("conflicting credentials for %s header %s", a.Origin, a.Header)
		}
	}
	result = slices.CompactFunc(result, Credential.Equal)
	counts := map[string]int{}
	for _, c := range result {
		counts[c.Origin]++
		if counts[c.Origin] > 16 {
			return nil, fmt.Errorf("credential injection supports at most 16 headers per origin")
		}
	}
	return result, nil
}

func validateActorToken(source *ActorToken) error {
	if len(source.Audiences) < 1 || len(source.Audiences) > 16 || source.ExpirationSeconds < 300 || source.ExpirationSeconds > 3600 {
		return fmt.Errorf("actor token requires 1 to 16 audiences and a lifetime of 300 to 3600 seconds")
	}
	seen := make(map[string]bool)
	for _, audience := range source.Audiences {
		if audience == "" || utf8.RuneCountInString(audience) > 512 || audience == "kagent-runtime" || seen[audience] {
			return fmt.Errorf("invalid, duplicate, or reserved Actor token audience")
		}
		seen[audience] = true
	}
	return nil
}

// validCredentialURI accepts ate-secret://<provider>/<path>. Provider names are
// lowercase DNS names without ports. k8s.io paths name one Secret key.
func validCredentialURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ate-secret" || u.Opaque != "" {
		return fmt.Errorf("invalid credential URI")
	}
	if u.User != nil || u.ForceQuery || u.RawQuery != "" || strings.Contains(raw, "#") {
		return fmt.Errorf("credential URI cannot carry user information, a query or a fragment")
	}
	if u.Host != u.Hostname() || len(validation.IsDNS1123Subdomain(u.Host)) != 0 {
		return fmt.Errorf("credential provider must be a lowercase DNS name without a port")
	}
	// Substrate requires at least two nonempty, unescaped path segments.
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if u.EscapedPath() != u.Path || !strings.HasPrefix(u.Path, "/") || len(parts) < 2 || slices.Contains(parts, "") {
		return fmt.Errorf("credential URI must have at least two nonempty path segments without percent encoding")
	}
	if u.Host != "k8s.io" {
		return nil
	}
	if len(parts) != 4 || parts[0] != "default" {
		return fmt.Errorf("credential URI must identify the default locator, namespace, Secret and key")
	}
	namespace, secret, key := parts[1], parts[2], parts[3]
	if len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(secret)) != 0 || len(validation.IsConfigMapKey(key)) != 0 {
		return fmt.Errorf("credential URI must identify the default locator, namespace, Secret and key")
	}
	return nil
}

// ValidCredentialHeader excludes routing, framing, hop-by-hop and cookie headers.
func ValidCredentialHeader(header string) bool {
	if len(header) > 256 || !httpguts.ValidHeaderFieldName(header) {
		return false
	}
	switch strings.ToLower(header) {
	case "host", "connection", "content-length", "transfer-encoding", "te", "trailer", "upgrade", "keep-alive", "proxy-authorization", "proxy-authenticate", "cookie":
		return false
	}
	return true
}
