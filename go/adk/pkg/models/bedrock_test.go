package models

import (
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/genai"
)

// testLogger returns a no-op logger for tests.

func TestBedrockStopReasonToGenai(t *testing.T) {
	tests := []struct {
		name     string
		reason   types.StopReason
		expected genai.FinishReason
	}{
		{name: "max tokens", reason: types.StopReasonMaxTokens, expected: genai.FinishReasonMaxTokens},
		{name: "end turn", reason: types.StopReasonEndTurn, expected: genai.FinishReasonStop},
		{name: "stop sequence", reason: types.StopReasonStopSequence, expected: genai.FinishReasonStop},
		{name: "tool use", reason: types.StopReasonToolUse, expected: genai.FinishReasonStop},
		{name: "unknown", reason: types.StopReason("unknown"), expected: genai.FinishReasonStop},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bedrockStopReasonToGenai(tt.reason); got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestConvertGenaiContentsToBedrockMessages(t *testing.T) {
	tests := []struct {
		name           string
		contents       []*genai.Content
		wantMsgCount   int
		wantSystemText string
		checkMsg       func(t *testing.T, msgs []types.Message)
	}{
		{
			name: "simple user message",
			contents: []*genai.Content{
				{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
			},
			wantMsgCount: 1,
		},
		{
			name: "system instruction extracted",
			contents: []*genai.Content{
				{Role: "system", Parts: []*genai.Part{{Text: "You are a helpful assistant"}}},
				{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
			},
			wantMsgCount:   1,
			wantSystemText: "You are a helpful assistant",
		},
		{
			name: "user and model conversation",
			contents: []*genai.Content{
				{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
				{Role: "model", Parts: []*genai.Part{{Text: "Hi there"}}},
			},
			wantMsgCount: 2,
		},
		{
			name: "FunctionCall in model-role becomes assistant message",
			contents: []*genai.Content{
				{
					Role: "model",
					Parts: []*genai.Part{
						{Text: "I'll call the tool"},
						{FunctionCall: &genai.FunctionCall{ID: "call_456", Name: "k8s_get_resources", Args: map[string]any{"resource": "pods"}}},
					},
				},
			},
			wantMsgCount: 1,
			checkMsg: func(t *testing.T, msgs []types.Message) {
				if msgs[0].Role != types.ConversationRoleAssistant {
					t.Errorf("expected assistant role, got %s", msgs[0].Role)
				}
				if len(msgs[0].Content) != 2 {
					t.Errorf("expected 2 content blocks (text + toolUse), got %d", len(msgs[0].Content))
				}
			},
		},
		{
			name: "FunctionResponse in user-role becomes user message",
			contents: []*genai.Content{
				{
					Role: "user",
					Parts: []*genai.Part{
						{FunctionResponse: &genai.FunctionResponse{ID: "call_456", Name: "k8s_get_resources", Response: map[string]any{"result": "pod1"}}},
					},
				},
			},
			wantMsgCount: 1,
			checkMsg: func(t *testing.T, msgs []types.Message) {
				if msgs[0].Role != types.ConversationRoleUser {
					t.Errorf("expected user role, got %s", msgs[0].Role)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, systemText := convertGenaiContentsToBedrockMessages(tt.contents, nil)
			if len(msgs) != tt.wantMsgCount {
				t.Errorf("expected %d messages, got %d", tt.wantMsgCount, len(msgs))
			}
			if systemText != tt.wantSystemText {
				t.Errorf("expected system text %q, got %q", tt.wantSystemText, systemText)
			}
			if tt.checkMsg != nil {
				tt.checkMsg(t, msgs)
			}
		})
	}
}

// TestConvertGenaiToolsToBedrock verifies schema conversion for all three tool
// sources: genai.Schema (declaration-based), map[string]any (MCP), and
// *jsonschema.Schema (functiontool.New).
func TestConvertGenaiToolsToBedrock(t *testing.T) {
	extractSchema := func(t *testing.T, tools []types.Tool, _ map[string]string) map[string]any {
		t.Helper()
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}
		tm, ok := tools[0].(*types.ToolMemberToolSpec)
		if !ok {
			t.Fatal("expected *types.ToolMemberToolSpec")
		}
		sm, ok := tm.Value.InputSchema.(*types.ToolInputSchemaMemberJson)
		if !ok {
			t.Fatal("expected *types.ToolInputSchemaMemberJson")
		}
		b, err := sm.Value.MarshalSmithyDocument()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var schema map[string]any
		if err := json.Unmarshal(b, &schema); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return schema
	}

	t.Run("genai.Schema types are lowercased", func(t *testing.T) {
		tools := []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name: "get_weather",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"location": {Type: genai.TypeString},
					"count":    {Type: genai.TypeInteger},
					"detailed": {Type: genai.TypeBoolean},
				},
				Required: []string{"location"},
			},
		}}}}

		bt1, nm1 := convertGenaiToolsToBedrock(tools)
		schema := extractSchema(t, bt1, nm1)

		props := schema["properties"].(map[string]any)
		for prop, want := range map[string]string{"location": "string", "count": "integer", "detailed": "boolean"} {
			got, _ := props[prop].(map[string]any)["type"].(string)
			if got != want {
				t.Errorf("property %q: want type %q, got %q", prop, want, got)
			}
		}
		required, _ := schema["required"].([]any)
		if len(required) != 1 || required[0] != "location" {
			t.Errorf("expected required=[location], got %v", required)
		}
	})

	t.Run("MCP map[string]any schema passes through", func(t *testing.T) {
		tools := []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name: "k8s_get_resources",
			ParametersJsonSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_type": map[string]any{"type": "string"},
				},
				"required": []any{"resource_type"},
			},
		}}}}

		bt2, nm2 := convertGenaiToolsToBedrock(tools)
		schema := extractSchema(t, bt2, nm2)
		props, ok := schema["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			t.Fatalf("expected non-empty properties, got %v", schema["properties"])
		}
		if props["resource_type"].(map[string]any)["type"] != "string" {
			t.Errorf("expected resource_type.type=string")
		}
	})

	t.Run("*jsonschema.Schema from functiontool.New", func(t *testing.T) {
		s := &jsonschema.Schema{Type: "object", Required: []string{"questions"}}
		s.Properties = map[string]*jsonschema.Schema{
			"questions": {Type: "array", Description: "List of questions to ask"},
		}
		tools := []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:                 "ask_user",
			ParametersJsonSchema: s,
		}}}}

		bt3, nm3 := convertGenaiToolsToBedrock(tools)
		schema := extractSchema(t, bt3, nm3)
		props, ok := schema["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			t.Fatalf("expected non-empty properties (means *jsonschema.Schema was not converted): %v", schema["properties"])
		}
		if _, ok := props["questions"]; !ok {
			t.Fatal("expected 'questions' in properties")
		}
	})
}

