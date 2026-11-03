// Package jwttest serves a test OIDC issuer for runtime authentication tests.
package jwttest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/stretchr/testify/require"
)

type Issuer struct {
	URL    string
	CAFile string
	Key    *ecdsa.PrivateKey
	keys   atomic.Value
}

func New(t *testing.T) *Issuer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	i := &Issuer{Key: key}
	i.SetKey(t, &key.PublicKey, "test-key")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"issuer": i.URL, "jwks_uri": i.URL + "/keys"}))
		case "/keys":
			_, err := w.Write(i.keys.Load().([]byte))
			require.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	i.URL = server.URL
	i.CAFile = filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(i.CAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600))
	return i
}

func (i *Issuer) SetKey(t *testing.T, publicKey any, kid string) {
	t.Helper()
	key, err := jwk.New(publicKey)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, kid))
	require.NoError(t, key.Set(jwk.AlgorithmKey, "ES256"))
	require.NoError(t, key.Set(jwk.KeyUsageKey, "sig"))
	set := jwk.NewSet()
	set.Add(key)
	data, err := json.Marshal(set)
	require.NoError(t, err)
	i.keys.Store(data)
}

func (i *Issuer) Claims(atespace, name, uid string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss": i.URL, "aud": []string{"kagent-runtime"}, "sub": "actor/" + atespace + "/" + name,
		"exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(), "nbf": now.Add(-5 * time.Minute).Unix(),
		"actor.ate.dev": map[string]string{"atespace": atespace, "actorName": name, "actorUID": uid},
	}
}

func (i *Issuer) Token(t *testing.T, claims jwt.Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = "test-key"
	raw, err := token.SignedString(i.Key)
	require.NoError(t, err)
	return raw
}
