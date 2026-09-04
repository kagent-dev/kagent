package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestGenaiContentsToResponsesInput(t *testing.T) {
	t.Run("user text and system instruction", func(t *testing.T) {
		input, instructions := genaiContentsToResponsesInput([]*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hello"}}},
		}, &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "be helpful"}}},
		})
		if instructions != "be helpful" {
			t.Fatalf("instructions = %q, want be helpful", instructions)
		}
		if len(input) != 1 || input[0].OfMessage == nil {
			t.Fatalf("input = %#v, want 1 message", input)
		}
		if input[0].OfMessage.Role != responses.EasyInputMessageRoleUser {
			t.Fatalf("role = %q, want user", input[0].OfMessage.Role)
		}
	})

	t.Run("function call and output paired", func(t *testing.T) {
		fc := genai.NewPartFromFunctionCall("add", map[string]any{"a": 1, "b": 2})
		fc.FunctionCall.ID = "call_1"
		fr := genai.NewPartFromFunctionResponse("add", map[string]any{"result": "3"})
		fr.FunctionResponse.ID = "call_1"

		input, _ := genaiContentsToResponsesInput([]*genai.Content{
			{Role: "model", Parts: []*genai.Part{fc}},
			{Role: "user", Parts: []*genai.Part{fr}},
		}, nil)
		if len(input) != 2 {
			t.Fatalf("len(input) = %d, want 2", len(input))
		}
		if input[0].OfFunctionCall == nil || input[0].OfFunctionCall.CallID != "call_1" {
			t.Fatalf("function_call = %#v", input[0].OfFunctionCall)
		}
		if input[1].OfFunctionCallOutput == nil || input[1].OfFunctionCallOutput.CallID != "call_1" {
			t.Fatalf("function_call_output = %#v", input[1].OfFunctionCallOutput)
		}
		if got := input[1].OfFunctionCallOutput.Output.OfString.Value; got != "3" {
			t.Fatalf("output = %q, want 3", got)
		}
	})
}

func TestGenaiToolsToResponsesTools(t *testing.T) {
	out := genaiToolsToResponsesTools([]*genai.Tool{{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:        "get_weather",
			Description: "weather",
			ParametersJsonSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
			},
		}},
	}})
	if len(out) != 1 || out[0].OfFunction == nil {
		t.Fatalf("out = %#v, want 1 function tool", out)
	}
	if out[0].OfFunction.Name != "get_weather" {
		t.Fatalf("name = %q", out[0].OfFunction.Name)
	}
	if out[0].OfFunction.Parameters["type"] != "object" {
		t.Fatalf("parameters = %#v", out[0].OfFunction.Parameters)
	}
}

func TestApplyOpenAIResponsesConfig(t *testing.T) {
	temp := 0.5
	mct := 128
	effort := "low"
	var params responses.ResponseNewParams
	applyOpenAIResponsesConfig(&params, &OpenAIConfig{
		Temperature:         &temp,
		MaxCompletionTokens: &mct,
		ReasoningEffort:     &effort,
	})
	if !params.Temperature.Valid() || params.Temperature.Value != 0.5 {
		t.Fatalf("Temperature = %#v", params.Temperature)
	}
	if !params.MaxOutputTokens.Valid() || params.MaxOutputTokens.Value != 128 {
		t.Fatalf("MaxOutputTokens = %#v", params.MaxOutputTokens)
	}
	if params.Reasoning.Effort != "low" {
		t.Fatalf("Reasoning.Effort = %q", params.Reasoning.Effort)
	}
}

func TestResponseToLLMResponse(t *testing.T) {
	raw := []byte(`{
		"id":"resp_1",
		"object":"response",
		"created_at":1,
		"status":"completed",
		"model":"gpt-4o",
		"output":[
			{
				"type":"message",
				"id":"msg_1",
				"role":"assistant",
				"status":"completed",
				"content":[{"type":"output_text","text":"hi","annotations":[]}]
			},
			{
				"type":"function_call",
				"id":"fc_1",
				"call_id":"call_1",
				"name":"add",
				"arguments":"{\"a\":1}",
				"status":"completed"
			}
		],
		"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":42},"output_tokens_details":{"reasoning_tokens":0}}
	}`)
	var resp responses.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out := responseToLLMResponse(&resp)
	if out.Content == nil || len(out.Content.Parts) != 2 {
		t.Fatalf("parts = %#v", out.Content)
	}
	if out.Content.Parts[0].Text != "hi" {
		t.Fatalf("text = %q", out.Content.Parts[0].Text)
	}
	if out.Content.Parts[1].FunctionCall == nil || out.Content.Parts[1].FunctionCall.ID != "call_1" {
		t.Fatalf("function call = %#v", out.Content.Parts[1].FunctionCall)
	}
	if out.UsageMetadata == nil || out.UsageMetadata.PromptTokenCount != 10 {
		t.Fatalf("usage = %#v", out.UsageMetadata)
	}
	if out.UsageMetadata.CachedContentTokenCount != 42 {
		t.Fatalf("cachedContentTokenCount = %d, want 42", out.UsageMetadata.CachedContentTokenCount)
	}
}

