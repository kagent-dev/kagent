package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveReadPath_AllowsSymlinkedSkillsDirectory(t *testing.T) {
	skillsDir := t.TempDir()
	skillFile := filepath.Join(skillsDir, "script.py")
	if err := os.WriteFile(skillFile, []byte("print('ok')\n"), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	resolved, err := resolveReadPath("session-read", skillsDir, "skills/script.py")
	if err != nil {
		t.Fatalf("resolveReadPath() error = %v", err)
	}
	if resolved != skillFile {
		t.Fatalf("resolveReadPath() = %q, want %q", resolved, skillFile)
	}
}

func TestResolveWritePath_BlocksSkillsSymlink(t *testing.T) {
	skillsDir := t.TempDir()
	_, err := resolveWritePath("session-write", skillsDir, "skills/new-file.txt")
	if err == nil {
		t.Fatal("expected write through skills symlink to be rejected")
	}
	if !strings.Contains(err.Error(), "outside the writable session directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewSkillsTools_ReturnsExpectedToolSet(t *testing.T) {
	skillsDir := t.TempDir()
	skillDir := filepath.Join(skillsDir, "demo")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: demo
description: Demo skill.
---
`), 0644); err != nil {
		t.Fatalf("failed to write skill metadata: %v", err)
	}

	tools, err := NewSkillsTools(skillsDir)
	if err != nil {
		t.Fatalf("NewSkillsTools() error = %v", err)
	}

	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.Name()] = true
	}

	for _, name := range []string{"skills", "read_file", "write_file", "edit_file", "bash"} {
		if !got[name] {
			t.Errorf("expected tool %q to be present", name)
		}
	}
}
