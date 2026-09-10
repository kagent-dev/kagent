package models

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestModelCachedTokens(t *testing.T) {
	tests := []struct {
		name   string
		cached string
		want   int32
	}{
		{name: "cache hit", cached: "8", want: 8},
		{name: "omitted"},
		{name: "null", cached: "null"},
		{name: "zero", cached: "0"},
		{name: "negative", cached: "-1"},
		{name: "int32 limit", cached: "2147483647", want: math.MaxInt32},
		{name: "int32 overflow", cached: "2147483648", want: math.MaxInt32},
		{name: "int64 limit", cached: "9223372036854775807", want: math.MaxInt32},
		{name: "int64 minimum", cached: "-9223372036854775808"},
	}
	for _, provider := range []string{"openai_chat", "openai_responses", "anthropic", "bedrock"} {
		for _, stream := range []bool{false, true} {
			for _, test := range tests {
				if provider == "bedrock" && (test.name == "int32 overflow" || test.name == "int64 limit" || test.name == "int64 minimum") {
					continue
				}
				t.Run(fmt.Sprintf("%s/stream=%t/%s", provider, stream, test.name), func(t *testing.T) {
					contentType, payload := cachedTokensPayload(t, provider, test.cached, stream)
					server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						writer.Header().Set("Content-Type", contentType)
						_, _ = writer.Write(payload)
					}))
					t.Cleanup(server.Close)
					logger := slog.New(slog.DiscardHandler)
					var llm model.LLM
					switch provider {
					case "openai_chat", "openai_responses":
						config := &OpenAIConfig{Model: "gpt-4o"}
						if provider == "openai_responses" {
							config.APIFormat = OpenAIAPIFormatResponses
						}
						llm = &OpenAIModel{
							Config: config,
							Client: openai.NewClient(openaioption.WithAPIKey("test"), openaioption.WithBaseURL(server.URL)),
							Logger: logger,
						}
					case "anthropic":
						llm = &AnthropicModel{
							Config: &AnthropicConfig{Model: "claude-sonnet-4-20250514"},
							Client: anthropic.NewClient(anthropicoption.WithAPIKey("test"), anthropicoption.WithBaseURL(server.URL)),
							Logger: logger,
						}
					case "bedrock":
						llm = &BedrockModel{
							Config: &BedrockConfig{Model: "anthropic.claude-v2"},
							Client: bedrockruntime.New(bedrockruntime.Options{
								Region: "us-east-1", BaseEndpoint: aws.String(server.URL),
								Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
									return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
								}),
							}),
							Logger: logger,
						}
					}
					var final *model.LLMResponse
					for response, err := range llm.GenerateContent(context.Background(), &model.LLMRequest{
						Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "ping"}}}},
					}, stream) {
						require.NoError(t, err)
						require.Empty(t, response.ErrorCode, response.ErrorMessage)
						if !response.Partial {
							final = response
						}
					}
					require.NotNil(t, final)
					require.NotNil(t, final.UsageMetadata)
					require.Equal(t, test.want, final.UsageMetadata.CachedContentTokenCount)
					require.Equal(t, int32(10), final.UsageMetadata.PromptTokenCount)
					require.Equal(t, int32(5), final.UsageMetadata.CandidatesTokenCount)
				})
			}
		}
	}
}

func cachedTokensPayload(t *testing.T, provider, cached string, stream bool) (string, []byte) {
	t.Helper()
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
	}
	field := ""
	if cached != "" {
		switch provider {
		case "openai_chat":
			field = fmt.Sprintf(`,"prompt_tokens_details":{"cached_tokens":%s}`, cached)
		case "openai_responses":
			field = fmt.Sprintf(`,"input_tokens_details":{"cached_tokens":%s}`, cached)
		case "anthropic":
			field = fmt.Sprintf(`,"cache_read_input_tokens":%s`, cached)
		case "bedrock":
			field = fmt.Sprintf(`,"cacheReadInputTokens":%s`, cached)
		}
	}
	var payload string
	switch provider {
	case "openai_chat":
		usage := fmt.Sprintf(`{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15%s}`, field)
		payload = fmt.Sprintf(`{"id":"chat_1","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":%s}`, usage)
		if stream {
			payload = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n"
			payload += fmt.Sprintf("data: {\"choices\":[],\"usage\":%s}\n\ndata: [DONE]\n\n", usage)
		}
	case "openai_responses":
		payload = fmt.Sprintf(`{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15%s}}`, field)
		if stream {
			payload = fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":%s}\n\n", payload)
		}
	case "anthropic":
		payload = fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":3%s}}`, field)
		if stream {
			payload = fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":%s}\n\n", payload)
			payload += "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n"
			payload += "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		}
	case "bedrock":
		usage := fmt.Sprintf(`{"inputTokens":10,"outputTokens":5,"totalTokens":15,"cacheWriteInputTokens":3%s}`, field)
		payload = fmt.Sprintf(`{"output":{"message":{"role":"assistant","content":[{"text":"pong"}]}},"stopReason":"end_turn","usage":%s}`, usage)
		if stream {
			var buffer bytes.Buffer
			require.NoError(t, eventstream.NewEncoder().Encode(&buffer, eventstream.Message{
				Headers: eventstream.Headers{
					{Name: ":message-type", Value: eventstream.StringValue("event")},
					{Name: ":event-type", Value: eventstream.StringValue("metadata")},
					{Name: ":content-type", Value: eventstream.StringValue("application/json")},
				},
				Payload: []byte(fmt.Sprintf(`{"usage":%s}`, usage)),
			}))
			return "application/vnd.amazon.eventstream", buffer.Bytes()
		}
	}
	return contentType, []byte(payload)
}

func TestAnthropicStreamingCachedTokenDeltas(t *testing.T) {
	for _, test := range []struct {
		name  string
		delta string
		want  int32
	}{
		{name: "omitted", want: 8},
		{name: "null", delta: `,"cache_read_input_tokens":null`, want: 8},
		{name: "cumulative update", delta: `,"cache_read_input_tokens":12`, want: 12},
		{name: "explicit zero", delta: `,"cache_read_input_tokens":0`},
		{name: "negative", delta: `,"cache_read_input_tokens":-1`},
		{name: "overflow", delta: `,"cache_read_input_tokens":2147483648`, want: math.MaxInt32},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"cache_read_input_tokens\":8}}}\n\n")
				_, _ = fmt.Fprintf(writer, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":0%s}}\n\n", test.delta)
				_, _ = io.WriteString(writer, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			}))
			t.Cleanup(server.Close)
			llm := &AnthropicModel{
				Config: &AnthropicConfig{Model: "claude-sonnet-4-20250514"},
				Client: anthropic.NewClient(anthropicoption.WithAPIKey("test"), anthropicoption.WithBaseURL(server.URL)),
				Logger: slog.New(slog.DiscardHandler),
			}
			var final *model.LLMResponse
			for response, err := range llm.GenerateContent(context.Background(), &model.LLMRequest{}, true) {
				require.NoError(t, err)
				require.Empty(t, response.ErrorCode, response.ErrorMessage)
				final = response
			}
			require.NotNil(t, final)
			if test.want == 0 {
				require.Nil(t, final.UsageMetadata)
			} else {
				require.NotNil(t, final.UsageMetadata)
				require.Equal(t, test.want, final.UsageMetadata.CachedContentTokenCount)
			}
		})
	}
}
