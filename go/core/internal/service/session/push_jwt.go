package session

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type pushJWTClaims struct {
	TaskID string `json:"taskId"`
	jwt.RegisteredClaims
}

type pushJWK struct {
	KeyType string `json:"kty"`
	Curve   string `json:"crv"`
	Alg     string `json:"alg"`
	Use     string `json:"use"`
	KeyID   string `json:"kid"`
	X       string `json:"x"`
}

type pushJWKSet struct {
	Keys []pushJWK `json:"keys"`
}

// PushJWTSigner signs one short-lived credential per delivery attempt and
// publishes the corresponding public key for webhook receivers.
type PushJWTSigner struct {
	issuer  string
	keyID   string
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

// NewPushJWTSigner accepts an unencrypted PKCS#8 Ed25519 PEM key shared by all replicas.
// An empty key disables JWT signing.
func NewPushJWTSigner(privateKeyPEM, issuer string) (*PushJWTSigner, error) {
	if privateKeyPEM == "" {
		return nil, nil
	}
	if issuer == "" {
		return nil, fmt.Errorf("push JWT issuer is required")
	}
	private, err := parsePushPrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return nil, err
	}
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	return &PushJWTSigner{
		issuer:  issuer,
		keyID:   base64.RawURLEncoding.EncodeToString(digest[:]),
		private: private,
		public:  public,
	}, nil
}

func (s *PushJWTSigner) Sign(taskID, audience string) (string, error) {
	now := time.Now()
	claims := pushJWTClaims{
		TaskID: taskID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = s.keyID
	return token.SignedString(s.private)
}

// ServeHTTP exposes only the public verification key.
func (s *PushJWTSigner) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/jwk-set+json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	set := pushJWKSet{Keys: []pushJWK{{
		KeyType: "OKP", Curve: "Ed25519", Alg: "EdDSA", Use: "sig",
		KeyID: s.keyID, X: base64.RawURLEncoding.EncodeToString(s.public),
	}}}
	data, err := json.Marshal(set)
	if err != nil {
		logging.FromContext(r.Context()).ErrorContext(r.Context(), "failed to encode push JWKS", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(data); err != nil {
		logging.FromContext(r.Context()).ErrorContext(r.Context(), "failed to write push JWKS", "error", err)
	}
}

// parsePushPrivateKey accepts exactly one unencrypted PKCS#8 Ed25519 PEM block.
func parsePushPrivateKey(material []byte) (ed25519.PrivateKey, error) {
	material = bytes.TrimSpace(material)
	if !bytes.HasPrefix(material, []byte("-----BEGIN PRIVATE KEY-----")) || bytes.Count(material, []byte("-----BEGIN ")) != 1 {
		return nil, fmt.Errorf("push signing key must be an unencrypted PKCS#8 PEM private key")
	}
	block, rest := pem.Decode(material)
	if block == nil || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("push signing key must contain exactly one unencrypted PKCS#8 PEM private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse push signing private key: %w", err)
	}
	private, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("push signing private key must use Ed25519")
	}
	return private, nil
}
