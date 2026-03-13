package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSkillsEmpty(t *testing.T) {
	dir := t.TempDir()
	skills, err := ListSkills(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestListSkillsNonExistentDir(t *testing.T) {
	skills, err := ListSkills("/nonexistent/skills/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil for nonexistent dir, got %v", skills)
	}
}

func TestListSkillsWithFrontmatter(t *testing.T) {
	dir := t.TempDir()

	// Create skill with full frontmatter.
	skillDir := filepath.Join(dir, "my-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: my-skill
description: A test skill
---

# My Skill

Skill content here.
`), 0o644)

	// Create skill with no frontmatter.
	skill2Dir := filepath.Join(dir, "bare-skill")
	os.MkdirAll(skill2Dir, 0o755)
	os.WriteFile(filepath.Join(skill2Dir, "SKILL.md"), []byte("# Bare Skill\nNo frontmatter.\n"), 0o644)

	skills, err := ListSkills(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	found := make(map[string]string)
	for _, s := range skills {
		found[s.Name] = s.Description
	}

	if desc, ok := found["my-skill"]; !ok || desc != "A test skill" {
		t.Errorf("expected my-skill with description, got %v", found)
	}
	if _, ok := found["bare-skill"]; !ok {
		t.Errorf("expected bare-skill fallback name, got %v", found)
	}
}

func TestLoadSkill(t *testing.T) {
	dir := t.TempDir()

	skillDir := filepath.Join(dir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	content := "---\nname: test-skill\n---\n\n# Test Skill\nHello!"
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644)

	result, err := LoadSkill(dir, "test-skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != content {
		t.Errorf("unexpected content: %s", result)
	}
}

func TestLoadSkillNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadSkill(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}

func TestLoadSkillPathTraversal(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadSkill(dir, "../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}
