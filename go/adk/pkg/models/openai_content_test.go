package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestOpenAIMistralContent(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			thinking := `{"type":"thinking","thinking":[{"type":"text","text":"Let me think."}],"closed":true}`
			answer := `{"type":"text","text":"{\"answer\":"}`
			reference := `{"type":"reference","reference_ids":[1]}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					for _, content := range []string{
						`[{"type":"thinking","thinking":[{"type":"text","text":"Let me "}],"closed":false}]`,
						`[{"type":"thinking","thinking":[{"type":"text","text":"think."}],"closed":false}]`,
						`[{"type":"thinking","thinking":[],"closed":true},` + answer + `]`,
						`"42}"`,
						`[` + reference + `]`,
					} {
						fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%s}}]}\n\n", content)
					}
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":5,\"total_tokens\":8}}\n\ndata: [DONE]\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":[%s,{"type":"text","text":"{\"answer\":42}"},%s]},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`, thinking, reference)
				}
			}))
			t.Cleanup(server.Close)
			m := &OpenAIModel{
				Config: &OpenAIConfig{Model: "mistral-test"},
				Client: openai.NewClient(option.WithBaseURL(server.URL), option.WithAPIKey("test")),
			}
			var final *model.LLMResponse
			var partialThought, partialAnswer strings.Builder
			for resp, err := range m.GenerateContent(context.Background(), &model.LLMRequest{
				Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)},
			}, stream) {
				require.NoError(t, err)
				require.Empty(t, resp.ErrorCode, resp.ErrorMessage)
				if !resp.Partial {
					final = resp
					continue
				}
				for _, part := range resp.Content.Parts {
					require.NotEmpty(t, part.Text)
					if part.Thought {
						partialThought.WriteString(part.Text)
					} else {
						partialAnswer.WriteString(part.Text)
					}
				}
			}
			require.NotNil(t, final)
			require.Len(t, final.Content.Parts, 2)
			require.True(t, final.TurnComplete)
			require.Equal(t, genai.FinishReasonStop, final.FinishReason)
			require.Equal(t, int32(8), final.UsageMetadata.TotalTokenCount)
			var thoughtText, answerText strings.Builder
			for _, part := range final.Content.Parts {
				if part.Thought {
					thoughtText.WriteString(part.Text)
				} else {
					answerText.WriteString(part.Text)
				}
			}
			require.JSONEq(t, `{"answer":42}`, answerText.String())
			require.Equal(t, "Let me think.", thoughtText.String())
			if stream {
				require.Equal(t, thoughtText.String(), partialThought.String())
				require.Equal(t, answerText.String(), partialAnswer.String())
			}

			// Persisted ADK content must retain the native chunks for the next turn.
			stored, err := json.Marshal(final.Content)
			require.NoError(t, err)
			var history genai.Content
			require.NoError(t, json.Unmarshal(stored, &history))
			messages, _ := genaiContentsToOpenAIMessages([]*genai.Content{&history}, nil)
			require.Len(t, messages, 1)
			require.NotNil(t, messages[0].OfAssistant)
			wire, err := json.Marshal(messages[0])
			require.NoError(t, err)
			wantThinking := thinking
			if stream {
				wantThinking = `{"type":"thinking","thinking":[{"type":"text","text":"Let me "},{"type":"text","text":"think."}],"closed":true}`
			}
			require.JSONEq(t, `{"role":"assistant","content":[`+wantThinking+`,{"type":"text","text":"{\"answer\":42}"}],"tool_calls":[]}`, string(wire))
			var replay struct {
				Role    string `json:"role"`
				Content []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					Thinking []struct {
						Text string `json:"text"`
					} `json:"thinking"`
					Closed *bool `json:"closed"`
				} `json:"content"`
			}
			require.NoError(t, json.Unmarshal(wire, &replay))
			require.Equal(t, "assistant", replay.Role)
			require.Len(t, replay.Content, 2)
			require.NotNil(t, replay.Content[0].Closed)
			require.True(t, *replay.Content[0].Closed)
			var replayThought, replayAnswer strings.Builder
			for _, chunk := range replay.Content {
				switch chunk.Type {
				case "thinking":
					require.NotNil(t, chunk.Closed)
					for _, inner := range chunk.Thinking {
						replayThought.WriteString(inner.Text)
					}
				case "text":
					replayAnswer.WriteString(chunk.Text)
				default:
					t.Fatalf("unexpected content chunk: %s", chunk.Type)
				}
			}
			require.Equal(t, thoughtText.String(), replayThought.String())
			require.Equal(t, answerText.String(), replayAnswer.String())
		})
	}
}

