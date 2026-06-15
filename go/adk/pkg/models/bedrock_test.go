package models

import (
	"encoding/json"
	"strings"
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
		{
			name: "thinking block preserved as ReasoningContent",
			contents: []*genai.Content{
				{
					Role: "model",
					Parts: []*genai.Part{
						{Thought: true, Text: "let me think", ThoughtSignature: []byte("sig123")},
						{FunctionCall: &genai.FunctionCall{ID: "c1", Name: "get_weather", Args: map[string]any{"location": "Paris"}}},
					},
				},
			},
			wantMsgCount: 1,
			checkMsg: func(t *testing.T, msgs []types.Message) {
				if len(msgs[0].Content) != 2 {
					t.Fatalf("expected 2 blocks (thinking + toolUse), got %d", len(msgs[0].Content))
				}
				rb, ok := msgs[0].Content[0].(*types.ContentBlockMemberReasoningContent)
				if !ok {
					t.Fatalf("block 0: want *ContentBlockMemberReasoningContent, got %T", msgs[0].Content[0])
				}
				rt, ok := rb.Value.(*types.ReasoningContentBlockMemberReasoningText)
				if !ok {
					t.Fatalf("reasoning value: want *ReasoningContentBlockMemberReasoningText, got %T", rb.Value)
				}
				if *rt.Value.Text != "let me think" {
					t.Errorf("text: want %q, got %q", "let me think", *rt.Value.Text)
				}
				if *rt.Value.Signature != "sig123" {
					t.Errorf("signature: want %q, got %q", "sig123", *rt.Value.Signature)
				}
				if _, ok := msgs[0].Content[1].(*types.ContentBlockMemberToolUse); !ok {
					t.Errorf("block 1: want *ContentBlockMemberToolUse, got %T", msgs[0].Content[1])
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

		bt1, nm1 := convertGenaiToolsToBedrock(tools, false, "")
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

		bt2, nm2 := convertGenaiToolsToBedrock(tools, false, "")
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

		bt3, nm3 := convertGenaiToolsToBedrock(tools, false, "")
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

	bedrockTools, nameMap := convertGenaiToolsToBedrock(tools, false, "")
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

func TestThinkingOnlyInLastAssistantTurn(t *testing.T) {
	contents := []*genai.Content{
		{
			Role: "model",
			Parts: []*genai.Part{
				{Thought: true, Text: "first think", ThoughtSignature: []byte("sig1")},
				{FunctionCall: &genai.FunctionCall{ID: "c1", Name: "tool_a", Args: map[string]any{}}},
			},
		},
		{
			Role:  "user",
			Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "c1", Name: "tool_a", Response: map[string]any{"r": "v1"}}}},
		},
		{
			Role: "model",
			Parts: []*genai.Part{
				{Thought: true, Text: "second think", ThoughtSignature: []byte("sig2")},
				{FunctionCall: &genai.FunctionCall{ID: "c2", Name: "tool_b", Args: map[string]any{}}},
			},
		},
		{
			Role:  "user",
			Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "c2", Name: "tool_b", Response: map[string]any{"r": "v2"}}}},
		},
	}

	msgs, _ := convertGenaiContentsToBedrockMessages(contents, nil)
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}

	// First assistant turn must NOT contain reasoning content.
	for _, block := range msgs[0].Content {
		if _, ok := block.(*types.ContentBlockMemberReasoningContent); ok {
			t.Error("first assistant turn must not contain reasoning content")
		}
	}

	// Last assistant turn (index 2) must contain reasoning content.
	hasReasoning := false
	for _, block := range msgs[2].Content {
		if _, ok := block.(*types.ContentBlockMemberReasoningContent); ok {
			hasReasoning = true
		}
	}
	if !hasReasoning {
		t.Error("last assistant turn must contain reasoning content")
	}
}

