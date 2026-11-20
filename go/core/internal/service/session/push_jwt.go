package session

import (
	"bytes"
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	// pushJWTLifetime sets callback token validity and the minimum public-key
	// retention after replacement activation, before adding clock skew.
	pushJWTLifetime = 5 * time.Minute
	// pushKeyPollInterval controls how often each replica reopens the key path
	// to detect Secret or CSI file replacements, including symlink swaps.
	pushKeyPollInterval = 15 * time.Second
	// pushKeyPropagationBuffer allows receiver configuration updates to propagate
	// after the configured JWKS prepublication delay, before the new key signs.
	pushKeyPropagationBuffer = 30 * time.Second
	// pushKeyMaxActivationDelay catches excessively long configured staging delays.
	pushKeyMaxActivationDelay = 24 * time.Hour
	// pushKeyClockSkew retains retired public keys beyond their last JWT
	// expiration to tolerate clock differences at receivers.
	pushKeyClockSkew = 30 * time.Second
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

type pushSigningKey struct {
	private        ed25519.PrivateKey
	jwk            pushJWK
	lastExpiration time.Time
}

type pendingPushKey struct {
	key        pushSigningKey
	activateAt time.Time
}

type retiredPushKey struct {
	jwk      pushJWK
	removeAt time.Time
}

// PushJWTSigner prepublishes replacement keys and retains previous public keys
// for outstanding credentials. Rotation history is process-local.
type PushJWTSigner struct {
	issuer          string
	path            string
	activationDelay time.Duration
	mu              sync.Mutex
	active          pushSigningKey
	pending         *pendingPushKey
	retired         map[string]retiredPushKey
}

var _ manager.LeaderElectionRunnable = (*PushJWTSigner)(nil)
var _ http.Handler = (*PushJWTSigner)(nil)

// NewPushJWTSigner reads an unencrypted PKCS#8 Ed25519 PEM key shared by replicas.
// An empty path disables signing. Startup uses the mounted key immediately;
// replacements wait activationDelay plus the propagation buffer.
func NewPushJWTSigner(path, issuer string, activationDelay time.Duration) (*PushJWTSigner, error) {
	if path == "" {
		return nil, nil
	}
	if issuer == "" {
		return nil, fmt.Errorf("push JWT issuer is required")
	}
	if activationDelay <= 0 || activationDelay > pushKeyMaxActivationDelay {
		return nil, fmt.Errorf("push signing key activation delay must be positive and at most %s", pushKeyMaxActivationDelay)
	}
	key, err := readPushSigningKey(path)
	if err != nil {
		return nil, err
	}
	return &PushJWTSigner{issuer: issuer, path: path, activationDelay: activationDelay,
		active: key, retired: make(map[string]retiredPushKey)}, nil
}

func readPushSigningKey(path string) (pushSigningKey, error) {
	material, err := os.ReadFile(path)
	if err != nil {
		return pushSigningKey{}, fmt.Errorf("failed to read push signing key: %w", err)
	}
	private, err := parsePushPrivateKey(material)
	if err != nil {
		return pushSigningKey{}, err
	}
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	return pushSigningKey{private: private, jwk: pushJWK{
		KeyType: "OKP", Curve: "Ed25519", Alg: "EdDSA", Use: "sig",
		KeyID: base64.RawURLEncoding.EncodeToString(digest[:]),
		X:     base64.RawURLEncoding.EncodeToString(public),
	}}, nil
}

// NeedLeaderElection keeps every signing and JWKS-serving replica up to date.
func (s *PushJWTSigner) NeedLeaderElection() bool { return false }

// Start reopens the configured path to handle projected-volume symlink swaps as
// well as CSI replacements. Failed reloads leave the last valid state intact.
func (s *PushJWTSigner) Start(ctx context.Context) error {
	ticker := time.NewTicker(pushKeyPollInterval)
	defer ticker.Stop()
	for {
		if err := s.reload(); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to reload push signing key", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *PushJWTSigner) reload() error {
	key, err := readPushSigningKey(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.advance(now)
	switch {
	case key.jwk.KeyID == s.active.jwk.KeyID:
		// Reverting to the active file cancels an unactivated replacement.
		s.pending = nil
	case s.pending != nil && key.jwk.KeyID == s.pending.key.jwk.KeyID:
		// Identity, rather than PEM formatting or mtime, preserves the deadline.
	default:
		s.pending = &pendingPushKey{key: key, activateAt: now.Add(s.activationDelay + pushKeyPropagationBuffer)}
	}
	return nil
}

// advance runs under mu so signing, activation, and retirement cannot interleave.
func (s *PushJWTSigner) advance(now time.Time) {
	if s.pending != nil && !now.Before(s.pending.activateAt) {
		// Peers may still use the old key even when this replica was idle.
		expiration := now.Add(pushJWTLifetime)
		if s.active.lastExpiration.After(expiration) {
			expiration = s.active.lastExpiration
		}
		s.retired[s.active.jwk.KeyID] = retiredPushKey{jwk: s.active.jwk, removeAt: expiration.Add(pushKeyClockSkew)}
		s.active = s.pending.key
		// A rollback may reactivate a key that still has valid earlier JWTs.
		if old, found := s.retired[s.active.jwk.KeyID]; found {
			s.active.lastExpiration = old.removeAt.Add(-pushKeyClockSkew)
		}
		delete(s.retired, s.active.jwk.KeyID)
		s.pending = nil
	}
	for id, key := range s.retired {
		if !now.Before(key.removeAt) {
			delete(s.retired, id)
		}
	}
}

func (s *PushJWTSigner) Sign(taskID, audience string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.advance(now)
	expiration := now.Add(pushJWTLifetime)
	claims := pushJWTClaims{
		TaskID: taskID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiration),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = s.active.jwk.KeyID
	credential, err := token.SignedString(s.active.private)
	if err == nil && expiration.After(s.active.lastExpiration) {
		s.active.lastExpiration = expiration
	}
	return credential, err
}

func (s *PushJWTSigner) keySet() pushJWKSet {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advance(time.Now())
	keys := make(map[string]pushJWK, len(s.retired)+2)
	for id, key := range s.retired {
		keys[id] = key.jwk
	}
	keys[s.active.jwk.KeyID] = s.active.jwk
	if s.pending != nil {
		keys[s.pending.key.jwk.KeyID] = s.pending.key.jwk
	}
	set := pushJWKSet{Keys: make([]pushJWK, 0, len(keys))}
	for _, key := range keys {
		set.Keys = append(set.Keys, key)
	}
	slices.SortFunc(set.Keys, func(a, b pushJWK) int { return cmp.Compare(a.KeyID, b.KeyID) })
	return set
}

// ServeHTTP exposes active, pending, and unexpired previous public keys only.
func (s *PushJWTSigner) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/jwk-set+json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	data, err := json.Marshal(s.keySet())
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
