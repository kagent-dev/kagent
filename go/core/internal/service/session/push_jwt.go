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
	"math/rand/v2"
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
	// pushJWTLifetime sets callback token validity.
	pushJWTLifetime = 5 * time.Minute
	// pushKeyPollInterval controls how often each replica reopens the key path
	// to detect Secret or CSI file replacements, including symlink swaps.
	pushKeyPollInterval = 15 * time.Second
	pushKeyPollJitter   = 3 * time.Second
	// A successful publication covers four poll intervals of signing, plus
	// the lifetime and allowed clock skew of the last JWT issued in that window.
	pushKeySigningWindow = 4 * pushKeyPollInterval
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

type pushJWKSet struct {
	Keys []database.PushJWK `json:"keys"`
}

type pushSigningKey struct {
	private ed25519.PrivateKey
	jwk     database.PushJWK
}

type pendingPushKey struct {
	key              pushSigningKey
	activateAt       time.Time
	revision         int64
	conflictReported bool
}

type pushSigningStore interface {
	IsPushSigningKeyActive(context.Context, string, string) (bool, error)
	ActivatePushSigningKey(context.Context, string, string, int64) (database.PushSigningState, error)
	PublishPushSigningKeys(context.Context, string, string, []database.PushJWK, time.Time) (database.PushSigningSnapshot, error)
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
	// updateMu serializes startup timer, file, and DB updates. mu protects
	// only the active key and shared snapshot read by signing and HTTP.
	updateMu  sync.Mutex
	pending   *pendingPushKey
	previous  *database.PushJWK
	mu        sync.RWMutex
	active    pushSigningKey
	published map[string]database.PublishedPushKey
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
		store: store, active: key,
		retainPrevious: cfg.PreviousPrivateKeyFile != "",
	}
	initial := key
	var missingPrevious error
	if signer.retainPrevious {
		previous, err := readPushSigningKey(cfg.PreviousPrivateKeyFile)
		switch {
		case errors.Is(err, os.ErrNotExist):
			missingPrevious = fmt.Errorf("previous push signing key is required until the current key is activated: %w", err)
			active, lookupErr := store.IsPushSigningKeyActive(ctx, issuer, key.jwk.KeyID)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if !active {
				return nil, missingPrevious
			}
		case err != nil:
			return nil, fmt.Errorf("failed to load previous push signing key: %w", err)
		default:
			initial = previous
			signer.previous = new(previous.jwk)
		}
	}
	state, err := signer.publish(ctx, initial.jwk.KeyID, nil)
	if err != nil {
		return nil, err
	}
	if missingPrevious != nil {
		if state.KeyID != key.jwk.KeyID {
			return nil, missingPrevious
		}
		logging.FromContext(ctx).WarnContext(ctx, "previous push signing key file is missing, using the activated current key", "path", cfg.PreviousPrivateKeyFile)
	}
	if state.KeyID != key.jwk.KeyID && initial.jwk.KeyID != key.jwk.KeyID {
		if state.KeyID != initial.jwk.KeyID {
			logging.FromContext(ctx).WarnContext(ctx, "recorded push signing key matches neither mounted key",
				"recorded_kid", state.KeyID, "current_kid", key.jwk.KeyID, "previous_kid", initial.jwk.KeyID)
		}
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
	return pushSigningKey{private: private, jwk: database.PushJWK{
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
	s.updateMu.Lock()
	if s.pending != nil {
		s.pending.activateAt = time.Now().Add(s.activationDelay + pushKeyPropagationBuffer)
	}
	// Construction already published. Spread the first refresh too, unless
	// initialization consumed the time left to issue another covered token.
	signUntil := s.published[s.active.jwk.KeyID].RetainUntil.Add(-pushJWTLifetime - pushKeyClockSkew)
	timer := time.NewTimer(min(s.nextPoll(), time.Until(signUntil)))
	s.updateMu.Unlock()
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if err := s.reload(ctx); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to reload push signing key", "error", err)
		}
		if err := s.activateDue(ctx); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to activate push signing key", "error", err)
		}
		s.updateMu.Lock()
		timer.Reset(s.nextPoll())
		s.updateMu.Unlock()
	}
}

// nextPoll runs under updateMu. A staged key wakes the poller at its deadline.
func (s *PushJWTSigner) nextPoll() time.Duration {
	wait := pushKeyPollInterval - pushKeyPollJitter + rand.N(2*pushKeyPollJitter)
	if s.pending != nil {
		if remaining := time.Until(s.pending.activateAt); remaining > 0 {
			return min(wait, remaining)
		}
	}
	return wait
}

