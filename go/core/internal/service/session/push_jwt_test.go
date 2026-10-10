package session

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
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