func TestHistoricalToolResultTruncation(t *testing.T) {
	longOutput := strings.Repeat("x", historyToolResultMaxLen+500)
	contents := []*genai.Content{
		{
			Role:  "user",
			Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "c1", Name: "tool_a", Response: map[string]any{"result": longOutput}}}},
		},
		{
			Role:  "user",
			Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "c2", Name: "tool_b", Response: map[string]any{"result": longOutput}}}},
		},
	}

	msgs, _ := convertGenaiContentsToBedrockMessages(contents, nil)
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}

	extractText := func(msg types.Message) string {
		for _, block := range msg.Content {
			if tr, ok := block.(*types.ContentBlockMemberToolResult); ok {
				for _, c := range tr.Value.Content {
					if txt, ok := c.(*types.ToolResultContentBlockMemberText); ok {
						return txt.Value
					}
				}
			}
		}
		return ""
	}

	first := extractText(msgs[0])
	if len(first) >= len(longOutput) {
		t.Errorf("historical tool result should be truncated, got len=%d", len(first))
	}

	last := extractText(msgs[1])
	if len(last) != len(longOutput) {
		t.Errorf("latest tool result must not be truncated, got len=%d want %d", len(last), len(longOutput))
	}
}

func TestTruncateToolResult(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		maxLen  int
		wantLen int
		wantMsg bool
	}{
		{"no truncation needed", "short", 100, 5, false},
		{"exact boundary", strings.Repeat("a", 100), 100, 100, false},
		{"truncated", strings.Repeat("a", 150), 100, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateToolResult(tc.input, tc.maxLen)
			if tc.wantMsg {
				if len(got) <= tc.maxLen {
					t.Errorf("expected truncated result longer than maxLen, got %d", len(got))
				}
				if !strings.Contains(got, "truncated") {
					t.Error("truncated result must contain truncation notice")
				}
			} else {
				if len(got) != tc.wantLen {
					t.Errorf("want len %d, got %d", tc.wantLen, len(got))
				}
			}
		})
	}
}

func TestBuildInferenceConfig(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }
	f32 := func(v float32) *float32 { return &v }
	i32 := func(v int32) *int32 { return &v }

	tests := []struct {
		name           string
		cfg            BedrockConfig
		thinkingActive bool
		wantNil        bool
		wantTemp       *float32
		wantTopP       *float32
		wantMaxTokens  *int32
	}{
		{
			name:           "thinking drops temperature and topP",
			cfg:            BedrockConfig{Temperature: f64(0.7), TopP: f64(0.9)},
			thinkingActive: true,
			wantNil:        true,
		},
		{
			name:           "thinking with maxTokens keeps only maxTokens",
			cfg:            BedrockConfig{Temperature: f64(0.7), TopP: f64(0.9), MaxTokens: func() *int { v := 1000; return &v }()},
			thinkingActive: true,
			wantNil:        false,
			wantMaxTokens:  i32(1000),
		},
		{
			name:           "no thinking passes temperature and topP",
			cfg:            BedrockConfig{Temperature: f64(0.7), TopP: f64(0.9)},
			thinkingActive: false,
			wantNil:        false,
			wantTemp:       f32(0.7),
			wantTopP:       f32(0.9),
		},
		{
			name:           "all nil returns nil",
			cfg:            BedrockConfig{},
			thinkingActive: false,
			wantNil:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildInferenceConfig(&tt.cfg, tt.thinkingActive)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("want non-nil InferenceConfiguration, got nil")
			}
			if tt.wantTemp == nil && got.Temperature != nil {
				t.Errorf("temperature: want nil, got %v", *got.Temperature)
			}
			if tt.wantTemp != nil {
				if got.Temperature == nil {
					t.Fatalf("temperature: want %v, got nil", *tt.wantTemp)
				}
				if *got.Temperature != *tt.wantTemp {
					t.Errorf("temperature: want %v, got %v", *tt.wantTemp, *got.Temperature)
				}
			}
			if tt.wantTopP == nil && got.TopP != nil {
				t.Errorf("topP: want nil, got %v", *got.TopP)
			}
			if tt.wantTopP != nil {
				if got.TopP == nil {
					t.Fatalf("topP: want %v, got nil", *tt.wantTopP)
				}
				if *got.TopP != *tt.wantTopP {
					t.Errorf("topP: want %v, got %v", *tt.wantTopP, *got.TopP)
				}
			}
			if tt.wantMaxTokens != nil {
				if got.MaxTokens == nil {
					t.Fatalf("maxTokens: want %v, got nil", *tt.wantMaxTokens)
				}
				if *got.MaxTokens != *tt.wantMaxTokens {
					t.Errorf("maxTokens: want %v, got %v", *tt.wantMaxTokens, *got.MaxTokens)
				}
			}
		})
	}
}