func (s *PushJWTSigner) reload(ctx context.Context) error {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	key, fileErr := readPushSigningKey(s.path)
	candidate := s.pending
	if fileErr == nil {
		switch {
		case key.jwk.KeyID == s.active.jwk.KeyID:
			s.pending, candidate = nil, nil
		case candidate == nil || key.jwk.KeyID != candidate.key.jwk.KeyID:
			// A replacement cancels the old candidate even if publication fails.
			s.pending = nil
			candidate = &pendingPushKey{key: key}
		}
	}
	// Even an unchanged or temporarily unreadable file must refresh peer keys
	// and renew the keys this process can still use.
	var previouslyPublishedUntil time.Time
	if candidate != nil {
		previouslyPublishedUntil = s.published[candidate.key.jwk.KeyID].RetainUntil
	}
	state, err := s.publish(ctx, s.active.jwk.KeyID, candidate)
	if err != nil {
		return errors.Join(fileErr, err)
	}
	if candidate != nil {
		now := time.Now()
		if candidate != s.pending {
			candidate.revision = state.Revision
		}
		if candidate != s.pending || !now.Before(previouslyPublishedUntil) {
			candidate.activateAt = now.Add(s.activationDelay + pushKeyPropagationBuffer)
		}
		s.pending = candidate
	}
	return fileErr
}

// publish runs during construction or under updateMu. Only locally held keys
// renew coverage. Copying peer keys back would keep abandoned keys alive forever.
func (s *PushJWTSigner) publish(ctx context.Context, initialKeyID string, candidate *pendingPushKey) (database.PushSigningState, error) {
	keys := []database.PushJWK{s.active.jwk}
	if candidate != nil {
		keys = append(keys, candidate.key.jwk)
	}
	if s.previous != nil {
		keys = append(keys, *s.previous)
	}
	// A stalled write must release the poller so it can retry with a fresh window.
	signUntil := time.Now().Add(pushKeySigningWindow)
	ctx, cancel := context.WithDeadline(ctx, signUntil)
	defer cancel()
	until := signUntil.Add(pushJWTLifetime + pushKeyClockSkew)
	snapshot, err := s.store.PublishPushSigningKeys(ctx, s.issuer, initialKeyID, keys, until)
	if err != nil {
		return database.PushSigningState{}, err
	}
	// A delayed database response can arrive after its signing window closes.
	now := time.Now()
	for _, key := range keys {
		if !coversPushJWT(snapshot.PublishedKeys[key.KeyID], now) {
			return database.PushSigningState{}, fmt.Errorf("push key %q publication expired before confirmation", key.KeyID)
		}
	}
	s.mu.Lock()
	s.published = snapshot.PublishedKeys
	s.mu.Unlock()
	return snapshot.PushSigningState, nil
}

func coversPushJWT(key database.PublishedPushKey, issuedAt time.Time) bool {
	return !key.RetainUntil.Before(issuedAt.Add(pushJWTLifetime + pushKeyClockSkew))
}

// activateDue commits before installing a candidate. Only the background updater
// calls it; signing and JWKS requests never wait for persistence.
func (s *PushJWTSigner) activateDue(ctx context.Context) error {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	pending := s.pending
	now := time.Now()
	if pending == nil || now.Before(pending.activateAt) {
		return nil
	}
	// A database stall must not commit a staged activation after this
	// replica's confirmed window has closed. The next poll renews it.
	signUntil := s.published[pending.key.jwk.KeyID].RetainUntil.Add(-pushJWTLifetime - pushKeyClockSkew)
	if !now.Before(signUntil) {
		return nil
	}
	ctx, cancel := context.WithDeadline(ctx, signUntil)
	defer cancel()
	state, err := s.store.ActivatePushSigningKey(ctx, s.issuer, pending.key.jwk.KeyID, pending.revision)
	if err != nil {
		return err
	}
	if state.KeyID != pending.key.jwk.KeyID {
		// Keep retrying at the original revision so we can confirm a peer's
		// matching activation while protecting intervening handoffs.
		if pending.conflictReported {
			return nil
		}
		pending.conflictReported = true
		return fmt.Errorf("push signing key activation superseded at revision %d; waiting for a peer to activate the configured key", state.Revision)
	}
	if s.retainPrevious {
		// A hot reload advances the same current/previous pair a peer reads on restart.
		s.previous = new(s.active.jwk)
	}
	s.mu.Lock()
	s.active = pending.key
	s.mu.Unlock()
	s.pending = nil
	return nil
}

func (s *PushJWTSigner) Sign(taskID, audience string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	if !coversPushJWT(s.published[s.active.jwk.KeyID], now) {
		return "", errors.New("push signing key publication must be renewed before signing")
	}
	claims := pushJWTClaims{
		TaskID: taskID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(pushJWTLifetime)),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = s.active.jwk.KeyID
	return token.SignedString(s.active.private)
}

func (s *PushJWTSigner) keySet() pushJWKSet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := pushJWKSet{Keys: make([]database.PushJWK, 0, len(s.published))}
	for _, key := range s.published {
		set.Keys = append(set.Keys, key.JWK)
	}
	slices.SortFunc(set.Keys, func(a, b database.PushJWK) int { return cmp.Compare(a.KeyID, b.KeyID) })
	return set
}

// ServeHTTP serves the last successful issuer snapshot without a database read.
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
