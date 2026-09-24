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
	params, err := buildAnthropicParams(anthropicAgentLoopRequest(), &AnthropicConfig{Model: "claude-sonnet-4-6"})
	require.NoError(t, err)

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
			params, err := buildAnthropicParams(anthropicAgentLoopRequest(), &AnthropicConfig{Model: "claude-sonnet-4-6", PromptCaching: true, CacheTTL: tt.cacheTTL})
			require.NoError(t, err)

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

	params, err := buildAnthropicParams(req, &AnthropicConfig{Model: "claude-sonnet-4-6", PromptCaching: true})
	require.NoError(t, err)

	wire := decodeWireRequest(t, marshalParams(t, params))
	last := wire.Messages[len(wire.Messages)-1]
	block := lastBlock(t, last.Content)
	assert.Equal(t, "tool_result", block["type"])
	assert.Equal(t, map[string]any{"type": "ephemeral"}, block["cache_control"])
}

func TestBuildAnthropicParamsPromptCachingWithoutToolsOrSystem(t *testing.T) {
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}}}

	params, err := buildAnthropicParams(req, &AnthropicConfig{Model: "claude-sonnet-4-6", PromptCaching: true})
	require.NoError(t, err)

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
	return anthropicTestServerWithConfig(t, &AnthropicConfig{Model: "claude-sonnet-4-6", PromptCaching: true}, handler)
}

// anthropicTestServerWithConfig is anthropicTestServer for a model built from
// cfg, pointed at the test server.
func anthropicTestServerWithConfig(t *testing.T, cfg *AnthropicConfig, handler func(w http.ResponseWriter)) (*AnthropicModel, *[]byte) {
	t.Helper()
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		captured = body
		handler(w)
	}))
	t.Cleanup(server.Close)

	cfg.BaseUrl = server.URL
	m, err := newAnthropicModelFromConfig(context.Background(), cfg, "test-key")
	require.NoError(t, err)
	return m, &captured
}

// anthropicEventStream serves Messages API stream events the way the API does:
// one server-sent event per stream event, named after its type.
func anthropicEventStream(t *testing.T, events []string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			var envelope struct {
				Type string `json:"type"`
			}
			require.NoError(t, json.Unmarshal([]byte(event), &envelope))
			_, _ = io.WriteString(w, "event: "+envelope.Type+"\ndata: "+event+"\n\n")
		}
	}
}

// cachedReplyEvents stream the reply "pod-a" to a request that read 900 prompt
// tokens from the cache and wrote 96.
var cachedReplyEvents = []string{
	`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":4,"cache_creation_input_tokens":96,"cache_read_input_tokens":900,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pod-a"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}`,
	`{"type":"message_stop"}`,
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
	m, captured := anthropicTestServer(t, anthropicEventStream(t, cachedReplyEvents))

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
	m, captured := anthropicTestServer(t, anthropicEventStream(t, cachedReplyEvents))

	final := finalResponse(t, m, true)

	wire := decodeWireRequest(t, *captured)
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.System)["cache_control"])
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.Tools)["cache_control"])
	assert.Equal(t, map[string]any{"type": "ephemeral"}, lastBlock(t, wire.Messages[len(wire.Messages)-1].Content)["cache_control"])
	assert.Equal(t, &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1000, CachedContentTokenCount: 900, CandidatesTokenCount: 5}, final.UsageMetadata)
	require.Len(t, final.Content.Parts, 1)
	assert.Equal(t, "pod-a", final.Content.Parts[0].Text)
}

