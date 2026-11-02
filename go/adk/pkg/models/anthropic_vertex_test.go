package models

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/require"
)

// Under KAGENT_SKIP_VERTEX_AUTH the Vertex AI client is built without
// Application Default Credentials and sends the Vertex-shaped request with the
// placeholder bearer the gateway replaces.
func TestNewAnthropicVertexAIModelWithGatewayCredential(t *testing.T) {
	t.Setenv("KAGENT_SKIP_VERTEX_AUTH", "true")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent/credentials.json")
	var got *http.Request
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5@20250929","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(server.Close)

	model, err := NewAnthropicVertexAIModel(t.Context(), &AnthropicConfig{Model: "claude-sonnet-4-5@20250929"}, "us-east5", "project")
	require.NoError(t, err)
	_, err = model.Client.Messages.New(t.Context(), anthropic.MessageNewParams{
		Model: "claude-sonnet-4-5@20250929", MaxTokens: 16,
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}, option.WithBaseURL(server.URL))
	require.NoError(t, err)
	require.Equal(t, "/v1/projects/project/locations/us-east5/publishers/anthropic/models/claude-sonnet-4-5@20250929:rawPredict", got.URL.Path)
	require.Equal(t, "Bearer "+VertexGatewayToken, got.Header.Get("Authorization"))
	var sent map[string]any
	require.NoError(t, json.Unmarshal(body, &sent))
	require.Equal(t, "vertex-2023-10-16", sent["anthropic_version"])
	require.NotContains(t, sent, "model")
}

// Without KAGENT_SKIP_VERTEX_AUTH the client authorizes requests with
// Application Default Credentials on top of the configured transport, so both
// the ADC access token and the transport's headers reach Vertex AI.
func TestNewAnthropicVertexAIModelWithApplicationDefaultCredentials(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"adc-access-token","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(tokenServer.Close)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	credentials, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   "project",
		"client_email": "agent@project.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})),
		"token_uri":    tokenServer.URL,
	})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "credentials.json")
	require.NoError(t, os.WriteFile(path, credentials, 0o600))
	t.Setenv("KAGENT_SKIP_VERTEX_AUTH", "false")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)

	var got *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5@20250929","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(server.Close)

	config := &AnthropicConfig{Model: "claude-sonnet-4-5@20250929", TransportConfig: TransportConfig{Headers: map[string]string{"X-Team": "platform"}}}
	model, err := NewAnthropicVertexAIModel(t.Context(), config, "us-east5", "project")
	require.NoError(t, err)
	_, err = model.Client.Messages.New(t.Context(), anthropic.MessageNewParams{
		Model: "claude-sonnet-4-5@20250929", MaxTokens: 16,
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}, option.WithBaseURL(server.URL))
	require.NoError(t, err)
	require.Equal(t, "Bearer adc-access-token", got.Header.Get("Authorization"))
	require.Equal(t, "platform", got.Header.Get("X-Team"))
}
