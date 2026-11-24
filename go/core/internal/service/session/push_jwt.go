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
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/database"
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
	key              pushSigningKey
	activateAt       time.Time
	revision         int64
	conflictReported bool
}

type retiredPushKey struct {
	jwk      pushJWK
	removeAt time.Time
}

type pushSigningStore interface {
	IsPushSigningKeyActive(context.Context, string, string) (bool, error)
	LoadOrInitializePushSigningState(context.Context, string, string) (database.PushSigningState, error)
	ActivatePushSigningKey(context.Context, string, string, int64) (database.PushSigningState, error)
}

var _ pushSigningStore = (*database.Client)(nil)

// PushJWTSignerConfig identifies current and optional previous key material.
type PushJWTSignerConfig struct {
	PrivateKeyFile         string
	PreviousPrivateKeyFile string
	Issuer                 string
	ActivationDelay        time.Duration
}

// PushJWTSigner prepublishes replacements and persists activation before signing.
// Replicas retain their own staging delays; the store informs subsequent startups.
type PushJWTSigner struct {
	issuer          string
	path            string
	activationDelay time.Duration
	store           pushSigningStore
	retainPrevious  bool
	// Serialize file/DB updates separately so slow I/O never blocks signing/JWKS.
	updateMu sync.Mutex
	mu       sync.Mutex
	active   pushSigningKey
	pending  *pendingPushKey
	retired  map[string]retiredPushKey
	previous *pushJWK
}

var _ manager.LeaderElectionRunnable = (*PushJWTSigner)(nil)
var _ http.Handler = (*PushJWTSigner)(nil)

// NewPushJWTSigner reads an unencrypted PKCS#8 Ed25519 PEM key shared by replicas.
// An empty current path disables signing. An available distinct previous key
// preserves staging on startup unless current is the latest committed activation.
// An omitted or identical previous key preserves immediate single-key startup.
// A configured previous file may be missing only when current is already active.
func NewPushJWTSigner(ctx context.Context, store pushSigningStore, cfg PushJWTSignerConfig) (*PushJWTSigner, error) {
	if cfg.PrivateKeyFile == "" {
		if cfg.PreviousPrivateKeyFile != "" {
			return nil, fmt.Errorf("previous push signing key requires a current key")
		}
		return nil, nil
	}
	issuer := strings.TrimRight(cfg.Issuer, "/")
	if issuer == "" {
		return nil, fmt.Errorf("push JWT issuer is required")
	}
	if cfg.ActivationDelay <= 0 || cfg.ActivationDelay > pushKeyMaxActivationDelay {
		return nil, fmt.Errorf("push signing key activation delay must be positive and at most %s", pushKeyMaxActivationDelay)
	}
	key, err := readPushSigningKey(cfg.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	signer := &PushJWTSigner{
		issuer: issuer, path: cfg.PrivateKeyFile, activationDelay: cfg.ActivationDelay,
		store: store, active: key, retired: make(map[string]retiredPushKey),
		retainPrevious: cfg.PreviousPrivateKeyFile != "",
	}
	initial := key
	if signer.retainPrevious {
		previous, err := readPushSigningKey(cfg.PreviousPrivateKeyFile)
		if errors.Is(err, os.ErrNotExist) {
			active, lookupErr := store.IsPushSigningKeyActive(ctx, issuer, key.jwk.KeyID)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if !active {
				return nil, fmt.Errorf("previous push signing key is required until the current key is activated: %w", err)
			}
			logging.FromContext(ctx).WarnContext(ctx, "previous push signing key file is missing, publishing only the active key", "path", cfg.PreviousPrivateKeyFile)
			return signer, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to load previous push signing key: %w", err)
		}
		initial = previous
		signer.previous = new(previous.jwk)
	}
	state, err := store.LoadOrInitializePushSigningState(ctx, issuer, initial.jwk.KeyID)
	if err != nil {
		return nil, err
	}
	if state.KeyID != key.jwk.KeyID && initial.jwk.KeyID != key.jwk.KeyID {
		signer.active = initial
		signer.pending = &pendingPushKey{key: key, revision: state.Revision,
			activateAt: time.Now().Add(cfg.ActivationDelay + pushKeyPropagationBuffer)}
	}
	return signer, nil
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
// well as CSI replacements. Failed reloads keep the active signing key intact.
func (s *PushJWTSigner) Start(ctx context.Context) error {
	// Application initialization must not consume the startup publication delay.
	s.mu.Lock()
	if s.pending != nil {
		s.pending.activateAt = time.Now().Add(s.activationDelay + pushKeyPropagationBuffer)
	}
	s.mu.Unlock()
	timer := time.NewTimer(pushKeyPollInterval)
	defer timer.Stop()
	for {
		if err := s.reload(ctx); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to reload push signing key", "error", err)
		}
		if err := s.activateDue(ctx); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to activate push signing key", "error", err)
		}
		wait := pushKeyPollInterval
		s.mu.Lock()
		if s.pending != nil {
			if remaining := time.Until(s.pending.activateAt); remaining > 0 {
				wait = min(wait, remaining)
			}
		}
		s.mu.Unlock()
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
	}
}

