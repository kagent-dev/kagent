package a2a

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestADKToolResponse(t *testing.T) {
	for _, test := range []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"MCP result", map[string]any{"output": map[string]any{"structuredContent": map[string]any{"ready": true}}}, `{"output":{"ready":true}}`},
		{"function result", map[string]any{"memories": []any{}}, `{"output":{"memories":[]}}`},
		{"confirmation", map[string]any{"error": "confirmation required"}, `{"error":"confirmation required"}`},
		{"pending input", map[string]any{"status": "pending", "questions": []any{"namespace?"}}, `{"pending":{"status":"pending","questions":["namespace?"]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := adkToolResponse(test.input)
			require.NoError(t, err)
			encoded, err := protojson.Marshal(response)
			require.NoError(t, err)
			require.JSONEq(t, test.want, string(encoded))
		})
	}
}

func TestToolResponseRoundTrip(t *testing.T) {
	for _, response := range []map[string]any{
		{"result": "application data"},
		{"status": "pending", "questions": []any{"namespace?"}},
	} {
		original := genai.NewPartFromFunctionResponse("lookup", response)
		original.FunctionResponse.ID = "call"
		encoded, err := genAIPartConverter(context.Background(), &adksession.Event{}, original)
		require.NoError(t, err)
		canonicalizeADKPart(encoded)
		decoded, err := a2aPartConverter(context.Background(), nil, encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, original.FunctionResponse, decoded.FunctionResponse)
	}
}
