// Package grant issues the signed workspace grants a Session volume source is
// admitted with, for tests: a generated ES256 key, its JWKS, and grants for any
// subject, snapshot, changed repositories and providers. It stands in for the
// workspace issuer that holds the people's sign-ins.
package grant

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWKSPath is where Serve publishes the verification keys.
const JWKSPath = "/.well-known/jwks.json"

// Snapshot names the CSI snapshot a Session's clone is seeded from.
type Snapshot struct {
	Driver string `json:"driver"`
	Handle string `json:"handle"`
}

// Provider is a host whose credential a Session gets: the egress gateway sets
// it on requests to Hostname with Scheme, from a token the named broker
// exchanges for Audience. Username is set for the Basic scheme.
type Provider struct {
	Hostname string `json:"hostname"`
	Audience string `json:"audience"`
	Scheme   string `json:"scheme"`
	Username string `json:"username,omitempty"`
	Broker   string `json:"broker"`
}

// Claims is a grant's payload. Changed lists the repositories, relative to the
// working directory, whose origin moved after the snapshot.
type Claims struct {
	jwt.RegisteredClaims
	Snapshot  Snapshot   `json:"snapshot"`
	Changed   []string   `json:"changed,omitempty"`
	Providers []Provider `json:"providers,omitempty"`
}

// JWK is the public half of the signing key as RFC 7517 publishes it.
type JWK struct {
	KeyType   string `json:"kty"`
	Curve     string `json:"crv"`
	X         string `json:"x"`
	Y         string `json:"y"`
	KeyID     string `json:"kid"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
}

// JWKS is the document a verifier fetches.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// Signer signs grants for one issuer and audience with a key of its own.
type Signer struct {
	Issuer   string
	Audience string
	key      *ecdsa.PrivateKey
	keyID    string
	public   JWK
}

// NewSigner generates a P-256 signing key.
func NewSigner(issuer, audience string) (*Signer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate grant signing key: %w", err)
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return nil, fmt.Errorf("failed to generate grant key id: %w", err)
	}
	// The uncompressed point: 0x04, then X and Y of 32 bytes each.
	point, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to encode grant verification key: %w", err)
	}
	keyID := base64.RawURLEncoding.EncodeToString(id)
	return &Signer{Issuer: issuer, Audience: audience, key: key, keyID: keyID, public: JWK{
		KeyType: "EC", Curve: "P-256",
		X:     base64.RawURLEncoding.EncodeToString(point[1:33]),
		Y:     base64.RawURLEncoding.EncodeToString(point[33:]),
		KeyID: keyID, Use: "sig", Algorithm: jwt.SigningMethodES256.Alg(),
	}}, nil
}

// KeyID is the kid every grant of this signer carries.
func (s *Signer) KeyID() string { return s.keyID }

// SignGrant returns the compact JWT admitting subject to a Session in a clone
// of snapshot, valid from now for ttl; a negative ttl yields an expired grant.
func (s *Signer) SignGrant(subject string, snapshot Snapshot, changed []string, providers []Provider, ttl time.Duration) (string, error) {
	now := time.Now()
	notBefore := now
	if ttl < 0 {
		notBefore = now.Add(2 * ttl)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.Issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{s.Audience},
			IssuedAt:  jwt.NewNumericDate(notBefore),
			NotBefore: jwt.NewNumericDate(notBefore),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Snapshot:  snapshot,
		Changed:   changed,
		Providers: providers,
	})
	token.Header["kid"] = s.keyID
	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to sign grant for %s: %w", subject, err)
	}
	return signed, nil
}

// JWKS publishes the signer's public key.
func (s *Signer) JWKS() JWKS {
	return JWKS{Keys: []JWK{s.public}}
}

// Handler serves the JWKS at JWKSPath, for a test that listens where the
// cluster reaches it.
func (s *Signer) Handler() http.Handler {
	routes := http.NewServeMux()
	routes.HandleFunc("GET "+JWKSPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.JWKS())
	})
	return routes
}

// Serve publishes the JWKS on a loopback server for the test's lifetime and
// returns its URL.
func (s *Signer) Serve(t testing.TB) string {
	t.Helper()
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	return server.URL + JWKSPath
}
