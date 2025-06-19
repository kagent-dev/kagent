package common

import (
	"context"
	"testing"
)

func TestShellTool(t *testing.T) {
	// Test successful command
	params := shellParams{Command: "echo hello"}
	output, err := shellTool(context.Background(), params)
	if err != nil {
		t.Errorf("shellTool failed with error: %v", err)
	}
	if output != "hello" {
		t.Errorf("shellTool output was incorrect, got: %s, want: %s", output, "hello")
	}

	// Test failing command
	params = shellParams{Command: "nonexistentcommand"}
	_, err = shellTool(context.Background(), params)
	if err == nil {
		t.Errorf("shellTool should have failed but did not")
	}

	// Test empty command
	params = shellParams{Command: ""}
	_, err = shellTool(context.Background(), params)
	if err == nil {
		t.Errorf("shellTool should have failed with an empty command but did not")
	}
}
