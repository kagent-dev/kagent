package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// anthropicAgentLoopRequest is a typical agent-loop request: a system prompt,
// two tools, a completed tool call and a fresh user turn.
func anthropicAgentLoopRequest() *model.LLMRequest {
	return &model.LLMRequest{
		Model: "anthropic",
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "list the pods"}}},
			{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "call-1", Name: "list_pods", Args: map[string]any{}}}}},
			{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "call-1", Name: "list_pods", Response: map[string]any{"result": "pod-a"}}}}},
			{Role: "user", Parts: []*genai.Part{{Text: "anything else?"}}},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "You are a Kubernetes assistant."}}},
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
				{Name: "get_weather", Description: "lookup weather"},
				{Name: "list_pods", Description: "list pods"},
			}}},
		},
	}
}

// wireRequest is the Messages API body as the SDK serializes it, so the tests
// assert what Anthropic receives rather than SDK-internal field layout.
type wireRequest struct {
	System   []map[string]any `json:"system"`
	Tools    []map[string]any `json:"tools"`
	Messages []struct {
		Role    string           `json:"role"`
		Content []map[string]any `json:"content"`
	} `json:"messages"`
}

func decodeWireRequest(t *testing.T, body []byte) wireRequest {
	t.Helper()
	var req wireRequest
	require.NoError(t, json.Unmarshal(body, &req))
	return req
}

func marshalParams(t *testing.T, params anthropic.MessageNewParams) []byte {
	t.Helper()
	body, err := json.Marshal(params)
	require.NoError(t, err)
	return body
}

func lastBlock(t *testing.T, blocks []map[string]any) map[string]any {
	t.Helper()
	require.NotEmpty(t, blocks)
	return blocks[len(blocks)-1]
}

func TestBuildAnthropicParamsWithoutPromptCachingSendsNoBreakpoints(t *testing.T) {
	params := buildAnthropicParams(anthropicAgentLoopRequest(), &AnthropicConfig{Model: "claude-sonnet-4-6"})

	body := marshalParams(t, params)
	assert.NotContains(t, string(body), "cache_control")
	wire := decodeWireRequest(t, body)
	require.Len(t, wire.System, 1)
	require.Len(t, wire.Tools, 2)
	require.Len(t, wire.Messages, 4)
}

func TestBuildAnthropicParamsPromptCachingMarksToolsSystemAndLatestTurn(t *testing.T) {
	tests := []struct {
		name     string
		cacheTTL string
		want     map[string]any
	}{
		{name: "default TTL leaves ttl unset", cacheTTL: "", want: map[string]any{"type": "ephemeral"}},
		{name: `"5m" leaves ttl unset`, cacheTTL: "5m", want: map[string]any{"type": "ephemeral"}},
		{name: `"1h" requests the hour-long cache`, cacheTTL: "1h", want: map[string]any{"type": "ephemeral", "ttl": "1h"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := buildAnthropicParams(anthropicAgentLoopRequest(), &AnthropicConfig{Model: "claude-sonnet-4-6", PromptCaching: true, CacheTTL: tt.cacheTTL})

			body := marshalParams(t, params)
			assert.Equal(t, 3, strings.Count(string(body), `"cache_control"`), "one breakpoint each for tools, system and the latest turn: %s", body)
			wire := decodeWireRequest(t, body)

			// Tools render first: the marker sits on the last definition so the whole list is one cached prefix.
			assert.Equal(t, tt.want, lastBlock(t, wire.Tools)["cache_control"])
			assert.NotContains(t, wire.Tools[0], "cache_control", "earlier tools must stay unmarked")
			assert.Equal(t, "list_pods", lastBlock(t, wire.Tools)["name"], "tool order must be preserved")

			assert.Equal(t, tt.want, lastBlock(t, wire.System)["cache_control"])

			last := wire.Messages[len(wire.Messages)-1]
			assert.Equal(t, "user", last.Role)
			assert.Equal(t, tt.want, lastBlock(t, last.Content)["cache_control"])
			for _, message := range wire.Messages[:len(wire.Messages)-1] {
				for _, block := range message.Content {
					assert.NotContains(t, block, "cache_control", "only the latest turn carries the moving breakpoint")
				}
			}
		})
	}
}

func TestBuildAnthropicParamsPromptCachingMarksTrailingToolResult(t *testing.T) {
	req := anthropicAgentLoopRequest()
	req.Contents = req.Contents[:3] // the turn ends with the tool result

	params := buildAnthropicParams(req, &AnthropicConfig{Model: "claude-sonnet-4-6", PromptCaching: true})

	wire := decodeWireRequest(t, marshalParams(t, params))
	last := wire.Messages[len(wire.Messages)-1]
	block := lastBlock(t, last.Content)
	assert.Equal(t, "tool_result", block["type"])
	assert.Equal(t, map[string]any{"type": "ephemeral"}, block["cache_control"])
}