func TestOpenAIModel_GenerateContent_Responses(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-4o",
			"output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed",
				"content":[{"type":"output_text","text":"pong","annotations":[]}]}],
			"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4,"input_tokens_details":{"cached_tokens":7},"output_tokens_details":{"reasoning_tokens":0}}
		}`))
	}))
	defer srv.Close()

	client := openai.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL(srv.URL),
		option.WithHTTPClient(srv.Client()),
	)
	m := &OpenAIModel{
		Config: &OpenAIConfig{Model: "gpt-4o", APIFormat: OpenAIAPIFormatResponses},
		Client: client,
		Logger: logr.Discard(),
	}

	var got *model.LLMResponse
	for resp, err := range m.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "ping"}}}},
	}, false) {
		if err != nil {
			t.Fatalf("GenerateContent error: %v", err)
		}
		got = resp
	}
	if !strings.HasSuffix(gotPath, "/responses") {
		t.Fatalf("path = %q, want .../responses", gotPath)
	}
	if gotBody["model"] != "gpt-4o" {
		t.Fatalf("body model = %#v", gotBody["model"])
	}
	if got == nil || got.Content == nil || len(got.Content.Parts) != 1 || got.Content.Parts[0].Text != "pong" {
		t.Fatalf("response = %#v", got)
	}
	if got == nil || got.UsageMetadata == nil || got.UsageMetadata.CachedContentTokenCount != 7 {
		t.Fatalf("cachedContentTokenCount = %#v, want 7", got.UsageMetadata)
	}
}

func TestOpenAIModel_GenerateContent_ResponsesStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(payload string) {
			_, _ = io.WriteString(w, "data: "+payload+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		write(`{"type":"response.output_text.delta","content_index":0,"delta":"hel","item_id":"msg_1","output_index":0,"sequence_number":1,"logprobs":[]}`)
		write(`{"type":"response.output_text.delta","content_index":0,"delta":"lo","item_id":"msg_1","output_index":0,"sequence_number":2,"logprobs":[]}`)
		write(`{"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-4o","output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3,"input_tokens_details":{"cached_tokens":5},"output_tokens_details":{"reasoning_tokens":0}}}}`)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := openai.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL(srv.URL),
		option.WithHTTPClient(srv.Client()),
	)
	m := &OpenAIModel{
		Config: &OpenAIConfig{Model: "gpt-4o", APIFormat: OpenAIAPIFormatResponses},
		Client: client,
		Logger: logr.Discard(),
	}

	var partials []string
	var final *model.LLMResponse
	for resp, err := range m.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}},
	}, true) {
		if err != nil {
			t.Fatalf("GenerateContent error: %v", err)
		}
		if resp.Partial {
			partials = append(partials, resp.Content.Parts[0].Text)
		} else {
			final = resp
		}
	}
	if strings.Join(partials, "") != "hello" {
		t.Fatalf("partials = %#v", partials)
	}
	if final == nil || final.Content.Parts[0].Text != "hello" {
		t.Fatalf("final = %#v", final)
	}
	if final == nil || final.UsageMetadata == nil || final.UsageMetadata.CachedContentTokenCount != 5 {
		t.Fatalf("cachedContentTokenCount = %#v, want 5", final.UsageMetadata)
	}
}

func TestGenaiContentsToResponsesInput_Image(t *testing.T) {
	input, _ := genaiContentsToResponsesInput([]*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{
			{Text: "what is this?"},
			{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
		},
	}}, nil)
	if len(input) != 1 || input[0].OfMessage == nil {
		t.Fatalf("input = %#v", input)
	}
	// Content is a union; ensure image URL made it into marshaled JSON.
	b, err := json.Marshal(input[0].OfMessage)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), "input_image") || !strings.Contains(string(b), "data:image/png;base64,") {
		t.Fatalf("marshaled message missing image: %s", b)
	}
}
