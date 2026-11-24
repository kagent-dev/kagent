package session

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const testPushPrivateKeyPEM = "-----BEGIN PRIVATE KEY-----\nMC4CAQAwBQYDK2VwBCIEIHNzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nz\n-----END PRIVATE KEY-----\n"

const testPushIssuer = "https://kagent.example"

type fakePushSigningStore struct {
	mu             sync.Mutex
	state          database.PushSigningState
	err            error
	nextLoadErr    error
	beforeActivate func()
}

var _ pushSigningStore = (*fakePushSigningStore)(nil)

func (f *fakePushSigningStore) IsPushSigningKeyActive(_ context.Context, _, keyID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state.KeyID == keyID, f.err
}

func (f *fakePushSigningStore) LoadOrInitializePushSigningState(_ context.Context, _, keyID string) (database.PushSigningState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nextLoadErr != nil {
		err := f.nextLoadErr
		f.nextLoadErr = nil
		return database.PushSigningState{}, err
	}
	if f.state.KeyID == "" {
		f.state = database.PushSigningState{KeyID: keyID, Revision: 1}
	}
	return f.state, f.err
}

func (f *fakePushSigningStore) ActivatePushSigningKey(_ context.Context, _, keyID string, revision int64) (database.PushSigningState, error) {
	if f.beforeActivate != nil {
		f.beforeActivate()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return database.PushSigningState{}, f.err
	}
	if f.state.KeyID != keyID && f.state.Revision == revision {
		f.state = database.PushSigningState{KeyID: keyID, Revision: revision + 1}
	}
	return f.state, nil
}

func newPushTestSigner(t *testing.T, path, issuer string, delay time.Duration) (*PushJWTSigner, error) {
	t.Helper()
	return NewPushJWTSigner(t.Context(), &fakePushSigningStore{}, PushJWTSignerConfig{
		PrivateKeyFile: path, Issuer: issuer, ActivationDelay: delay,
	})
}

