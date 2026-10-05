package session

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

// NewPushJWTSigner accepts a base64-encoded Ed25519 seed shared by all replicas.
func NewPushJWTSigner(seed, issuer string) (*PushJWTSigner, error) {
	if issuer == "" {
		return nil, fmt.Errorf("push JWT issuer is required")
	}
	material, err := base64.StdEncoding.DecodeString(seed)
	if err != nil || len(material) != ed25519.SeedSize {
		return nil, fmt.Errorf("push JWT signing seed must be 32 base64-encoded bytes")
	}
	private := ed25519.NewKeyFromSeed(material)
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
