package a2a

import (
	"fmt"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
)

func TestConvertA2AMessageToGenAIContent_FunctionCall(t *testing.T) {
	msg := &a2atype.Message{
		Role: a2atype.MessageRoleUser,
		Parts: a2atype.ContentParts{
			&a2atype.DataPart{
				Data: map[string]interface{}{
					"name": "my_func",
					"args": map[string]interface{}{"key": "value"},
				},
				Metadata: map[string]interface{}{
					GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionCall,
				},
			},
		},
	}

	content, err := convertA2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("convertA2AMessageToGenAIContent() error = %v", err)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(content.Parts))
	}
	part := content.Parts[0]
	if part.FunctionCall == nil {
		t.Fatal("Expected FunctionCall to be set")
	}
	if part.FunctionCall.Name != "my_func" {
		t.Errorf("Expected name = %q, got %q", "my_func", part.FunctionCall.Name)
	}
}

func TestConvertA2AMessageToGenAIContent_FunctionResponse(t *testing.T) {
	msg := &a2atype.Message{
		Role: a2atype.MessageRoleAgent,
		Parts: a2atype.ContentParts{
			&a2atype.DataPart{
				Data: map[string]interface{}{
					"name":     "my_func",
					"response": map[string]interface{}{"result": "ok"},
				},
				Metadata: map[string]interface{}{
					GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionResponse,
				},
			},
		},
	}

	content, err := convertA2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("convertA2AMessageToGenAIContent() error = %v", err)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(content.Parts))
	}
	part := content.Parts[0]
	if part.FunctionResponse == nil {
		t.Fatal("Expected FunctionResponse to be set")
	}
	if part.FunctionResponse.Name != "my_func" {
		t.Errorf("Expected name = %q, got %q", "my_func", part.FunctionResponse.Name)
	}
}

func TestConvertA2AMessageToGenAIContent_TextPart(t *testing.T) {
	msg := &a2atype.Message{
		Role: a2atype.MessageRoleUser,
		Parts: a2atype.ContentParts{
			a2atype.TextPart{Text: "hello world"},
		},
	}

	content, err := convertA2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("convertA2AMessageToGenAIContent() error = %v", err)
	}
	if content.Role != "user" {
		t.Errorf("Expected role = user, got %q", content.Role)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(content.Parts))
	}
	if content.Parts[0].Text != "hello world" {
		t.Errorf("Expected text = %q, got %q", "hello world", content.Parts[0].Text)
	}
}

func TestConvertA2AMessageToGenAIContent_AgentRole(t *testing.T) {
	msg := &a2atype.Message{
		Role: a2atype.MessageRoleAgent,
		Parts: a2atype.ContentParts{
			a2atype.TextPart{Text: "model response"},
		},
	}

	content, err := convertA2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("convertA2AMessageToGenAIContent() error = %v", err)
	}
	if content.Role != "model" {
		t.Errorf("Expected role = model, got %q", content.Role)
	}
}

func TestConvertA2AMessageToGenAIContent_Nil(t *testing.T) {
	_, err := convertA2AMessageToGenAIContent(nil)
	if err == nil {
		t.Fatal("Expected error for nil message")
	}
}

func TestFormatRunnerError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantCode     string
		wantContains string
	}{
		{
			name:     "nil_error",
			err:      nil,
			wantCode: "",
		},
		{
			name:         "mcp_connection_error",
			err:          fmt.Errorf("failed to get mcp session: dial tcp timeout"),
			wantCode:     "MCP_CONNECTION_ERROR",
			wantContains: "MCP connection failure",
		},
		{
			name:         "dns_error",
			err:          fmt.Errorf("lookup mcp-server: no such host"),
			wantCode:     "MCP_DNS_ERROR",
			wantContains: "DNS resolution failure",
		},
		{
			name:         "connection_refused",
			err:          fmt.Errorf("dial tcp: connect: connection refused"),
			wantCode:     "MCP_CONNECTION_REFUSED",
			wantContains: "Failed to connect",
		},
		{
			name:     "generic_error",
			err:      fmt.Errorf("something unexpected"),
			wantCode: "RUNNER_ERROR",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, code := formatRunnerError(tt.err)
			if code != tt.wantCode {
				t.Errorf("Expected code %q, got %q", tt.wantCode, code)
			}
			if tt.wantContains != "" && !strings.Contains(msg, tt.wantContains) {
				t.Errorf("Expected message to contain %q, got %q", tt.wantContains, msg)
			}
		})
	}
}