func (s *PushJWTSigner) reload(ctx context.Context) error {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	key, err := readPushSigningKey(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	switch {
	case key.jwk.KeyID == s.active.jwk.KeyID:
		// Reverting to the active file cancels an unactivated replacement.
		s.pending = nil
		s.mu.Unlock()
		return nil
	case s.pending != nil && key.jwk.KeyID == s.pending.key.jwk.KeyID:
		// Identity, rather than PEM formatting or mtime, preserves the deadline.
		s.mu.Unlock()
		return nil
	}
	// This valid replacement supersedes the pending key even if staging it fails.
	s.pending = nil
	activeID := s.active.jwk.KeyID
	s.mu.Unlock()
	state, err := s.store.LoadOrInitializePushSigningState(ctx, s.issuer, activeID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.pending = &pendingPushKey{key: key, revision: state.Revision,
		activateAt: time.Now().Add(s.activationDelay + pushKeyPropagationBuffer)}
	s.mu.Unlock()
	return nil
}

// activateDue commits before installing a candidate. Only the background updater
// calls it; signing and JWKS requests never wait for persistence.
func (s *PushJWTSigner) activateDue(ctx context.Context) error {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	s.mu.Lock()
	pending := s.pending
	s.mu.Unlock()
	if pending == nil || time.Now().Before(pending.activateAt) {
		return nil
	}
	state, err := s.store.ActivatePushSigningKey(ctx, s.issuer, pending.key.jwk.KeyID, pending.revision)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.KeyID != pending.key.jwk.KeyID {
		// Keep retrying at the original revision so we can confirm a peer's
		// matching activation while protecting intervening handoffs.
		if pending.conflictReported {
			return nil
		}
		pending.conflictReported = true
		return fmt.Errorf("push signing key activation superseded at revision %d; waiting for a peer to activate the configured key", state.Revision)
	}
	now := time.Now()
	// Peers may still use the old key even when this replica was idle.
	expiration := now.Add(pushJWTLifetime)
	if s.active.lastExpiration.After(expiration) {
		expiration = s.active.lastExpiration
	}
	s.retired[s.active.jwk.KeyID] = retiredPushKey{jwk: s.active.jwk, removeAt: expiration.Add(pushKeyClockSkew)}
	if s.retainPrevious {
		// A hot reload advances the same current/previous pair a peer reads on restart.
		s.previous = new(s.active.jwk)
	}
	s.active = s.pending.key
	// A rollback may reactivate a key that still has valid earlier JWTs.
	if old, found := s.retired[s.active.jwk.KeyID]; found {
		s.active.lastExpiration = old.removeAt.Add(-pushKeyClockSkew)
	}
	delete(s.retired, s.active.jwk.KeyID)
	s.pending = nil
	return nil
}

// expireRetired runs under mu; the configured previous public key is pinned
// separately because peers can continue using it throughout their local delay.
func (s *PushJWTSigner) expireRetired(now time.Time) {
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
	s.expireRetired(now)
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
	s.expireRetired(time.Now())
	keys := make(map[string]pushJWK, len(s.retired)+3) // Active, pending, and configured previous.
	for id, key := range s.retired {
		keys[id] = key.jwk
	}
	if s.previous != nil {
		keys[s.previous.KeyID] = *s.previous
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