func TestPushJWTSigningAndJWKS(t *testing.T) {
	signer, err := newPushTestSigner(t, pushKeyFile(t, []byte(testPushPrivateKeyPEM)), "https://kagent.example", 5*time.Minute)
	require.NoError(t, err)
	credential, err := signer.Sign("task-1", "https://receiver.example/callback")
	require.NoError(t, err)
	retryCredential, err := signer.Sign("task-1", "https://receiver.example/callback")
	require.NoError(t, err)
	require.NotEqual(t, credential, retryCredential, "each attempt needs a fresh JWT ID")
	response := httptest.NewRecorder()
	signer.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	require.Equal(t, http.StatusOK, response.Code)
	restarted, err := newPushTestSigner(t, pushKeyFile(t, []byte(testPushPrivateKeyPEM)), "https://kagent.example", 5*time.Minute)
	require.NoError(t, err)
	restartedResponse := httptest.NewRecorder()
	restarted.ServeHTTP(restartedResponse, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	require.JSONEq(t, response.Body.String(), restartedResponse.Body.String(), "restarts must publish the same key and kid")

	var keys struct {
		Keys []struct {
			KTY string `json:"kty"`
			CRV string `json:"crv"`
			KID string `json:"kid"`
			X   string `json:"x"`
		} `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &keys))
	require.Len(t, keys.Keys, 1)
	require.Equal(t, "OKP", keys.Keys[0].KTY)
	require.Equal(t, "Ed25519", keys.Keys[0].CRV)
	public, err := base64.RawURLEncoding.DecodeString(keys.Keys[0].X)
	require.NoError(t, err)
	parsed, err := jwt.Parse(credential, func(token *jwt.Token) (any, error) {
		require.Equal(t, keys.Keys[0].KID, token.Header["kid"])
		return ed25519.PublicKey(public), nil
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer("https://kagent.example"), jwt.WithAudience("https://receiver.example/callback"))
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	require.Equal(t, "task-1", parsed.Claims.(jwt.MapClaims)["taskId"])
	_, err = jwt.Parse(credential, func(*jwt.Token) (any, error) { return ed25519.PublicKey(public), nil },
		jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithAudience("https://other.example/callback"))
	require.Error(t, err)
}

func TestPushJWTSigningKeyValidation(t *testing.T) {
	_, err := newPushTestSigner(t, pushKeyFile(t, []byte("bad")), "https://kagent.example", 5*time.Minute)
	require.ErrorContains(t, err, "PKCS#8 PEM private key")
	_, err = newPushTestSigner(t, pushKeyFile(t, []byte(testPushPrivateKeyPEM)), "", 5*time.Minute)
	require.ErrorContains(t, err, "issuer is required")
	signer, err := newPushTestSigner(t, "", "https://kagent.example", 5*time.Minute)
	require.NoError(t, err)
	require.Nil(t, signer)
	path := pushKeyFile(t, []byte(testPushPrivateKeyPEM))
	t.Run("Given a valid key, when the activation delay is 24h, assert that construction succeeds", func(t *testing.T) {
		signer, err := newPushTestSigner(t, path, "https://kagent.example", 24*time.Hour)
		require.NoError(t, err)
		require.NotNil(t, signer)
	})
	for _, delay := range []time.Duration{0, -time.Nanosecond, 24*time.Hour + time.Nanosecond, time.Duration(1<<63 - 1)} {
		t.Run("Given a valid key, when the activation delay is "+delay.String()+", assert that construction is rejected", func(t *testing.T) {
			_, err := newPushTestSigner(t, path, "https://kagent.example", delay)
			require.ErrorContains(t, err, "activation delay")
		})
	}
}

func pemKey(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestParsePushPrivateKey(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	valid := pemKey(t, key)
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	for _, test := range []struct {
		name     string
		material []byte
		valid    bool
	}{
		{"PKCS8 Ed25519", valid, true},
		{"whitespace", append([]byte(" \n"), append(valid, '\n')...), true},
		{"empty", nil, false},
		{"seed", []byte("c3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3M="), false},
		{"EC key", pemKey(t, ec), false},
		{"invalid DER", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("bad")}), false},
		{"public key", pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: key.Public().(ed25519.PublicKey)}), false},
		{"encrypted key", pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: key}), false},
		{"multiple blocks", append(append([]byte(nil), valid...), valid...), false},
		{"trailing data", append(append([]byte(nil), valid...), []byte("garbage")...), false},
		{"leading data", append([]byte("garbage\n"), valid...), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parsePushPrivateKey(test.material)
			if test.valid {
				require.NoError(t, err)
				require.Equal(t, key, parsed)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func pushKeyFile(t *testing.T, material []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private-key.pem")
	require.NoError(t, os.WriteFile(path, material, 0o600))
	return path
}

func pushTestJWKS(t *testing.T, signer *PushJWTSigner) map[string]ed25519.PublicKey {
	t.Helper()
	response := httptest.NewRecorder()
	signer.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "PRIVATE KEY")
	var set pushJWKSet
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &set))
	keys := make(map[string]ed25519.PublicKey)
	for _, key := range set.Keys {
		require.NotContains(t, keys, key.KeyID, "each kid must occur only once")
		public, err := base64.RawURLEncoding.DecodeString(key.X)
		require.NoError(t, err)
		keys[key.KeyID] = ed25519.PublicKey(public)
	}
	return keys
}

func pushTestToken(t *testing.T, signer *PushJWTSigner) (string, string, time.Time) {
	t.Helper()
	credential, err := signer.Sign("task", "https://receiver.example/callback")
	require.NoError(t, err)
	keys := pushTestJWKS(t, signer)
	token, err := jwt.Parse(credential, func(token *jwt.Token) (any, error) {
		return keys[token.Header["kid"].(string)], nil
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer("https://kagent.example"), jwt.WithAudience("https://receiver.example/callback"))
	require.NoError(t, err)
	expires, err := token.Claims.GetExpirationTime()
	require.NoError(t, err)
	issued, err := token.Claims.GetIssuedAt()
	require.NoError(t, err)
	require.Equal(t, pushJWTLifetime, expires.Sub(issued.Time))
	return credential, token.Header["kid"].(string), expires.Time
}

func TestPushSigningKeyRotation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given A is active, when B is published, assert that A signs throughout staging.
		signer := testPushSigner(t)
		_, oldID, _ := pushTestToken(t, signer)
		_, replacement, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(signer.path, pemKey(t, replacement), 0o600))
		require.NoError(t, signer.reload(t.Context()))
		keys := pushTestJWKS(t, signer)
		require.Len(t, keys, 2)
		time.Sleep(5*time.Minute + pushKeyPropagationBuffer - time.Second)
		require.NoError(t, signer.reload(t.Context()), "unchanged file must preserve activation time")
		_, beforeID, lastExpiration := pushTestToken(t, signer)
		require.Equal(t, oldID, beforeID)
		time.Sleep(time.Second)
		require.NoError(t, signer.activateDue(t.Context()))
		_, newID, _ := pushTestToken(t, signer)
		require.NotEqual(t, oldID, newID)
		require.Equal(t, replacement.Public(), keys[newID])
		removeAt := time.Now().Add(pushJWTLifetime + pushKeyClockSkew)

		// The last local token's expiry cannot shorten retention for peer tokens.
		time.Sleep(time.Until(lastExpiration.Add(pushKeyClockSkew)))
		require.Contains(t, pushTestJWKS(t, signer), oldID)
		time.Sleep(time.Until(removeAt) - time.Nanosecond)
		require.Contains(t, pushTestJWKS(t, signer), oldID)
		time.Sleep(time.Nanosecond)
		require.NotContains(t, pushTestJWKS(t, signer), oldID)
		require.Contains(t, pushTestJWKS(t, signer), newID)
	})
}

func TestPushIdleReplicaRetainsPeerSigningKey(t *testing.T) {
	scenarios := []struct {
		name             string
		signBeforeReload bool
	}{
		{name: "Given a replica that never signed, when it activates a replacement, assert that peer tokens remain verifiable"},
		{name: "Given a replica whose local tokens expired, when it activates a replacement, assert that peer tokens remain verifiable", signBeforeReload: true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				signer := testPushSigner(t)
				peer, err := newPushTestSigner(t, signer.path, signer.issuer, signer.activationDelay)
				require.NoError(t, err)
				if scenario.signBeforeReload {
					pushTestToken(t, signer)
				}
				_, replacement, err := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(signer.path, pemKey(t, replacement), 0o600))
				require.NoError(t, signer.reload(t.Context()))
				time.Sleep(signer.activationDelay + pushKeyPropagationBuffer)
				require.NoError(t, signer.activateDue(t.Context()))
				_, newID, _ := pushTestToken(t, signer)
				// The peer has not reloaded yet and still signs with the old key.
				credential, oldID, expiration := pushTestToken(t, peer)
				require.NotEqual(t, oldID, newID)
				keys := pushTestJWKS(t, signer)
				require.Contains(t, keys, oldID, "idle replicas must publish keys needed by peer tokens")
				parsed, err := jwt.Parse(credential, func(token *jwt.Token) (any, error) {
					return keys[token.Header["kid"].(string)], nil
				}, jwt.WithValidMethods([]string{"EdDSA"}))
				require.NoError(t, err)
				require.True(t, parsed.Valid)
				time.Sleep(time.Until(expiration.Add(pushKeyClockSkew)) - time.Nanosecond)
				require.Contains(t, pushTestJWKS(t, signer), oldID)
				time.Sleep(time.Nanosecond)
				keys = pushTestJWKS(t, signer)
				require.NotContains(t, keys, oldID)
				require.Contains(t, keys, newID)
			})
		})
	}
}

func TestPushKeyReloadPreservesValidState(t *testing.T) {
	for _, material := range []string{"", "malformed"} {
		t.Run("Given an active key, when a file update is invalid, assert that signing continues", func(t *testing.T) {
			signer := testPushSigner(t)
			_, oldID, _ := pushTestToken(t, signer)
			require.NoError(t, os.WriteFile(signer.path, []byte(material), 0o600))
			require.Error(t, signer.reload(t.Context()))
			_, id, _ := pushTestToken(t, signer)
			require.Equal(t, oldID, id)
			require.NoError(t, os.Remove(signer.path))
			require.Error(t, signer.reload(t.Context()))
			_, id, _ = pushTestToken(t, signer)
			require.Equal(t, oldID, id)
		})
	}
}

func TestPushKeyReloadReplacesPendingAndHandlesRollback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		signer := testPushSigner(t)
		_, originalID, _ := pushTestToken(t, signer)
		original, err := os.ReadFile(signer.path)
		require.NoError(t, err)
		var pendingID string
		for range 2 {
			_, next, err := ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(signer.path, pemKey(t, next), 0o600))
			require.NoError(t, signer.reload(t.Context()))
			keys := pushTestJWKS(t, signer)
			require.Len(t, keys, 2, "new file replaces the pending key")
			if pendingID != "" {
				require.NotContains(t, keys, pendingID)
			}
			for id := range keys {
				if id != originalID {
					pendingID = id
				}
			}
		}
		require.NoError(t, os.WriteFile(signer.path, original, 0o600))
		require.NoError(t, signer.reload(t.Context()))
		time.Sleep(6 * time.Minute)
		_, id, _ := pushTestToken(t, signer)
		require.Equal(t, originalID, id, "rollback to active key cancels pending activation")
		require.Len(t, pushTestJWKS(t, signer), 1)
	})
}

func TestPushReplacementCancelsPendingKeyWhenStateReadFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given A is signing and B is pending, when the reloader reads C and its
		// state lookup fails at B's deadline, assert that A signs until C is ready.
		store := &fakePushSigningStore{}
		cfg := pushHandoffConfig(t)
		signer := pushHandoffSigner(t, store, cfg)
		stop := startPushTestReloader(t, signer)
		defer stop()
		_, oldID, _ := pushTestToken(t, signer)
		require.Len(t, pushTestJWKS(t, signer), 2)
		delay := cfg.ActivationDelay + pushKeyPropagationBuffer
		time.Sleep(delay - time.Second)
		_, replacement, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		store.mu.Lock()
		store.nextLoadErr = errors.New("temporary signing state lookup failure")
		store.mu.Unlock()
		require.NoError(t, os.WriteFile(cfg.PrivateKeyFile, pemKey(t, replacement), 0o600))
		time.Sleep(time.Second)
		synctest.Wait()
		_, id, _ := pushTestToken(t, signer)
		require.Equal(t, oldID, id, "a failed lookup must not allow the replaced key to activate")
		state, err := store.LoadOrInitializePushSigningState(t.Context(), testPushIssuer, oldID)
		require.NoError(t, err)
		require.Equal(t, database.PushSigningState{KeyID: oldID, Revision: 1}, state)
		time.Sleep(pushKeyPollInterval)
		synctest.Wait()
		keys := pushTestJWKS(t, signer)
		require.Len(t, keys, 2)
		require.Contains(t, keys, oldID)
		time.Sleep(delay - time.Second)
		_, id, _ = pushTestToken(t, signer)
		require.Equal(t, oldID, id, "C must have its full publication delay")
		time.Sleep(time.Second)
		synctest.Wait()
		_, id, _ = pushTestToken(t, signer)
		require.NotEqual(t, oldID, id)
		require.Equal(t, replacement.Public(), keys[id])
	})
}

func TestPushKeyPollingFollowsFileReplacement(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run("Given a running reloader, when the key path is replaced, assert that JWKS updates without restart", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				signer := testPushSigner(t)
				old := pushTestJWKS(t, signer)
				if symlink {
					target := signer.path + ".original"
					require.NoError(t, os.Rename(signer.path, target))
					require.NoError(t, os.Symlink(target, signer.path))
				}
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)
				go func() { done <- signer.Start(ctx) }()
				synctest.Wait()
				_, next, err := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)
				replacement := signer.path + ".replacement"
				if symlink {
					target := signer.path + ".new"
					require.NoError(t, os.WriteFile(target, pemKey(t, next), 0o600))
					require.NoError(t, os.Symlink(target, replacement))
				} else {
					require.NoError(t, os.WriteFile(replacement, pemKey(t, next), 0o600))
				}
				require.NoError(t, os.Rename(replacement, signer.path))
				time.Sleep(pushKeyPollInterval)
				synctest.Wait()
				keys := pushTestJWKS(t, signer)
				require.Len(t, keys, 2)
				for id := range old {
					require.Contains(t, keys, id)
				}
				require.False(t, signer.NeedLeaderElection())
				cancel()
				require.NoError(t, <-done)
			})
		})
	}
}

func TestPushKeyConcurrentReloadAndSigning(t *testing.T) {
	signer := testPushSigner(t)
	_, next, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(signer.path, pemKey(t, next), 0o600))
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 30 {
				require.NoError(t, signer.reload(t.Context()))
				pushTestToken(t, signer)
			}
		})
	}
	workers.Wait()
}

func TestPushRestartUsesMountedKeyImmediately(t *testing.T) {
	signer := testPushSigner(t)
	_, originalID, _ := pushTestToken(t, signer)
	_, replacement, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(signer.path, pemKey(t, replacement), 0o600))
	require.NoError(t, signer.reload(t.Context()))
	restarted, err := newPushTestSigner(t, signer.path, "https://kagent.example", 5*time.Minute)
	require.NoError(t, err)
	_, id, _ := pushTestToken(t, restarted)
	require.NotEqual(t, originalID, id)
	require.Len(t, pushTestJWKS(t, restarted), 1)
}

func TestPushRollbackRetainsEarlierTokens(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := pushKeyFile(t, []byte(testPushPrivateKeyPEM))
		signer, err := newPushTestSigner(t, path, "https://kagent.example", time.Second)
		require.NoError(t, err)
		_, originalID, expiration := pushTestToken(t, signer)
		stage := func(material []byte) {
			require.NoError(t, os.WriteFile(path, material, 0o600))
			require.NoError(t, signer.reload(t.Context()))
			time.Sleep(time.Second + pushKeyPropagationBuffer)
			require.NoError(t, signer.activateDue(t.Context())) // activation without issuing another token
		}
		_, replacement, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		stage(pemKey(t, replacement))
		stage([]byte(testPushPrivateKeyPEM))
		stage(pemKey(t, replacement))
		removeAt := time.Now().Add(pushJWTLifetime + pushKeyClockSkew)
		require.Contains(t, pushTestJWKS(t, signer), originalID)
		time.Sleep(time.Until(expiration.Add(pushKeyClockSkew)))
		require.Contains(t, pushTestJWKS(t, signer), originalID, "rollback must preserve a full overlap after each activation")
		time.Sleep(time.Until(removeAt) - time.Nanosecond)
		require.Contains(t, pushTestJWKS(t, signer), originalID)
		time.Sleep(time.Nanosecond)
		require.NotContains(t, pushTestJWKS(t, signer), originalID)
	})
}

func TestPushHandoffAcrossReplicasAndRestarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given B/current and A/previous, when replicas start at different times,
		// assert that each keeps its delay but later startups recover committed B.
		store := &fakePushSigningStore{}
		cfg := pushHandoffConfig(t)
		first := pushHandoffSigner(t, store, cfg)
		stopFirst := startPushTestReloader(t, first)
		defer stopFirst()
		_, oldID, _ := pushTestToken(t, first)
		time.Sleep(time.Minute)
		second := pushHandoffSigner(t, store, cfg)
		stopSecond := startPushTestReloader(t, second)
		defer stopSecond()
		time.Sleep(cfg.ActivationDelay + pushKeyPropagationBuffer - time.Minute)
		synctest.Wait()
		_, newID, _ := pushTestToken(t, first)
		require.NotEqual(t, oldID, newID)
		_, secondID, _ := pushTestToken(t, second)
		require.Equal(t, oldID, secondID, "a peer activation must not shorten local staging")
		activated, err := store.LoadOrInitializePushSigningState(t.Context(), testPushIssuer, oldID)
		require.NoError(t, err)
		// An old Pod can restart with A alone while the rollout is in progress.
		oldConfig := cfg
		oldConfig.PrivateKeyFile, oldConfig.PreviousPrivateKeyFile = cfg.PreviousPrivateKeyFile, ""
		_, fallbackID, _ := pushTestToken(t, pushHandoffSigner(t, store, oldConfig))
		require.Equal(t, oldID, fallbackID)
		restarted := pushHandoffSigner(t, store, cfg)
		_, restartedID, _ := pushTestToken(t, restarted)
		require.Equal(t, newID, restartedID)
		require.Contains(t, pushTestJWKS(t, restarted), oldID)
		time.Sleep(time.Minute)
		synctest.Wait()
		_, secondID, _ = pushTestToken(t, second)
		require.Equal(t, newID, secondID)
		confirmed, err := store.LoadOrInitializePushSigningState(t.Context(), testPushIssuer, oldID)
		require.NoError(t, err)
		require.Equal(t, activated, confirmed, "the old Pod and duplicate confirmation must leave the activation intact")
		time.Sleep(2 * pushJWTLifetime)
		require.Contains(t, pushTestJWKS(t, first), oldID, "configured previous remains published for lagging peers")
		require.Contains(t, pushTestJWKS(t, restarted), oldID)
	})
}

func TestPushHotReloadRetainsPreviousForRestartedPeer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := pushHandoffConfig(t)
		store := &fakePushSigningStore{}
		running := pushHandoffSigner(t, store, cfg)
		delay := cfg.ActivationDelay + pushKeyPropagationBuffer
		time.Sleep(delay)
		require.NoError(t, running.activateDue(t.Context()))
		_, a, _ := pushTestToken(t, running)

		rotate := func() {
			t.Helper()
			current, err := os.ReadFile(cfg.PrivateKeyFile)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(cfg.PreviousPrivateKeyFile, current, 0o600))
			_, next, err := ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(cfg.PrivateKeyFile, pemKey(t, next), 0o600))
			require.NoError(t, running.reload(t.Context()))
		}
		// Stage B in place, then restart a peer halfway through the handoff.
		rotate()
		time.Sleep(delay / 2)
		peer := pushHandoffSigner(t, store, cfg)
		time.Sleep(delay / 2)
		require.NoError(t, running.activateDue(t.Context()))
		_, b, _ := pushTestToken(t, running)
		require.NotEqual(t, a, b)
		time.Sleep(time.Minute)
		require.NoError(t, peer.activateDue(t.Context()))
		token, peerID, expiry := pushTestToken(t, peer)
		require.Equal(t, a, peerID)

		// Verify the peer's still-valid token against the running replica after
		// its ordinary retirement deadline has passed.
		time.Sleep(time.Until(expiry) - time.Second)
		require.NoError(t, peer.activateDue(t.Context()))
		keys := pushTestJWKS(t, running)
		require.Contains(t, keys, a)
		_, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return keys[a], nil },
			jwt.WithValidMethods([]string{"EdDSA"}))
		require.NoError(t, err)
		time.Sleep(24 * time.Hour)
		keys = pushTestJWKS(t, running)
		require.Contains(t, keys, a)
		require.Equal(t, pushTestJWKS(t, peer), keys)

		// The next handoff replaces A with B as the previous public key.
		rotate()
		time.Sleep(delay)
		require.NoError(t, running.activateDue(t.Context()))
		time.Sleep(pushJWTLifetime + pushKeyClockSkew)
		keys = pushTestJWKS(t, running)
		require.NotContains(t, keys, a)
		require.Contains(t, keys, b)
		require.Equal(t, keys, pushTestJWKS(t, pushHandoffSigner(t, store, cfg)))
	})
}

func TestPushRestartBeforeActivationRestartsDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given an uncommitted B, when restarting, assert that the full delay starts
		// when the runnable begins, even if construction happened much earlier.
		store := &fakePushSigningStore{}
		cfg := pushHandoffConfig(t)
		first := pushHandoffSigner(t, store, cfg)
		_, oldID, _ := pushTestToken(t, first)
		stop := startPushTestReloader(t, first)
		time.Sleep(cfg.ActivationDelay / 2)
		stop()
		restarted := pushHandoffSigner(t, store, cfg)
		time.Sleep(cfg.ActivationDelay * 2)
		stop = startPushTestReloader(t, restarted)
		defer stop()
		time.Sleep(cfg.ActivationDelay + pushKeyPropagationBuffer - time.Second)
		synctest.Wait()
		_, id, _ := pushTestToken(t, restarted)
		require.Equal(t, oldID, id)
		time.Sleep(time.Second)
		synctest.Wait()
		_, id, _ = pushTestToken(t, restarted)
		require.NotEqual(t, oldID, id)
	})
}

func TestPushPreviousKeyFallback(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		previous func(*testing.T, string) string
	}{
		{"omitted", func(*testing.T, string) string { return "" }},
		{"same file", func(_ *testing.T, current string) string { return current }},
		{"same key in another file", func(t *testing.T, current string) string {
			material, err := os.ReadFile(current)
			require.NoError(t, err)
			return pushKeyFile(t, append(material, '\n'))
		}},
	} {
		t.Run("Given previous is "+scenario.name+", when starting, assert that current signs immediately and preserves the record", func(t *testing.T) {
			cfg := pushHandoffConfig(t)
			cfg.PreviousPrivateKeyFile = scenario.previous(t, cfg.PrivateKeyFile)
			key, err := readPushSigningKey(cfg.PrivateKeyFile)
			require.NoError(t, err)
			initial := database.PushSigningState{KeyID: "recorded-key", Revision: 7}
			store := &fakePushSigningStore{state: initial}
			signer := pushHandoffSigner(t, store, cfg)
			_, id, _ := pushTestToken(t, signer)
			require.Equal(t, key.jwk.KeyID, id)
			state, err := store.LoadOrInitializePushSigningState(t.Context(), testPushIssuer, "ignored")
			require.NoError(t, err)
			require.Equal(t, initial, state)
			require.Len(t, pushTestJWKS(t, signer), 1)
		})
	}
	t.Run("Given current is activated, when starting with the previous file missing, assert that current signs and a warning identifies the file", func(t *testing.T) {
		cfg := pushHandoffConfig(t)
		require.NoError(t, os.Remove(cfg.PreviousPrivateKeyFile))
		current, err := readPushSigningKey(cfg.PrivateKeyFile)
		require.NoError(t, err)
		initial := database.PushSigningState{KeyID: current.jwk.KeyID, Revision: 2}
		store := &fakePushSigningStore{state: initial}
		var logs bytes.Buffer
		ctx := logging.IntoContext(t.Context(), slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))

		signer, err := NewPushJWTSigner(ctx, store, cfg)
		require.NoError(t, err)
		_, id, _ := pushTestToken(t, signer)
		require.Equal(t, current.jwk.KeyID, id)
		require.Len(t, pushTestJWKS(t, signer), 1)
		require.Contains(t, logs.String(), cfg.PreviousPrivateKeyFile)
		require.Equal(t, initial, store.state)
	})
	for _, scenario := range []struct {
		name  string
		state database.PushSigningState
	}{
		{"another key is active", database.PushSigningState{KeyID: "recorded-key", Revision: 7}},
		{"no activation is recorded", database.PushSigningState{}},
	} {
		t.Run("Given "+scenario.name+", when starting with the previous file missing, assert that startup fails and preserves the record", func(t *testing.T) {
			cfg := pushHandoffConfig(t)
			require.NoError(t, os.Remove(cfg.PreviousPrivateKeyFile))
			store := &fakePushSigningStore{state: scenario.state}

			_, err := NewPushJWTSigner(t.Context(), store, cfg)
			require.ErrorIs(t, err, os.ErrNotExist)
			require.ErrorContains(t, err, cfg.PreviousPrivateKeyFile)
			require.Equal(t, scenario.state, store.state)
		})
	}
	t.Run("Given malformed previous material, when starting, assert that configuration fails", func(t *testing.T) {
		cfg := pushHandoffConfig(t)
		cfg.PreviousPrivateKeyFile = pushKeyFile(t, []byte("malformed"))
		_, err := NewPushJWTSigner(t.Context(), &fakePushSigningStore{}, cfg)
		require.ErrorContains(t, err, "previous push signing key")
	})
	t.Run("Given unreadable previous material, when starting, assert that configuration fails", func(t *testing.T) {
		cfg := pushHandoffConfig(t)
		cfg.PreviousPrivateKeyFile = t.TempDir() // EISDIR must not become the ENOENT fallback.
		_, err := NewPushJWTSigner(t.Context(), &fakePushSigningStore{}, cfg)
		require.ErrorContains(t, err, "previous push signing key")
	})
	t.Run("Given unavailable activation state, when starting, assert that recovery fails", func(t *testing.T) {
		_, err := NewPushJWTSigner(t.Context(), &fakePushSigningStore{err: errors.New("database unavailable")}, pushHandoffConfig(t))
		require.ErrorContains(t, err, "database unavailable")
	})
	t.Run("Given a missing previous file, when the activation lookup fails, assert that startup reports the database error", func(t *testing.T) {
		cfg := pushHandoffConfig(t)
		require.NoError(t, os.Remove(cfg.PreviousPrivateKeyFile))
		store := &fakePushSigningStore{err: errors.New("activation lookup failed")}
		_, err := NewPushJWTSigner(t.Context(), store, cfg)
		require.ErrorIs(t, err, store.err)
	})
}

func TestPushPreviousFileIsOnlyReadAtStartup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given startup A/B, when only previous changes or disappears, assert that
		// signing and JWKS retain the original material through activation.
		cfg := pushHandoffConfig(t)
		signer := pushHandoffSigner(t, &fakePushSigningStore{}, cfg)
		before := pushTestJWKS(t, signer)
		_, oldID, _ := pushTestToken(t, signer)
		require.NoError(t, os.WriteFile(cfg.PreviousPrivateKeyFile, []byte("invalid update"), 0o600))
		require.NoError(t, signer.reload(t.Context()))
		require.Equal(t, before, pushTestJWKS(t, signer))
		_, id, _ := pushTestToken(t, signer)
		require.Equal(t, oldID, id)
		require.NoError(t, os.Remove(cfg.PreviousPrivateKeyFile))
		time.Sleep(cfg.ActivationDelay + pushKeyPropagationBuffer)
		require.NoError(t, signer.reload(t.Context()))
		require.NoError(t, signer.activateDue(t.Context()))
		_, id, _ = pushTestToken(t, signer)
		require.NotEqual(t, oldID, id)
		require.Equal(t, before, pushTestJWKS(t, signer))
	})
}

func TestPushActivationFailureKeepsServing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given a due handoff, when persistence stalls and fails, assert that old
		// signatures and both public keys remain available, then retry succeeds.
		store := &fakePushSigningStore{}
		cfg := pushHandoffConfig(t)
		signer := pushHandoffSigner(t, store, cfg)
		_, oldID, _ := pushTestToken(t, signer)
		time.Sleep(cfg.ActivationDelay + pushKeyPropagationBuffer)
		started, release := make(chan struct{}), make(chan struct{})
		store.err = errors.New("database unavailable")
		store.beforeActivate = func() { close(started); <-release }
		done := make(chan error, 1)
		go func() { done <- signer.activateDue(t.Context()) }()
		<-started
		_, id, _ := pushTestToken(t, signer)
		require.Equal(t, oldID, id)
		require.Len(t, pushTestJWKS(t, signer), 2)
		close(release)
		require.ErrorContains(t, <-done, "database unavailable")
		_, id, _ = pushTestToken(t, signer)
		require.Equal(t, oldID, id)
		store.err, store.beforeActivate = nil, nil
		require.NoError(t, signer.activateDue(t.Context()))
		// No B token has been issued yet; reconstruction still recovers the commit.
		restarted := pushHandoffSigner(t, store, cfg)
		_, id, _ = pushTestToken(t, restarted)
		require.NotEqual(t, oldID, id)
	})
}

func TestPushStaleActivationWaitsForPeer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given B was pending before A -> C -> A, when B's delay expires,
		// assert that it cannot undo the rollback but can adopt a peer's later B.
		store := &fakePushSigningStore{}
		cfg := pushHandoffConfig(t)
		signer := pushHandoffSigner(t, store, cfg)
		_, oldID, _ := pushTestToken(t, signer)
		state, err := store.ActivatePushSigningKey(t.Context(), testPushIssuer, "key-c", 1)
		require.NoError(t, err)
		state, err = store.ActivatePushSigningKey(t.Context(), testPushIssuer, oldID, state.Revision)
		require.NoError(t, err)
		time.Sleep(cfg.ActivationDelay + pushKeyPropagationBuffer)
		require.ErrorContains(t, signer.activateDue(t.Context()), "superseded")
		require.NoError(t, signer.reload(t.Context()))
		require.NoError(t, signer.activateDue(t.Context()))
		_, id, _ := pushTestToken(t, signer)
		require.Equal(t, oldID, id)
		unchanged, err := store.LoadOrInitializePushSigningState(t.Context(), testPushIssuer, "ignored")
		require.NoError(t, err)
		require.Equal(t, state, unchanged)
		peer := pushHandoffSigner(t, store, cfg)
		time.Sleep(cfg.ActivationDelay + pushKeyPropagationBuffer)
		require.NoError(t, peer.activateDue(t.Context()))
		_, newID, _ := pushTestToken(t, peer)
		require.NotEqual(t, oldID, newID)
		require.NoError(t, signer.activateDue(t.Context()))
		_, id, _ = pushTestToken(t, signer)
		require.Equal(t, newID, id, "the earlier conflict must not prevent adopting committed B")
		confirmed, err := store.LoadOrInitializePushSigningState(t.Context(), testPushIssuer, oldID)
		require.NoError(t, err)
		require.Equal(t, database.PushSigningState{KeyID: newID, Revision: state.Revision + 1}, confirmed)
	})
}

func TestPushRollbackStagesPreviouslyUsedKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given A -> B completed, when rolling back with A/current and B/previous,
		// assert that A stages again and subsequent restarts use committed A.
		store := &fakePushSigningStore{}
		cfg := pushHandoffConfig(t)
		signer := pushHandoffSigner(t, store, cfg)
		_, oldID, _ := pushTestToken(t, signer)
		time.Sleep(cfg.ActivationDelay + pushKeyPropagationBuffer)
		require.NoError(t, signer.activateDue(t.Context()))
		_, newID, _ := pushTestToken(t, signer)
		cfg.PrivateKeyFile, cfg.PreviousPrivateKeyFile = cfg.PreviousPrivateKeyFile, cfg.PrivateKeyFile
		rollback := pushHandoffSigner(t, store, cfg)
		_, id, _ := pushTestToken(t, rollback)
		require.Equal(t, newID, id)
		time.Sleep(cfg.ActivationDelay + pushKeyPropagationBuffer)
		require.NoError(t, rollback.activateDue(t.Context()))
		_, id, _ = pushTestToken(t, pushHandoffSigner(t, store, cfg))
		require.Equal(t, oldID, id)
	})
}

func TestPushStartupWithActivationOutsideConfiguredPair(t *testing.T) {
	// Given an activation outside the configured pair, when starting, assert
	// that the previous key signs while the configured current key stages.
	store := &fakePushSigningStore{state: database.PushSigningState{KeyID: "unrelated-key", Revision: 3}}
	cfg := pushHandoffConfig(t)
	signer := pushHandoffSigner(t, store, cfg)
	_, id, _ := pushTestToken(t, signer)
	key, err := readPushSigningKey(cfg.PreviousPrivateKeyFile)
	require.NoError(t, err)
	require.Equal(t, key.jwk.KeyID, id)
	require.Len(t, pushTestJWKS(t, signer), 2)
}

func pushHandoffConfig(t *testing.T) PushJWTSignerConfig {
	t.Helper()
	_, current, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return PushJWTSignerConfig{
		PrivateKeyFile:         pushKeyFile(t, pemKey(t, current)),
		PreviousPrivateKeyFile: pushKeyFile(t, []byte(testPushPrivateKeyPEM)),
		Issuer:                 testPushIssuer + "/", ActivationDelay: 5 * time.Minute,
	}
}

func pushHandoffSigner(t *testing.T, store pushSigningStore, cfg PushJWTSignerConfig) *PushJWTSigner {
	t.Helper()
	signer, err := NewPushJWTSigner(t.Context(), store, cfg)
	require.NoError(t, err)
	return signer
}

// Call only inside synctest, and stop before leaving the bubble.
func startPushTestReloader(t *testing.T, signer *PushJWTSigner) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- signer.Start(ctx) }()
	synctest.Wait()
	return func() { cancel(); require.NoError(t, <-done) }
}
