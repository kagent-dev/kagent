package adk

import (
	"testing"

	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"google.golang.org/genai"
)

func TestConvertMapToGenAIContent_CodeExecutionResult(t *testing.T) {
	msgMap := map[string]interface{}{
		core.PartKeyRole: "user",
		core.PartKeyParts: []map[string]interface{}{
			{
				"code_execution_result": map[string]interface{}{
					"outcome": "OUTCOME_OK",
					"output":  "Hello, world!",
				},
			},
		},
	}

	content, err := convertMapToGenAIContent(msgMap)
	if err != nil {
		t.Fatalf("convertMapToGenAIContent() error = %v", err)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(content.Parts))
	}
	part := content.Parts[0]
	if part.CodeExecutionResult == nil {
		t.Fatal("Expected CodeExecutionResult to be set")
	}
	if part.CodeExecutionResult.Outcome != genai.OutcomeOK {
		t.Errorf("Expected outcome = OUTCOME_OK, got %q", part.CodeExecutionResult.Outcome)
	}
	if part.CodeExecutionResult.Output != "Hello, world!" {
		t.Errorf("Expected output = %q, got %q", "Hello, world!", part.CodeExecutionResult.Output)
	}
}

func TestConvertMapToGenAIContent_ExecutableCode(t *testing.T) {
	msgMap := map[string]interface{}{
		core.PartKeyRole: "user",
		core.PartKeyParts: []map[string]interface{}{
			{
				"executable_code": map[string]interface{}{
					"code":     "print('hello')",
					"language": "PYTHON",
				},
			},
		},
	}

	content, err := convertMapToGenAIContent(msgMap)
	if err != nil {
		t.Fatalf("convertMapToGenAIContent() error = %v", err)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(content.Parts))
	}
	part := content.Parts[0]
	if part.ExecutableCode == nil {
		t.Fatal("Expected ExecutableCode to be set")
	}
	if part.ExecutableCode.Code != "print('hello')" {
		t.Errorf("Expected code = %q, got %q", "print('hello')", part.ExecutableCode.Code)
	}
	if part.ExecutableCode.Language != genai.LanguagePython {
		t.Errorf("Expected language = PYTHON, got %q", part.ExecutableCode.Language)
	}
}