func TestConvertGenaiToolsToBedrockPromptCaching(t *testing.T) {
	tools := []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
		{Name: "get_weather", Description: "lookup weather"},
		{Name: "list_pods", Description: "list pods"},
	}}}

	t.Run("disabled: no cache marker appended", func(t *testing.T) {
		out, _ := convertGenaiToolsToBedrock(tools, false, "")
		if len(out) != 2 {
			t.Fatalf("expected 2 tools, got %d", len(out))
		}
		for i, tool := range out {
			if _, ok := tool.(*types.ToolMemberCachePoint); ok {
				t.Fatalf("did not expect a CachePoint at index %d when caching disabled", i)
			}
		}
	})

	t.Run("enabled: cache marker appended at the END of the tool list", func(t *testing.T) {
		out, _ := convertGenaiToolsToBedrock(tools, true, "")
		if len(out) != 3 {
			t.Fatalf("expected 3 entries (2 tools + 1 CachePoint), got %d", len(out))
		}
		// The first two must remain ToolSpec entries (order preserved).
		for i := range 2 {
			if _, ok := out[i].(*types.ToolMemberToolSpec); !ok {
				t.Fatalf("entry %d: expected ToolMemberToolSpec, got %T", i, out[i])
			}
		}
		// The trailing entry must be a CachePoint with type=default.
		cp, ok := out[2].(*types.ToolMemberCachePoint)
		if !ok {
			t.Fatalf("trailing entry: expected ToolMemberCachePoint, got %T", out[2])
		}
		if cp.Value.Type != types.CachePointTypeDefault {
			t.Errorf("expected CachePointType=default, got %v", cp.Value.Type)
		}
		// Default (empty) TTL must leave Ttl unset so Bedrock applies its
		// standard 5-minute cache (broadest model support).
		if cp.Value.Ttl != "" {
			t.Errorf("expected unset Ttl for default cache, got %q", cp.Value.Ttl)
		}
	})

	t.Run(`cacheTTL "5m": Ttl left unset (default 5-minute cache)`, func(t *testing.T) {
		out, _ := convertGenaiToolsToBedrock(tools, true, "5m")
		cp, ok := out[len(out)-1].(*types.ToolMemberCachePoint)
		if !ok {
			t.Fatalf("trailing entry: expected ToolMemberCachePoint, got %T", out[len(out)-1])
		}
		if cp.Value.Ttl != "" {
			t.Errorf("expected unset Ttl for 5m, got %q", cp.Value.Ttl)
		}
	})

	t.Run(`cacheTTL "1h": Ttl set to extended-TTL caching`, func(t *testing.T) {
		out, _ := convertGenaiToolsToBedrock(tools, true, "1h")
		cp, ok := out[len(out)-1].(*types.ToolMemberCachePoint)
		if !ok {
			t.Fatalf("trailing entry: expected ToolMemberCachePoint, got %T", out[len(out)-1])
		}
		if cp.Value.Ttl != types.CacheTTLOneHour {
			t.Errorf("expected Ttl=%q, got %q", types.CacheTTLOneHour, cp.Value.Ttl)
		}
	})

	t.Run("enabled but no tools: no cache marker (skipped)", func(t *testing.T) {
		out, _ := convertGenaiToolsToBedrock(nil, true, "")
		if len(out) != 0 {
			t.Fatalf("expected empty slice for no tools, got %d entries", len(out))
		}
	})
}
