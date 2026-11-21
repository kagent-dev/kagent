package session

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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
)

const testPushPrivateKeyPEM = "-----BEGIN PRIVATE KEY-----\nMC4CAQAwBQYDK2VwBCIEIHNzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nz\n-----END PRIVATE KEY-----\n"

func TestPushJWTSigningAndJWKS(t *testing.T) {
	signer, err := NewPushJWTSigner(pushKeyFile(t, []byte(testPushPrivateKeyPEM)), "https://kagent.example", 5*time.Minute)
	require.NoError(t, err)
	credential, err := signer.Sign("task-1", "https://receiver.example/callback")
	require.NoError(t, err)
	retryCredential, err := signer.Sign("task-1", "https://receiver.example/callback")
	require.NoError(t, err)
	require.NotEqual(t, credential, retryCredential, "each attempt needs a fresh JWT ID")
	response := httptest.NewRecorder()
	signer.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	require.Equal(t, http.StatusOK, response.Code)
	restarted, err := NewPushJWTSigner(pushKeyFile(t, []byte(testPushPrivateKeyPEM)), "https://kagent.example", 5*time.Minute)
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
	_, err := NewPushJWTSigner(pushKeyFile(t, []byte("bad")), "https://kagent.example", 5*time.Minute)
	require.ErrorContains(t, err, "PKCS#8 PEM private key")
	_, err = NewPushJWTSigner(pushKeyFile(t, []byte(testPushPrivateKeyPEM)), "", 5*time.Minute)
	require.ErrorContains(t, err, "issuer is required")
	signer, err := NewPushJWTSigner("", "https://kagent.example", 5*time.Minute)
	require.NoError(t, err)
	require.Nil(t, signer)
	path := pushKeyFile(t, []byte(testPushPrivateKeyPEM))
	t.Run("Given a valid key, when the activation delay is 24h, assert that construction succeeds", func(t *testing.T) {
		signer, err := NewPushJWTSigner(path, "https://kagent.example", 24*time.Hour)
		require.NoError(t, err)
		require.NotNil(t, signer)
	})
	for _, delay := range []time.Duration{0, -time.Nanosecond, 24*time.Hour + time.Nanosecond, time.Duration(1<<63 - 1)} {
		t.Run("Given a valid key, when the activation delay is "+delay.String()+", assert that construction is rejected", func(t *testing.T) {
			_, err := NewPushJWTSigner(path, "https://kagent.example", delay)
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
		require.NoError(t, signer.reload())
		keys := pushTestJWKS(t, signer)
		require.Len(t, keys, 2)
		time.Sleep(5*time.Minute + pushKeyPropagationBuffer - time.Second)
		require.NoError(t, signer.reload(), "unchanged file must preserve activation time")
		_, beforeID, lastExpiration := pushTestToken(t, signer)
		require.Equal(t, oldID, beforeID)
		time.Sleep(time.Second)
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
				peer, err := NewPushJWTSigner(signer.path, signer.issuer, signer.activationDelay)
				require.NoError(t, err)
				if scenario.signBeforeReload {
					pushTestToken(t, signer)
				}
				_, replacement, err := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(signer.path, pemKey(t, replacement), 0o600))
				require.NoError(t, signer.reload())
				time.Sleep(signer.activationDelay + pushKeyPropagationBuffer)
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
			require.Error(t, signer.reload())
			_, id, _ := pushTestToken(t, signer)
			require.Equal(t, oldID, id)
			require.NoError(t, os.Remove(signer.path))
			require.Error(t, signer.reload())
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
			require.NoError(t, signer.reload())
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
		require.NoError(t, signer.reload())
		time.Sleep(6 * time.Minute)
		_, id, _ := pushTestToken(t, signer)
		require.Equal(t, originalID, id, "rollback to active key cancels pending activation")
		require.Len(t, pushTestJWKS(t, signer), 1)
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
				require.NoError(t, signer.reload())
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
	require.NoError(t, signer.reload())
	restarted, err := NewPushJWTSigner(signer.path, "https://kagent.example", 5*time.Minute)
	require.NoError(t, err)
	_, id, _ := pushTestToken(t, restarted)
	require.NotEqual(t, originalID, id)
	require.Len(t, pushTestJWKS(t, restarted), 1)
}

func TestPushRollbackRetainsEarlierTokens(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := pushKeyFile(t, []byte(testPushPrivateKeyPEM))
		signer, err := NewPushJWTSigner(path, "https://kagent.example", time.Second)
		require.NoError(t, err)
		_, originalID, expiration := pushTestToken(t, signer)
		stage := func(material []byte) {
			require.NoError(t, os.WriteFile(path, material, 0o600))
			require.NoError(t, signer.reload())
			time.Sleep(time.Second + pushKeyPropagationBuffer)
			pushTestJWKS(t, signer) // activation without issuing another token
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