func TestExtractFunctionResponseContent(t *testing.T) {
	tests := []struct {
		name     string
		response any
		expected string
	}{
		{name: "nil", response: nil, expected: ""},
		{name: "string", response: "success", expected: "success"},
		{name: "map with result", response: map[string]any{"result": "success"}, expected: "success"},
		{name: "MCP content array", response: map[string]any{"content": []any{map[string]any{"text": "hello"}, map[string]any{"text": "world"}}}, expected: "hello\nworld"},
		{name: "fallback to JSON", response: 123, expected: "123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractFunctionResponseContent(tt.response); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestParametersJsonSchemaToMap(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		if parametersJsonSchemaToMap(nil) != nil {
			t.Error("expected nil")
		}
	})
	t.Run("map[string]any passes through", func(t *testing.T) {
		input := map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}}
		result := parametersJsonSchemaToMap(input)
		if _, ok := result["properties"].(map[string]any)["query"]; !ok {
			t.Error("expected 'query' in properties")
		}
	})
	t.Run("*jsonschema.Schema round-trips via JSON", func(t *testing.T) {
		s := &jsonschema.Schema{Type: "object", Required: []string{"content"}}
		s.Properties = map[string]*jsonschema.Schema{"content": {Type: "string"}}
		result := parametersJsonSchemaToMap(s)
		if result == nil {
			t.Fatal("expected non-nil")
		}
		if result["type"] != "object" {
			t.Errorf("expected type=object, got %v", result["type"])
		}
		if result["properties"].(map[string]any)["content"].(map[string]any)["type"] != "string" {
			t.Error("expected content.type=string")
		}
	})
}

