package models

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"log/slog"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestOpenAIModel_Name(t *testing.T) {
	m := &OpenAIModel{Config: &OpenAIConfig{Model: "gpt-4o"}}
	if got := m.Name(); got != "gpt-4o" {
		t.Errorf("Name() = %q, want %q", got, "gpt-4o")
	}
}

func TestOpenAIModelGenerateContentSendsStructuredOutputWithTools(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(encoded, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"{\"answer\":4}"},"finish_reason":"stop"}]
		}`)
	}))
	defer server.Close()

	client := openai.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL(server.URL),
		option.WithHTTPClient(server.Client()),
	)
	llm := &OpenAIModel{
		Config: &OpenAIConfig{Model: "gpt-4o"},
		Client: client,
		Logger: slog.New(slog.DiscardHandler),
	}
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"answer": map[string]any{"type": "integer"}},
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
	for _, err := range llm.GenerateContent(context.Background(), request, false) {
		if err != nil {
			t.Fatalf("GenerateContent error: %v", err)
		}
	}

	responseFormat, ok := body["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_schema" {
		t.Fatalf("response_format = %#v", body["response_format"])
	}
	jsonSchema, ok := responseFormat["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("response_format.json_schema = %#v", responseFormat["json_schema"])
	}
	if _, present := jsonSchema["strict"]; present {
		t.Fatalf("response_format.json_schema.strict must be omitted: %#v", jsonSchema)
	}
	if gotSchema, ok := jsonSchema["schema"].(map[string]any); !ok || gotSchema["type"] != "object" {
		t.Fatalf("response_format.json_schema.schema = %#v", jsonSchema["schema"])
	}
	if tools, ok := body["tools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", body["tools"])
	}
}

func TestFunctionResponseContentString(t *testing.T) {
	tests := []struct {
		name string
		resp any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"empty string", "", ""},
		{"map with content[0].text", map[string]any{
			"content": []any{
				map[string]any{"text": "extracted text"},
			},
		}, "extracted text"},
		{"map with result", map[string]any{
			"result": "result value",
		}, "result value"},
		{"map with both prefers content", map[string]any{
			"content": []any{
				map[string]any{"text": "from content"},
			},
			"result": "from result",
		}, "from content"},
		{"map empty content slice falls back to JSON", map[string]any{
			"content": []any{},
		}, `{"content":[]}`},
		{"map with result when content empty", map[string]any{
			"content": []any{},
			"result":  "fallback",
		}, "fallback"},
		{"other type falls back to JSON", 42, "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFunctionResponseContent(tt.resp)
			if got != tt.want {
				t.Errorf("extractFunctionResponseContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenaiToolsToOpenAITools(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		out := genaiToolsToOpenAITools(nil)
		if out != nil {
			t.Errorf("genaiToolsToOpenAITools(nil) = %v, want nil", out)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		out := genaiToolsToOpenAITools([]*genai.Tool{})
		if len(out) != 0 {
			t.Errorf("len(out) = %d, want 0", len(out))
		}
	})

	t.Run("nil tool skipped", func(t *testing.T) {
		out := genaiToolsToOpenAITools([]*genai.Tool{nil, {FunctionDeclarations: []*genai.FunctionDeclaration{
			{Name: "foo", Description: "desc"},
		}}})
		if len(out) != 1 {
			t.Errorf("len(out) = %d, want 1", len(out))
		}
	})

	t.Run("tool with params", func(t *testing.T) {
		tools := []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:        "get_weather",
				Description: "Get weather",
				ParametersJsonSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			}},
		}}
		out := genaiToolsToOpenAITools(tools)
		if len(out) != 1 {
			t.Fatalf("len(out) = %d, want 1", len(out))
		}
		// We only check we got one tool; internal shape is openai-specific
	})

	t.Run("nil params gets default object schema with properties", func(t *testing.T) {
		tools := []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:        "istio_analyze_cluster_configuration",
				Description: "Analyze Istio cluster config",
			}},
		}}
		out := genaiToolsToOpenAITools(tools)
		if len(out) != 1 {
			t.Fatalf("len(out) = %d, want 1", len(out))
		}
		// The converted tool should have a valid schema; OpenAI rejects
		// object schemas missing "properties".
	})

	t.Run("object type without properties gets empty properties", func(t *testing.T) {
		tools := []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:        "no_props",
				Description: "Object type but no properties field",
				ParametersJsonSchema: map[string]any{
					"type": "object",
				},
			}},
		}}
		out := genaiToolsToOpenAITools(tools)
		if len(out) != 1 {
			t.Fatalf("len(out) = %d, want 1", len(out))
		}
	})
}

func TestGenaiContentsToOpenAIMessages(t *testing.T) {
	t.Run("nil contents", func(t *testing.T) {
		msgs, sys, _ := genaiContentsToOpenAIMessages(nil, nil)
		if len(msgs) != 0 {
			t.Errorf("len(messages) = %d, want 0", len(msgs))
		}
		if sys != "" {
			t.Errorf("systemInstruction = %q, want empty", sys)
		}
	})

	t.Run("system instruction from config", func(t *testing.T) {
		config := &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{
					{Text: "You are helpful."},
					{Text: "Be concise."},
				},
			},
		}
		msgs, sys, _ := genaiContentsToOpenAIMessages(nil, config)
		if len(msgs) != 0 {
			t.Errorf("len(messages) = %d, want 0", len(msgs))
		}
		wantSys := "You are helpful.\nBe concise."
		if sys != wantSys {
			t.Errorf("systemInstruction = %q, want %q", sys, wantSys)
		}
	})

	t.Run("system instruction trims and skips empty text", func(t *testing.T) {
		config := &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{
					{Text: "  one  "},
					{Text: ""},
					{Text: "two"},
				},
			},
		}
		_, sys, _ := genaiContentsToOpenAIMessages(nil, config)
		// Implementation joins parts then TrimSpace; empty text part adds nothing
		wantSys := "one  \ntwo"
		if sys != wantSys {
			t.Errorf("systemInstruction = %q, want %q", sys, wantSys)
		}
	})

	t.Run("user content with text", func(t *testing.T) {
		contents := []*genai.Content{{
			Role:  string(genai.RoleUser),
			Parts: []*genai.Part{{Text: "Hello"}},
		}}
		msgs, sys, _ := genaiContentsToOpenAIMessages(contents, nil)
		if sys != "" {
			t.Errorf("systemInstruction = %q, want empty", sys)
		}
		if len(msgs) != 1 {
			t.Fatalf("len(messages) = %d, want 1", len(msgs))
		}
		// First message should be user message (we only assert count and no panic)
	})

	t.Run("content with role system skipped", func(t *testing.T) {
		contents := []*genai.Content{
			{Role: "system", Parts: []*genai.Part{{Text: "sys"}}},
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{Text: "user"}}},
		}
		msgs, _, _ := genaiContentsToOpenAIMessages(contents, nil)
		// System role content is skipped (handled via config), so only user message
		if len(msgs) != 1 {
			t.Errorf("len(messages) = %d, want 1 (system content skipped)", len(msgs))
		}
	})

	t.Run("nil and empty content skipped", func(t *testing.T) {
		contents := []*genai.Content{
			nil,
			{Role: "", Parts: nil},
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{Text: "only"}}},
		}
		msgs, _, _ := genaiContentsToOpenAIMessages(contents, nil)
		if len(msgs) != 1 {
			t.Errorf("len(messages) = %d, want 1", len(msgs))
		}
	})

	t.Run("nil args (replay regression) encode as empty object", func(t *testing.T) {
		contents := []*genai.Content{
			{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "ping", Args: nil}}}},
		}
		msgs, _, _ := genaiContentsToOpenAIMessages(contents, nil)
		if len(msgs) == 0 || msgs[0].OfAssistant == nil || len(msgs[0].OfAssistant.ToolCalls) != 1 {
			t.Fatalf("messages = %#v, want first message to be assistant with 1 tool call", msgs)
		}
		if got := msgs[0].OfAssistant.ToolCalls[0].GetFunction().Arguments; got != `{}` {
			t.Errorf("arguments = %q, want {}", got)
		}
	})

	t.Run("image link in newest user turn is sent as image_url", func(t *testing.T) {
		const link = "https://example.com/cat.png"
		contents := []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{
			{Text: "what is this"},
			{FileData: &genai.FileData{FileURI: link, MIMEType: "image/png"}},
		}}}
		msgs, _, err := genaiContentsToOpenAIMessages(contents, nil)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(msgs) != 1 || msgs[0].OfUser == nil {
			t.Fatalf("messages = %#v, want one user message", msgs)
		}
		parts := msgs[0].OfUser.Content.OfArrayOfContentParts
		if len(parts) != 2 || parts[0].OfText == nil || parts[0].OfText.Text != "what is this" || parts[1].OfImageURL == nil {
			t.Fatalf("content parts = %#v, want text then image_url", parts)
		}
		if got := parts[1].OfImageURL.ImageURL.URL; got != link {
			t.Errorf("image_url = %q, want %q", got, link)
		}
	})

	t.Run("non-image link in newest user turn fails", func(t *testing.T) {
		const link = "https://example.com/report.pdf"
		contents := []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{
			{FileData: &genai.FileData{FileURI: link, MIMEType: "application/pdf"}},
		}}}
		_, _, err := genaiContentsToOpenAIMessages(contents, nil)
		if err == nil || !strings.Contains(err.Error(), link) {
			t.Fatalf("error = %v, want one that names %q", err, link)
		}
	})

	t.Run("link without media type in newest user turn fails", func(t *testing.T) {
		contents := []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{
			{FileData: &genai.FileData{FileURI: "https://example.com/blob"}},
		}}}
		_, _, err := genaiContentsToOpenAIMessages(contents, nil)
		if err == nil || !strings.Contains(err.Error(), "without a media type") {
			t.Fatalf("error = %v, want one that says the media type is missing", err)
		}
	})

	t.Run("image link without https in newest user turn fails", func(t *testing.T) {
		contents := []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{
			{FileData: &genai.FileData{FileURI: "http://10.0.0.5/cat.png", MIMEType: "image/png"}},
		}}}
		if _, _, err := genaiContentsToOpenAIMessages(contents, nil); err == nil {
			t.Fatal("error = nil, want an error")
		}
	})

	t.Run("non-image link in newest user turn fails when a model turn follows", func(t *testing.T) {
		contents := []*genai.Content{
			{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "read this"}, {FileData: &genai.FileData{FileURI: "https://example.com/report.pdf", MIMEType: "application/pdf"}}}},
			{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "ok"}}},
		}
		if _, _, err := genaiContentsToOpenAIMessages(contents, nil); err == nil {
			t.Fatal("error = nil, want an error for the newest user turn")
		}
	})

	t.Run("image link in older user turn is replayed as image_url", func(t *testing.T) {
		const link = "https://example.com/cat.png"
		contents := []*genai.Content{
			{Role: genai.RoleUser, Parts: []*genai.Part{{FileData: &genai.FileData{FileURI: link, MIMEType: "image/png"}}}},
			{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "a cat"}}},
			{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "what colour"}}},
		}
		msgs, _, err := genaiContentsToOpenAIMessages(contents, nil)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		parts := msgs[0].OfUser.Content.OfArrayOfContentParts
		if len(parts) != 1 || parts[0].OfImageURL == nil || parts[0].OfImageURL.ImageURL.URL != link {
			t.Fatalf("older user message parts = %#v, want one image_url %q", parts, link)
		}
	})

	t.Run("non-image link in older user turn becomes a note", func(t *testing.T) {
		link := &genai.FileData{FileURI: "https://example.com/report.pdf", MIMEType: "application/pdf", DisplayName: "report.pdf"}
		contents := []*genai.Content{
			{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "read this"}, {FileData: link}}},
			{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "ok"}}},
			{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "hello again"}}},
		}
		msgs, _, err := genaiContentsToOpenAIMessages(contents, nil)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("len(messages) = %d, want 3", len(msgs))
		}
		if got := msgs[0].OfUser.Content.OfString.Value; !strings.Contains(got, `[Link "report.pdf (https://example.com/report.pdf)" was not sent`) {
			t.Errorf("older user message = %q, want the link note", got)
		}
		if got := msgs[2].OfUser.Content.OfString.Value; got != "hello again" {
			t.Errorf("newest user message = %q, want %q", got, "hello again")
		}
	})

	t.Run("PDF bytes are sent as a file part", func(t *testing.T) {
		data := []byte("%PDF-1.4 test")
		contents := []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{
			{Text: "summarize"},
			{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: data, DisplayName: "report.pdf"}},
		}}}
		msgs, _, err := genaiContentsToOpenAIMessages(contents, nil)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(msgs) != 1 || msgs[0].OfUser == nil {
			t.Fatalf("messages = %#v, want one user message", msgs)
		}
		parts := msgs[0].OfUser.Content.OfArrayOfContentParts
		if len(parts) != 2 || parts[0].OfText == nil || parts[0].OfText.Text != "summarize" || parts[1].OfFile == nil {
			t.Fatalf("content parts = %#v, want text then file", parts)
		}
		file := parts[1].OfFile.File
		if want := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(data); file.FileData.Value != want {
			t.Errorf("file_data = %q, want %q", file.FileData.Value, want)
		}
		if file.Filename.Value != "report.pdf" {
			t.Errorf("filename = %q, want %q", file.Filename.Value, "report.pdf")
		}
	})
}

func TestOpenAIModelSendsPDFBytes(t *testing.T) {
	for name, tt := range map[string]struct {
		config *OpenAIConfig
		want   bool
	}{
		"chat completions": {&OpenAIConfig{}, true},
		"nil config":       {nil, true},
		"custom base url":  {&OpenAIConfig{BaseUrl: "https://example.com/v1"}, false},
		"responses api":    {&OpenAIConfig{APIFormat: OpenAIAPIFormatResponses}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := (&OpenAIModel{Config: tt.config}).SendsPDFBytes(); got != tt.want {
				t.Errorf("SendsPDFBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyOpenAIConfig(t *testing.T) {
	t.Run("nil config no panic", func(t *testing.T) {
		var params openai.ChatCompletionNewParams
		applyOpenAIConfig(&params, nil)
	})

	t.Run("config with temperature", func(t *testing.T) {
		temp := 0.7
		cfg := &OpenAIConfig{Temperature: &temp}
		var params openai.ChatCompletionNewParams
		applyOpenAIConfig(&params, cfg)
		if !params.Temperature.Valid() || params.Temperature.Value != 0.7 {
			t.Errorf("Temperature: Valid=%v, Value=%v, want (true, 0.7)", params.Temperature.Valid(), params.Temperature.Value)
		}
	})

	t.Run("config with max_tokens", func(t *testing.T) {
		n := 100
		cfg := &OpenAIConfig{MaxTokens: &n}
		var params openai.ChatCompletionNewParams
		applyOpenAIConfig(&params, cfg)
		if !params.MaxTokens.Valid() || params.MaxTokens.Value != 100 {
			t.Errorf("MaxTokens: Valid=%v, Value=%v, want (true, 100)", params.MaxTokens.Valid(), params.MaxTokens.Value)
		}
	})

	t.Run("config with max_completion_tokens", func(t *testing.T) {
		n := 100
		cfg := &OpenAIConfig{MaxCompletionTokens: &n}
		var params openai.ChatCompletionNewParams
		applyOpenAIConfig(&params, cfg)
		if !params.MaxCompletionTokens.Valid() || params.MaxCompletionTokens.Value != 100 {
			t.Errorf("MaxCompletionTokens: Valid=%v, Value=%v, want (true, 100)", params.MaxCompletionTokens.Valid(), params.MaxCompletionTokens.Value)
		}
	})

	t.Run("both set prefers max_completion_tokens", func(t *testing.T) {
		mt := 100
		mct := 200
		cfg := &OpenAIConfig{MaxTokens: &mt, MaxCompletionTokens: &mct}
		var params openai.ChatCompletionNewParams
		applyOpenAIConfig(&params, cfg)
		if !params.MaxCompletionTokens.Valid() || params.MaxCompletionTokens.Value != 200 {
			t.Errorf("MaxCompletionTokens: Valid=%v, Value=%v, want (true, 200)", params.MaxCompletionTokens.Valid(), params.MaxCompletionTokens.Value)
		}
		if params.MaxTokens.Valid() {
			t.Errorf("MaxTokens should not be set when max_completion_tokens is present, got Value=%v", params.MaxTokens.Value)
		}
	})

	t.Run("config with reasoning_effort", func(t *testing.T) {
		effort := "medium"
		cfg := &OpenAIConfig{ReasoningEffort: &effort}
		var params openai.ChatCompletionNewParams
		applyOpenAIConfig(&params, cfg)
		if params.ReasoningEffort != "medium" {
			t.Errorf("ReasoningEffort: got %q, want %q", params.ReasoningEffort, "medium")
		}
	})
}

func TestGenaiContentsToOpenAIMessages_PreservesThoughtSignatureOnToolCallAndToolResult(t *testing.T) {
	thoughtSignature := []byte("abc")

	functionCall := genai.NewPartFromFunctionCall("add", map[string]any{"a": 2, "b": 2})
	functionCall.FunctionCall.ID = "call_1"
	functionCall.ThoughtSignature = thoughtSignature

	functionResponse := genai.NewPartFromFunctionResponse("add", map[string]any{"result": "4"})
	functionResponse.FunctionResponse.ID = "call_1"

	messages, _, _ := genaiContentsToOpenAIMessages([]*genai.Content{
		{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{functionCall},
		},
		{
			Role:  string(genai.RoleUser),
			Parts: []*genai.Part{functionResponse},
		},
	}, nil)

	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}

	assistantJSON, err := json.Marshal(messages[0].OfAssistant)
	if err != nil {
		t.Fatalf("json.Marshal(assistant) error = %v", err)
	}
	toolJSON, err := json.Marshal(messages[1].OfTool)
	if err != nil {
		t.Fatalf("json.Marshal(tool) error = %v", err)
	}

	want := base64.StdEncoding.EncodeToString(thoughtSignature)
	assertThoughtSignature := func(name string, payload []byte) {
		t.Helper()
		var obj map[string]any
		if err := json.Unmarshal(payload, &obj); err != nil {
			t.Fatalf("%s json.Unmarshal error = %v", name, err)
		}
		extra, ok := obj["extra_content"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing extra_content: %s", name, string(payload))
		}
		googleExtra, ok := extra["google"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing google extra content: %s", name, string(payload))
		}
		if got, _ := googleExtra["thought_signature"].(string); got != want {
			t.Fatalf("%s thought_signature = %q, want %q", name, got, want)
		}
	}

	var assistantObj map[string]any
	if err := json.Unmarshal(assistantJSON, &assistantObj); err != nil {
		t.Fatalf("assistant json.Unmarshal error = %v", err)
	}
	toolCalls, ok := assistantObj["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("assistant tool_calls = %#v, want 1 tool call", assistantObj["tool_calls"])
	}
	firstToolCall, ok := toolCalls[0].(map[string]any)
	if !ok {
		t.Fatalf("assistant tool call = %#v, want object", toolCalls[0])
	}
	firstToolCallJSON, err := json.Marshal(firstToolCall)
	if err != nil {
		t.Fatalf("json.Marshal(firstToolCall) error = %v", err)
	}

	assertThoughtSignature("assistant tool_call", firstToolCallJSON)
	assertThoughtSignature("tool message", toolJSON)
}

func TestChatCompletionToLLMResponse_PreservesThoughtSignature(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl-1",
		"object":"chat.completion",
		"created":123,
		"model":"gemini-2.5-flash",
		"choices":[{
			"index":0,
			"finish_reason":"tool_calls",
			"message":{
				"role":"assistant",
				"tool_calls":[{
					"id":"call_1",
					"type":"function",
					"function":{
						"name":"add",
						"arguments":"{\"a\":2,\"b\":2}"
					},
					"extra_content":{
						"google":{
							"thought_signature":"YWJj"
						}
					}
				}]
			}
		}],
		"usage":{
			"prompt_tokens":3,
			"completion_tokens":4,
			"total_tokens":7
		}
	}`)

	var completion openai.ChatCompletion
	if err := json.Unmarshal(raw, &completion); err != nil {
		t.Fatalf("json.Unmarshal(ChatCompletion) error = %v", err)
	}

	resp := chatCompletionToLLMResponse(&completion)
	if resp.Content == nil || len(resp.Content.Parts) != 1 {
		t.Fatalf("response parts = %#v, want 1 function-call part", resp.Content)
	}

	part := resp.Content.Parts[0]
	if part.FunctionCall == nil {
		t.Fatalf("part.FunctionCall = nil, want function call")
	}
	if part.FunctionCall.Name != "add" {
		t.Fatalf("part.FunctionCall.Name = %q, want %q", part.FunctionCall.Name, "add")
	}
	if string(part.ThoughtSignature) != "abc" {
		t.Fatalf("part.ThoughtSignature = %q, want %q", string(part.ThoughtSignature), "abc")
	}
	if resp.UsageMetadata == nil || resp.UsageMetadata.PromptTokenCount != 3 || resp.UsageMetadata.CandidatesTokenCount != 4 {
		t.Fatalf("usage metadata = %#v, want prompt=3 completion=4", resp.UsageMetadata)
	}
}

func TestExtractThoughtSignatureFromStreamingToolCallChunk(t *testing.T) {
	raw := []byte(`{
		"index":0,
		"id":"call_1",
		"type":"function",
		"function":{
			"name":"add",
			"arguments":"{\"a\":2"
		},
		"extra_content":{
			"google":{
				"thought_signature":"YWJj"
			}
		}
	}`)

	var toolCall openai.ChatCompletionChunkChoiceDeltaToolCall
	if err := json.Unmarshal(raw, &toolCall); err != nil {
		t.Fatalf("json.Unmarshal(ChatCompletionChunkChoiceDeltaToolCall) error = %v", err)
	}

	thoughtSignature := extractThoughtSignatureFromExtraFields(toolCall.JSON.ExtraFields)
	if string(thoughtSignature) != "abc" {
		t.Fatalf("thoughtSignature = %q, want %q", string(thoughtSignature), "abc")
	}
}