func TestOpenAIStreamingContentVariants(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content []string
		want    string
		wantErr bool
		thought bool
	}{
		{name: "plain text and empty deltas", content: []string{`null`, `"hello"`, `[]`, `" world"`, `""`}, want: "hello world"},
		{name: "literal JSON string", content: []string{`"[{\"type\":\"text\",\"text\":\"literal\"}]"`}, want: `[{"type":"text","text":"literal"}]`},
		{name: "invalid thinking", content: []string{`[{"type":"thinking","thinking":"invalid"}]`}, wantErr: true},
		{name: "unknown extension", content: []string{`[{"type":"future","text":{"value":42},"thinking":false},{"type":"text","text":"answer"}]`}, want: "answer"},
		{name: "thinking fragments marked closed", content: []string{
			`[{"type":"thinking","thinking":[{"type":"text","text":"a "}],"closed":true}]`,
			`[{"type":"thinking","thinking":[{"type":"text","text":"plan"}],"closed":true}]`,
		}, want: "a plan", thought: true},
		{name: "thinking extension", content: []string{`[{"type":"thinking","thinking":[{"type":"future","text":{"value":42}},{"type":"text","text":"a plan"}],"closed":true}]`}, want: "a plan", thought: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, content := range tt.content {
					fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%s}}]}\n\n", content)
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)
			m := &OpenAIModel{Client: openai.NewClient(option.WithBaseURL(server.URL), option.WithAPIKey("test"))}
			var responses []*model.LLMResponse
			runStreaming(context.Background(), m, openai.ChatCompletionNewParams{}, func(resp *model.LLMResponse, err error) bool {
				require.NoError(t, err)
				responses = append(responses, resp)
				return true
			})
			require.NotEmpty(t, responses)
			final := responses[len(responses)-1]
			if tt.wantErr {
				require.NotEmpty(t, final.ErrorCode)
				require.NotEmpty(t, final.ErrorMessage)
				require.Nil(t, final.Content)
			} else {
				require.Empty(t, final.ErrorCode)
				require.False(t, final.Partial)
				require.Len(t, final.Content.Parts, 1)
				require.Equal(t, tt.want, final.Content.Parts[0].Text)
				require.Equal(t, tt.thought, final.Content.Parts[0].Thought)
			}
		})
	}
}

func TestChatCompletionContentStrings(t *testing.T) {
	for _, text := range []string{"hello", `[{"type":"text","text":"literal JSON answer"}]`, ""} {
		t.Run(text, func(t *testing.T) {
			content, err := json.Marshal(text)
			require.NoError(t, err)
			var completion openai.ChatCompletion
			require.NoError(t, json.Unmarshal([]byte(`{"choices":[{"message":{"role":"assistant","content":`+string(content)+`}}]}`), &completion))
			resp := chatCompletionToLLMResponse(&completion)
			if text == "" {
				require.Empty(t, resp.Content.Parts)
			} else {
				require.Len(t, resp.Content.Parts, 1)
				require.Equal(t, text, resp.Content.Parts[0].Text)
				require.False(t, resp.Content.Parts[0].Thought)
			}
		})
	}
}

func TestMistralContentWithToolCalls(t *testing.T) {
	content := `[{"type":"thinking","thinking":[{"type":"text","text":"Use a tool."}],"closed":true}]`
	var completion openai.ChatCompletion
	require.NoError(t, json.Unmarshal([]byte(`{"choices":[{"message":{"role":"assistant","content":`+content+`,"tool_calls":[{"type":"function","id":"call_1","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`), &completion))
	resp := chatCompletionToLLMResponse(&completion)
	require.Len(t, resp.Content.Parts, 2)
	require.True(t, resp.Content.Parts[0].Thought)
	require.Equal(t, "lookup", resp.Content.Parts[1].FunctionCall.Name)
	result := genai.NewPartFromFunctionResponse("lookup", map[string]any{"result": "found"})
	result.FunctionResponse.ID = "call_1"
	messages, _ := genaiContentsToOpenAIMessages([]*genai.Content{
		resp.Content,
		{Role: "user", Parts: []*genai.Part{result}},
	}, nil)
	require.Len(t, messages, 2)
	wire, err := json.Marshal(messages[0])
	require.NoError(t, err)
	require.JSONEq(t, `{"role":"assistant","content":`+content+`,"tool_calls":[{"type":"function","id":"call_1","function":{"name":"lookup","arguments":"{}"}}]}`, string(wire))
	require.Equal(t, "call_1", messages[1].OfTool.ToolCallID)
	require.Equal(t, "found", messages[1].OfTool.Content.OfString.Value)
}

func TestChatCompletionInvalidContentChunks(t *testing.T) {
	for _, content := range []string{
		`[{"type":"thinking","thinking":"invalid"}]`,
		`[{"type":"text","text":42}]`,
		`[{"type":"thinking","thinking":[{"type":"text","text":42}]}]`,
		`[{"type":"thinking","thinking":[{"text":"missing type"}]}]`,
		`[{"text":"missing type"}]`,
	} {
		t.Run(content, func(t *testing.T) {
			var completion openai.ChatCompletion
			require.NoError(t, json.Unmarshal([]byte(`{"choices":[{"message":{"content":`+content+`}}]}`), &completion))
			resp := chatCompletionToLLMResponse(&completion)
			require.NotEmpty(t, resp.ErrorCode)
			require.NotEmpty(t, resp.ErrorMessage)
			require.Nil(t, resp.Content)
		})
	}
}