func TestBuildAnthropicParamsPromptCachingWithoutToolsOrSystem(t *testing.T) {
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}}}

	params := buildAnthropicParams(req, &AnthropicConfig{Model: "claude-sonnet-4-6", PromptCaching: true})

	body := marshalParams(t, params)
	assert.Equal(t, 1, strings.Count(string(body), `"cache_control"`), "only the conversation breakpoint applies: %s", body)
	wire := decodeWireRequest(t, body)
	assert.Empty(t, wire.System)
	assert.Empty(t, wire.Tools)
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.Messages[0].Content)["cache_control"])
}

func TestAnthropicUsageToGenai(t *testing.T) {
	tests := []struct {
		name  string
		usage anthropic.Usage
		want  *genai.GenerateContentResponseUsageMetadata
	}{
		{name: "no usage", usage: anthropic.Usage{}, want: nil},
		{
			name:  "uncached",
			usage: anthropic.Usage{InputTokens: 10, OutputTokens: 5},
			want:  &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 5},
		},
		{
			name:  "cache read and write fold into the prompt count",
			usage: anthropic.Usage{InputTokens: 4, CacheReadInputTokens: 900, CacheCreationInputTokens: 96, OutputTokens: 5},
			want:  &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1000, CachedContentTokenCount: 900, CandidatesTokenCount: 5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, anthropicUsageToGenai(tt.usage))
		})
	}
}

// anthropicTestServer serves canned Messages API responses and records the
// request body the SDK sent.
func anthropicTestServer(t *testing.T, handler func(w http.ResponseWriter)) (*AnthropicModel, *[]byte) {
	t.Helper()
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		captured = body
		handler(w)
	}))
	t.Cleanup(server.Close)

	m, err := newAnthropicModelFromConfig(&AnthropicConfig{Model: "claude-sonnet-4-6", BaseUrl: server.URL, PromptCaching: true}, "test-key", logr.Discard())
	require.NoError(t, err)
	return m, &captured
}

func finalResponse(t *testing.T, m *AnthropicModel, stream bool) *model.LLMResponse {
	t.Helper()
	var final *model.LLMResponse
	for resp, err := range m.GenerateContent(context.Background(), anthropicAgentLoopRequest(), stream) {
		require.NoError(t, err)
		require.Empty(t, resp.ErrorMessage)
		if resp.TurnComplete {
			final = resp
		}
	}
	require.NotNil(t, final, "no final response yielded")
	return final
}

func TestAnthropicNonStreamingCarriesBreakpointsAndCacheUsage(t *testing.T) {
	m, captured := anthropicTestServer(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"pod-a"}],"stop_reason":"end_turn","stop_sequence":null,
			"usage":{"input_tokens":4,"cache_creation_input_tokens":96,"cache_read_input_tokens":900,"output_tokens":5}}`)
	})

	final := finalResponse(t, m, false)

	wire := decodeWireRequest(t, *captured)
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.System)["cache_control"])
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.Tools)["cache_control"])
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.Messages[len(wire.Messages)-1].Content)["cache_control"])
	assert.Equal(t, &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1000, CachedContentTokenCount: 900, CandidatesTokenCount: 5}, final.UsageMetadata)
	require.Len(t, final.Content.Parts, 1)
	assert.Equal(t, "pod-a", final.Content.Parts[0].Text)
}

func TestAnthropicStreamingCarriesBreakpointsAndCacheUsage(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":4,"cache_creation_input_tokens":96,"cache_read_input_tokens":900,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pod-a"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}`,
		`{"type":"message_stop"}`,
	}
	m, captured := anthropicTestServer(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			var envelope struct {
				Type string `json:"type"`
			}
			require.NoError(t, json.Unmarshal([]byte(event), &envelope))
			_, _ = io.WriteString(w, "event: "+envelope.Type+"\ndata: "+event+"\n\n")
		}
	})

	final := finalResponse(t, m, true)

	wire := decodeWireRequest(t, *captured)
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.System)["cache_control"])
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.Tools)["cache_control"])
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.Messages[len(wire.Messages)-1].Content)["cache_control"])
	assert.Equal(t, &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1000, CachedContentTokenCount: 900, CandidatesTokenCount: 5}, final.UsageMetadata)
	require.Len(t, final.Content.Parts, 1)
	assert.Equal(t, "pod-a", final.Content.Parts[0].Text)
}
