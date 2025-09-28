package config

import (
	"os"
	"path"
	"testing"
)

func TestGetConfigDirFirstRun(t *testing.T) {
	homeDir := t.TempDir()
	checkGetConfig(t, homeDir)
}

func TestGetConfigDirSubsequentRun(t *testing.T) {
	homeDir := t.TempDir()
	checkGetConfig(t, homeDir)
	checkGetConfig(t, homeDir)
}

func TestHandlesErrorWhenCreatingConfigDir(t *testing.T) {
	homeDir := t.TempDir()
	nonExistentDir := path.Join(homeDir, "/invalid/path")
	result, err := GetConfigDir(nonExistentDir)
	if err == nil {
		t.Fatalf("Expected error, but got nil")
	}
	if result != "" {
		t.Fatalf("Expected empty string, but got %s", result)
	}
}

func checkGetConfig(t *testing.T, homeDir string) {
	configDir, err := GetConfigDir(homeDir)

	//check for error
	if err != nil {
		t.Fatalf("Expected no error, but got %v", err)
	}

	//check it's equal to the expected path
	expectedDir := path.Join(homeDir, ".config", "kagent")
	if configDir != expectedDir {
		t.Fatalf("Expected %s, but got %s", expectedDir, configDir)
	}

	//check kagent folder is exists
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Fatalf("Expected %s to exist, but it does not", path.Join(homeDir, "kagent"))
	}
}
