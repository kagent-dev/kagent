package session

import (
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
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testPushPrivateKeyPEM = "-----BEGIN PRIVATE KEY-----\nMC4CAQAwBQYDK2VwBCIEIHNzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nz\n-----END PRIVATE KEY-----\n"

func TestPushJWTSigningAndJWKS(t *testing.T) {
	signer, err := NewPushJWTSigner(testPushPrivateKeyPEM, "https://kagent.example")
	require.NoError(t, err)
	credential, err := signer.Sign("task-1", "https://receiver.example/callback")
	require.NoError(t, err)
	retryCredential, err := signer.Sign("task-1", "https://receiver.example/callback")
	require.NoError(t, err)
	require.NotEqual(t, credential, retryCredential, "each attempt needs a fresh JWT ID")
	response := httptest.NewRecorder()
	signer.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	require.Equal(t, http.StatusOK, response.Code)
	restarted, err := NewPushJWTSigner(testPushPrivateKeyPEM, "https://kagent.example")
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
	_, err := NewPushJWTSigner("bad", "https://kagent.example")
	require.ErrorContains(t, err, "PKCS#8 PEM private key")
	_, err = NewPushJWTSigner(testPushPrivateKeyPEM, "")
	require.ErrorContains(t, err, "issuer is required")
	signer, err := NewPushJWTSigner("", "https://kagent.example")
	require.NoError(t, err)
	require.Nil(t, signer)
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
