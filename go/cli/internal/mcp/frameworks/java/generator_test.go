package java

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/cli/internal/mcp"
)

func TestNewGenerator(t *testing.T) {
	generator := NewGenerator()
	if generator == nil {
		t.Fatal("NewGenerator() returned nil")
	}
}

func TestGenerateProject(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "java-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	generator := NewGenerator()
	config := mcp.ProjectConfig{
		ProjectName: "test-project",
		Version:     "0.1.0",
		Description: "Test Java MCP project",
		Author:      "Test Author",
		Email:       "test@example.com",
		Directory:   tempDir,
		NoGit:       true,
		Verbose:     false,
	}

	err = generator.GenerateProject(config)
	if err != nil {
		t.Fatalf("GenerateProject failed: %v", err)
	}

	// Check that key files were created
	expectedFiles := []string{
		"pom.xml",
		"src/main/java/com/example/Main.java",
		"src/main/java/com/example/tools/Tool.java",
		"src/main/java/com/example/tools/Tools.java",
		"src/main/java/com/example/tools/Echo.java",
		"Dockerfile",
		"README.md",
		".gitignore",
	}

	for _, file := range expectedFiles {
		filePath := filepath.Join(tempDir, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not created", file)
		}
	}
}

func TestGenerateTool(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "java-tool-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Create the tools directory structure
	toolsDir := filepath.Join(tempDir, "src", "main", "java", "com", "example", "tools")
	err = os.MkdirAll(toolsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create tools directory: %v", err)
	}

	generator := NewGenerator()
	config := mcp.ToolConfig{
		ToolName:    "weather",
		Description: "Get weather information",
	}

	err = generator.GenerateTool(tempDir, config)
	if err != nil {
		t.Fatalf("GenerateTool failed: %v", err)
	}

	// Check that the tool file was created
	toolFile := filepath.Join(toolsDir, "Weather.java")
	if _, err := os.Stat(toolFile); os.IsNotExist(err) {
		t.Errorf("Expected tool file %s was not created", toolFile)
	}

	// Check that Tools.java was updated
	toolsFile := filepath.Join(toolsDir, "Tools.java")
	if _, err := os.Stat(toolsFile); os.IsNotExist(err) {
		t.Errorf("Expected Tools.java file was not created")
	}
}
