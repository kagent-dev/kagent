package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/require"
)

// With the egress gateway minting the access token, the Vertex AI client is
// built without Application Default Credentials and sends the Vertex-shaped
// request with the placeholder bearer the gateway overwrites.
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
	defer server.Close()

	model, err := NewAnthropicVertexAIModel(context.Background(), &AnthropicConfig{Model: "claude-sonnet-4-5@20250929"}, "us-east5", "project")
	require.NoError(t, err)
	_, err = model.Client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model: "claude-sonnet-4-5@20250929", MaxTokens: 16,
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}, option.WithBaseURL(server.URL))
	require.NoError(t, err)
	require.Equal(t, "/v1/projects/project/locations/us-east5/publishers/anthropic/models/claude-sonnet-4-5@20250929:rawPredict", got.URL.Path)
	require.Equal(t, "Bearer kagent-credential-injected", got.Header.Get("Authorization"))
	var sent map[string]any
	require.NoError(t, json.Unmarshal(body, &sent))
	require.Equal(t, "vertex-2023-10-16", sent["anthropic_version"])
	require.NotContains(t, sent, "model")
}
