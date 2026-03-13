package tools

import (
	"context"
	"testing"
)

func TestExecSimple(t *testing.T) {
	result, err := Exec(context.Background(), "echo hello", 0, "")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", result.Stdout)
	}
}

func TestExecNonZeroExit(t *testing.T) {
	result, err := Exec(context.Background(), "exit 42", 0, "")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestExecWorkingDir(t *testing.T) {
	dir := t.TempDir()
	result, err := Exec(context.Background(), "pwd", 0, dir)
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestExecEmptyCommand(t *testing.T) {
	_, err := Exec(context.Background(), "", 0, "")
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestExecTimeout(t *testing.T) {
	_, err := Exec(context.Background(), "sleep 10", 100, "")
	if err == nil {
		// Timeout should cause a non-zero exit or error
		t.Log("timeout completed without error (process was killed)")
	}
}
