package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
)

// mockLLMRoutes serves the mock LLM's model APIs from cfg.
func mockLLMRoutes(cfg mockllm.Config) *http.ServeMux {
	routes := http.NewServeMux()
	routes.HandleFunc("POST /v1/chat/completions", mockllm.NewOpenAIProvider(cfg.OpenAI).Handle)
	routes.HandleFunc("POST /v1/responses", mockllm.NewOpenAIResponseProvider(cfg.OpenAIResponse).Handle)
	routes.HandleFunc("POST /v1/embeddings", mockllm.NewOpenAIEmbeddingProvider(cfg.OpenAIEmbeddings).Handle)
	routes.HandleFunc("POST /v1/messages", anthropicToolResults(cfg.Anthropic))
	return routes
}

// anthropicToolResults serves the Messages API from mocks whose tool_result
// follow-ups match the result as well as its ID: mockllm compares a
// tool_result by tool_use_id alone, so a call Claude rejects would still
// complete the turn. A tool_result mock answers only a result for its ID that
// is not an error and contains the mock's content.
func anthropicToolResults(mocks []mockllm.AnthropicMock) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		results := lastToolResults(body)
		var admitted []mockllm.AnthropicMock
		for _, mock := range mocks {
			if toolResultMatches(mock.Match.Message, results) {
				admitted = append(admitted, mock)
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		mockllm.NewAnthropicProvider(admitted).Handle(w, r)
	}
}

// toolResult is what a tool_result block reports back to the model.
type toolResult struct {
	text    string
	isError bool
}

// toolResultMatches reports whether a mock's expected message admits the
// request's tool results: a message that expects no tool_result always does.
func toolResultMatches(expected anthropic.MessageParam, results map[string]toolResult) bool {
	if len(expected.Content) != 1 || expected.Content[0].OfToolResult == nil {
		return true
	}
	want := expected.Content[0].OfToolResult
	result, ok := results[want.ToolUseID]
	if !ok || result.isError {
		return false
	}
	for _, part := range want.Content {
		if part.OfText == nil || !strings.Contains(result.text, part.OfText.Text) {
			return false
		}
	}
	return true
}

// lastToolResults returns the tool_result blocks of a Messages API request's
// last message by tool_use_id; a block's content is a string or text blocks.
func lastToolResults(body []byte) map[string]toolResult {
	var request struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &request) != nil || len(request.Messages) == 0 {
		return nil
	}
	var blocks []struct {
		Type      string          `json:"type"`
		ToolUseID string          `json:"tool_use_id"`
		IsError   bool            `json:"is_error"`
		Content   json.RawMessage `json:"content"`
	}
	if json.Unmarshal(request.Messages[len(request.Messages)-1].Content, &blocks) != nil {
		return nil
	}
	results := map[string]toolResult{}
	for _, block := range blocks {
		if block.Type == "tool_result" {
			results[block.ToolUseID] = toolResult{text: contentText(block.Content), isError: block.IsError}
		}
	}
	return results
}

func contentText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(content, &parts)
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		texts = append(texts, part.Text)
	}
	return strings.Join(texts, "\n")
}

// rejectionReplies are the follow-up replies no e2e test accepts: a rejected
// tool call answered with one of them still fails its test.
var rejectionReplies = []string{"FETCH_FAILED"}