func TestSanitizeBedrockToolID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "valid ID unchanged", id: "call_123", want: "call_123"},
		{name: "valid ID with dots and colons", id: "tool.v1:run-1", want: "tool.v1:run-1"},
		{name: "empty ID gets generated", id: "", want: "tool_1"},
		{name: "invalid chars replaced", id: "call/foo@bar", want: "call_foo_bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idMap := make(map[string]string)
			counter := 0
			if got := sanitizeBedrockToolID(tt.id, idMap, &counter); got != tt.want {
				t.Errorf("sanitizeBedrockToolID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}

	t.Run("multiple empty IDs get unique sanitized IDs", func(t *testing.T) {
		idMap, counter := make(map[string]string), 0
		first := sanitizeBedrockToolID("", idMap, &counter)
		second := sanitizeBedrockToolID("", idMap, &counter)
		if first == second {
			t.Errorf("expected different IDs for repeated empty input, both got %q", first)
		}
	})

	t.Run("different invalid IDs get different sanitized IDs", func(t *testing.T) {
		idMap, counter := make(map[string]string), 0
		first := sanitizeBedrockToolID("", idMap, &counter)
		second := sanitizeBedrockToolID("///", idMap, &counter)
		if first == second {
			t.Errorf("expected different IDs for different invalid inputs, both got %q", first)
		}
	})
}

func TestSanitizeBedrockToolName(t *testing.T) {
	tests := []struct {
		name string
		tool string
		want string
	}{
		{name: "valid name unchanged", tool: "get_weather", want: "get_weather"},
		{name: "valid name with hyphen", tool: "fetch-data", want: "fetch-data"},
		{name: "dot replaced", tool: "fetch.get_url", want: "fetch_get_url"},
		{name: "colon replaced", tool: "filesystem:read_file", want: "filesystem_read_file"},
		{name: "space replaced", tool: "my tool", want: "my_tool"},
		{name: "multiple invalid chars", tool: "a.b:c d", want: "a_b_c_d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nameMap := make(map[string]string)
			counter := 0
			if got := sanitizeBedrockToolName(tt.tool, nameMap, &counter); got != tt.want {
				t.Errorf("sanitizeBedrockToolName(%q) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}

	t.Run("empty name gets synthetic", func(t *testing.T) {
		nameMap, counter := make(map[string]string), 0
		got := sanitizeBedrockToolName("", nameMap, &counter)
		if got != "tool_fn_1" {
			t.Errorf("expected tool_fn_1, got %q", got)
		}
		if counter != 1 {
			t.Errorf("expected counter=1, got %d", counter)
		}
	})

	t.Run("caching returns same sanitized name", func(t *testing.T) {
		nameMap, counter := make(map[string]string), 0
		first := sanitizeBedrockToolName("fetch.get_url", nameMap, &counter)
		second := sanitizeBedrockToolName("fetch.get_url", nameMap, &counter)
		if first != second {
			t.Errorf("expected same cached result, got %q and %q", first, second)
		}
		if counter != 0 {
			t.Errorf("expected counter unchanged, got %d", counter)
		}
	})
}

func TestConvertGenaiToolsToBedrockSanitizesNames(t *testing.T) {
	tools := []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
		{Name: "fetch.get_url", Description: "Fetch a URL"},
		{Name: "filesystem:read_file", Description: "Read a file"},
	}}}

	bedrockTools, nameMap := convertGenaiToolsToBedrock(tools)
	if len(bedrockTools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(bedrockTools))
	}

	// Verify sanitized names in the Bedrock tool specs.
	for i, want := range []string{"fetch_get_url", "filesystem_read_file"} {
		tm, ok := bedrockTools[i].(*types.ToolMemberToolSpec)
		if !ok {
			t.Fatalf("tool %d: expected *types.ToolMemberToolSpec", i)
		}
		got := ""
		if tm.Value.Name != nil {
			got = *tm.Value.Name
		}
		if got != want {
			t.Errorf("tool %d: expected name %q, got %q", i, want, got)
		}
	}

	// Verify nameMap contains the mappings.
	if nameMap["fetch.get_url"] != "fetch_get_url" {
		t.Errorf("nameMap[fetch.get_url] = %q, want fetch_get_url", nameMap["fetch.get_url"])
	}
	if nameMap["filesystem:read_file"] != "filesystem_read_file" {
		t.Errorf("nameMap[filesystem:read_file] = %q, want filesystem_read_file", nameMap["filesystem:read_file"])
	}
}

func TestStreamingToolCallParseArgs(t *testing.T) {
	tests := []struct {
		name      string
		inputJSON string
		wantKeys  map[string]any
		wantEmpty bool
	}{
		{name: "empty input", inputJSON: "", wantEmpty: true},
		{name: "valid JSON", inputJSON: `{"location":"San Francisco","unit":"fahrenheit"}`, wantKeys: map[string]any{"location": "San Francisco", "unit": "fahrenheit"}},
		{name: "invalid JSON wrapped in _raw", inputJSON: `not-valid-json`, wantKeys: map[string]any{"_raw": "not-valid-json"}},
		{name: "chunked JSON assembled", inputJSON: `{"query":` + `"hello world"}`, wantKeys: map[string]any{"query": "hello world"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := (&streamingToolCall{InputJSON: tt.inputJSON}).parseArgs()
			if tt.wantEmpty {
				if len(result) != 0 {
					t.Errorf("expected empty map, got %v", result)
				}
				return
			}
			for k, want := range tt.wantKeys {
				if got, ok := result[k]; !ok || got != want {
					t.Errorf("key %q: expected %v, got %v (present=%v)", k, want, got, ok)
				}
			}
		})
	}
}
