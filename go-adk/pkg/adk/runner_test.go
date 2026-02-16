package adk

import (
	"fmt"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/converter"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/session"
)

func TestA2AMessageToGenAIContent_FunctionCall(t *testing.T) {
	msg := &a2atype.Message{
		Role: a2atype.MessageRoleUser,
		Parts: a2atype.ContentParts{
			&a2atype.DataPart{
				Data: map[string]interface{}{
					"name": "my_func",
					"args": map[string]interface{}{"key": "value"},
				},
				Metadata: map[string]interface{}{
					a2a.MetadataKeyType: a2a.A2ADataPartMetadataTypeFunctionCall,
				},
			},
		},
	}

	content, err := converter.A2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("A2AMessageToGenAIContent() error = %v", err)
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

func TestA2AMessageToGenAIContent_FunctionResponse(t *testing.T) {
	msg := &a2atype.Message{
		Role: a2atype.MessageRoleAgent,
		Parts: a2atype.ContentParts{
			&a2atype.DataPart{
				Data: map[string]interface{}{
					"name":     "my_func",
					"response": map[string]interface{}{"result": "ok"},
				},
				Metadata: map[string]interface{}{
					a2a.MetadataKeyType: a2a.A2ADataPartMetadataTypeFunctionResponse,
				},
			},
		},
	}

	content, err := converter.A2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("A2AMessageToGenAIContent() error = %v", err)
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

func TestA2AMessageToGenAIContent_TextPart(t *testing.T) {
	msg := &a2atype.Message{
		Role: a2atype.MessageRoleUser,
		Parts: a2atype.ContentParts{
			a2atype.TextPart{Text: "hello world"},
		},
	}

	content, err := converter.A2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("A2AMessageToGenAIContent() error = %v", err)
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

func TestA2AMessageToGenAIContent_AgentRole(t *testing.T) {
	msg := &a2atype.Message{
		Role: a2atype.MessageRoleAgent,
		Parts: a2atype.ContentParts{
			a2atype.TextPart{Text: "model response"},
		},
	}

	content, err := converter.A2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("A2AMessageToGenAIContent() error = %v", err)
	}
	if content.Role != "model" {
		t.Errorf("Expected role = model, got %q", content.Role)
	}
}

func TestA2AMessageToGenAIContent_Nil(t *testing.T) {
	_, err := converter.A2AMessageToGenAIContent(nil)
	if err == nil {
		t.Fatal("Expected error for nil message")
	}
}

func TestExtractRunArgs(t *testing.T) {
	sess := &session.Session{ID: "s1"}
	args := map[string]interface{}{
		a2a.ArgKeyUserID:    "user1",
		a2a.ArgKeySessionID: "sess1",
		a2a.ArgKeySession:   sess,
	}
	ra := extractRunArgs(args)
	if ra.userID != "user1" {
		t.Errorf("Expected userID = %q, got %q", "user1", ra.userID)
	}
	if ra.sessionID != "sess1" {
		t.Errorf("Expected sessionID = %q, got %q", "sess1", ra.sessionID)
	}
	if ra.session != sess {
		t.Error("Expected session to be the same pointer")
	}
}

func TestExtractRunArgs_Empty(t *testing.T) {
	ra := extractRunArgs(map[string]interface{}{})
	if ra.userID != "" || ra.sessionID != "" || ra.session != nil || ra.sessionService != nil {
		t.Error("Expected all zero values for empty args")
	}
}

func TestExtractMessageFromArgs(t *testing.T) {
	logger := logr.Discard()

	t.Run("pointer_message", func(t *testing.T) {
		msg := &a2atype.Message{ID: "m1", Role: a2atype.MessageRoleUser}
		args := map[string]interface{}{a2a.ArgKeyMessage: msg}
		result := extractMessageFromArgs(args, logger)
		if result == nil || result.ID != "m1" {
			t.Errorf("Expected message with ID m1, got %v", result)
		}
	})

	t.Run("value_message", func(t *testing.T) {
		msg := a2atype.Message{ID: "m2", Role: a2atype.MessageRoleUser}
		args := map[string]interface{}{a2a.ArgKeyMessage: msg}
		result := extractMessageFromArgs(args, logger)
		if result == nil || result.ID != "m2" {
			t.Errorf("Expected message with ID m2, got %v", result)
		}
	})

	t.Run("nil_message", func(t *testing.T) {
		args := map[string]interface{}{}
		result := extractMessageFromArgs(args, logger)
		if result != nil {
			t.Error("Expected nil for missing message")
		}
	})

	t.Run("wrong_type", func(t *testing.T) {
		args := map[string]interface{}{a2a.ArgKeyMessage: "not a message"}
		result := extractMessageFromArgs(args, logger)
		if result != nil {
			t.Error("Expected nil for wrong type")
		}
	})
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

func TestRunConfigFromArgs(t *testing.T) {
	t.Run("with_sse_streaming", func(t *testing.T) {
		args := map[string]interface{}{
			a2a.ArgKeyRunConfig: map[string]interface{}{
				a2a.RunConfigKeyStreamingMode: "SSE",
			},
		}
		cfg := runConfigFromArgs(args)
		if cfg.StreamingMode == "" {
			t.Error("Expected StreamingMode to be set for SSE")
		}
	})

	t.Run("empty_args", func(t *testing.T) {
		cfg := runConfigFromArgs(map[string]interface{}{})
		if cfg.StreamingMode != "" {
			t.Errorf("Expected empty StreamingMode for empty args, got %v", cfg.StreamingMode)
		}
	})
}