func TestAnthropicModelGenerateContentSendsStructuredOutputWithTools(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(encoded, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg-1","type":"message","role":"assistant","model":"claude-sonnet-4-5",
			"content":[{"type":"text","text":"{\"answer\":4}"}],
			"stop_reason":"end_turn","stop_sequence":null,
			"usage":{"input_tokens":1,"output_tokens":1}
		}`)
	}))
	defer server.Close()

	llm, err := newAnthropicModelFromConfig(context.Background(), &AnthropicConfig{
		Model:   "claude-sonnet-4-5",
		BaseUrl: server.URL,
	}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"answer": map[string]any{"type": "integer"}},
		"required":             []any{"answer"},
		"additionalProperties": false,
	}
	request := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "calculate"}}}},
		Config: &genai.GenerateContentConfig{
			ResponseJsonSchema: schema,
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: "calculator", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			}}}},
		},
	}
	for _, generateErr := range llm.GenerateContent(context.Background(), request, false) {
		if generateErr != nil {
			t.Fatalf("GenerateContent error: %v", generateErr)
		}
	}

	outputConfig, ok := body["output_config"].(map[string]any)
	if !ok {
		t.Fatalf("output_config = %#v", body["output_config"])
	}
	format, ok := outputConfig["format"].(map[string]any)
	if !ok || format["type"] != "json_schema" {
		t.Fatalf("output_config.format = %#v", outputConfig["format"])
	}
	if gotSchema, ok := format["schema"].(map[string]any); !ok || gotSchema["additionalProperties"] != false {
		t.Fatalf("output_config.format.schema = %#v", format["schema"])
	}
	if tools, ok := body["tools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", body["tools"])
	}
}

// A ModelConfig's maxTokens is sized for the agent's streamed replies, and the
// SDK refuses a non-streaming call whose max_tokens is above 21,333 before
// sending it. A non-streaming caller, the context compaction summarizer among
// them, still gets its one final response: the call streams and the message is
// accumulated.
func TestAnthropicNonStreamingAnswersAboveTheSDKsNonStreamingMaxTokens(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":12,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pod-a is "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"running."}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"list_pods","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"namespace\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"kagent\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":9}}`,
		`{"type":"message_stop"}`,
	}
	maxTokens := 32000
	m, captured := anthropicTestServerWithConfig(t, &AnthropicConfig{Model: "claude-sonnet-5", MaxTokens: &maxTokens}, anthropicEventStream(t, events))

	var responses []*model.LLMResponse
	for resp, err := range m.GenerateContent(context.Background(), anthropicAgentLoopRequest(), false) {
		require.NoError(t, err)
		responses = append(responses, resp)
	}

	var wire struct {
		Stream    bool `json:"stream"`
		MaxTokens int  `json:"max_tokens"`
	}
	require.NoError(t, json.Unmarshal(*captured, &wire))
	assert.True(t, wire.Stream)
	assert.Equal(t, 32000, wire.MaxTokens)

	require.Len(t, responses, 1, "a non-streaming call yields its final response only")
	final := responses[0]
	assert.False(t, final.Partial)
	assert.True(t, final.TurnComplete)
	assert.Equal(t, genai.FinishReasonStop, final.FinishReason)
	assert.Equal(t, &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 12, CandidatesTokenCount: 9}, final.UsageMetadata)
	require.Len(t, final.Content.Parts, 2)
	assert.Equal(t, "pod-a is running.", final.Content.Parts[0].Text)
	call := final.Content.Parts[1].FunctionCall
	require.NotNil(t, call)
	assert.Equal(t, "toolu_1", call.ID)
	assert.Equal(t, "list_pods", call.Name)
	assert.Equal(t, map[string]any{"namespace": "kagent"}, call.Args)
}

func TestAnthropicNonStreamingReportsAnAPIError(t *testing.T) {
	m, _ := anthropicTestServer(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long"}}`)
	})

	var errs []error
	for resp, err := range m.GenerateContent(context.Background(), anthropicAgentLoopRequest(), false) {
		assert.Nil(t, resp)
		errs = append(errs, err)
	}

	require.Len(t, errs, 1)
	require.Error(t, errs[0])
	assert.Contains(t, errs[0].Error(), "anthropic API error")
	assert.Contains(t, errs[0].Error(), "prompt is too long")
}