// A tool call the runtime rejects must not complete its turn: every Claude
// and chat-completions follow-up mock matches the call's output, which a
// rejection never carries, rather than the call's ID alone.
func TestToolCallMocksRefuseARejectedCall(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("mocks/*.json")
	require.NoError(t, err)
	for _, file := range files {
		raw, err := os.ReadFile(file)
		require.NoError(t, err)
		var cfg mockllm.Config
		require.NoError(t, json.Unmarshal(raw, &cfg), file)
		server := httptest.NewServer(mockLLMRoutes(cfg))
		t.Cleanup(server.Close)
		for _, mock := range cfg.OpenAI {
			for _, choice := range mock.Response.Choices {
				for _, call := range choice.Message.ToolCalls {
					for _, rejection := range goADKRejections(call.Function.Name, call.Function.Arguments) {
						t.Run(filepath.Base(file)+"/"+call.ID, func(t *testing.T) {
							body := map[string]any{"model": "gpt-4.1-mini", "messages": []map[string]any{
								{"role": "user", "content": "Run the tool."},
								{"role": "tool", "tool_call_id": call.ID, "content": rejection},
							}}
							requireRejected(t, server.URL+"/v1/chat/completions", nil, body)
						})
					}
				}
			}
		}
		for _, mock := range cfg.Anthropic {
			for _, block := range mock.Response.Content {
				if block.Type != "tool_use" {
					continue
				}
				t.Run(filepath.Base(file)+"/"+block.ID, func(t *testing.T) {
					body := map[string]any{"model": "claude-sonnet-4-5", "max_tokens": 100, "messages": []map[string]any{
						{"role": "user", "content": []map[string]any{{"type": "text", "text": "Run the tool."}}},
						{"role": "assistant", "content": []map[string]any{{"type": "tool_use", "id": block.ID, "name": block.Name, "input": map[string]any{}}}},
						{"role": "user", "content": []map[string]any{{
							"type": "tool_result", "tool_use_id": block.ID, "is_error": true,
							"content": "<tool_use_error>Error: No such tool available: " + block.Name + "</tool_use_error>",
						}}},
					}}
					requireRejected(t, server.URL+"/v1/messages", http.Header{"X-Api-Key": {"mock-key"}, "Anthropic-Version": {"2023-06-01"}}, body)
				})
			}
		}
	}
}

// A Claude tool_result mock answers the call's output, whether Claude sends it
// as a string or as text blocks.
func TestAnthropicToolResultMockAnswersTheOutput(t *testing.T) {
	t.Parallel()
	cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_claude_builtin_tools.json", claudeInteractionMocks)
	require.NoError(t, err)
	server := httptest.NewServer(mockLLMRoutes(cfg))
	t.Cleanup(server.Close)
	for name, test := range map[string]struct {
		id      string
		content any
		reply   string
	}{
		"string": {id: "toolu_builtin_bash", content: "CLAUDE_TOOL_WRITTEN\n", reply: "toolu_builtin_read"},
		"blocks": {id: "toolu_builtin_read", content: []map[string]any{{"type": "text", "text": "     1\tCLAUDE_TOOL_FILE"}}, reply: "CLAUDE_BUILTIN_TOOLS_DONE"},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"model": "claude-sonnet-4-5", "max_tokens": 100, "messages": []map[string]any{
				{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": test.id, "content": test.content}}},
			}})
			require.NoError(t, err)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/v1/messages", bytes.NewReader(body))
			require.NoError(t, err)
			request.Header = http.Header{"Content-Type": {"application/json"}, "X-Api-Key": {"mock-key"}, "Anthropic-Version": {"2023-06-01"}}
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			reply, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.StatusCode, "%s", reply)
			require.Contains(t, string(reply), test.reply)
		})
	}
}

// goADKRejections are the tool messages the Go ADK sends for a call it does
// not run: the unknown tool's error, and for a shell call the bash tool's
// error, which quotes the command.
func goADKRejections(name, arguments string) []string {
	rejections := []string{`{"error":"tool '` + name + `' not found."}`}
	var shell struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(arguments), &shell) == nil && shell.Command != "" {
		rejections = append(rejections, "Error executing command "+strconv.Quote(shell.Command)+": exit status 1")
	}
	return rejections
}

func requireRejected(t *testing.T, url string, headers http.Header, body any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(encoded))
	require.NoError(t, err)
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = http.Header{}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	reply, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	if response.StatusCode == http.StatusNotFound {
		return
	}
	for _, rejection := range rejectionReplies {
		if strings.Contains(string(reply), rejection) {
			return
		}
	}
	t.Fatalf("a rejected tool call was answered (%d): %s", response.StatusCode, reply)
}
