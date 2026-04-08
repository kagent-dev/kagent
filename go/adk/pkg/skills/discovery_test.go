package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createSkillTestEnv(t *testing.T) (sessionDir, skillsRootDir string) {
	// Create temporary directory structure
	tmpDir, err := os.MkdirTemp("", "skill-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	sessionDir = filepath.Join(tmpDir, "session")
	skillsRootDir = filepath.Join(tmpDir, "skills_root")

	// Create session directories
	uploadsDir := filepath.Join(sessionDir, "uploads")
	outputsDir := filepath.Join(sessionDir, "outputs")
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		t.Fatalf("Failed to create uploads dir: %v", err)
	}
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		t.Fatalf("Failed to create outputs dir: %v", err)
	}

	// Create skill directory
	skillDir := filepath.Join(skillsRootDir, "csv-to-json")
	scriptDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		t.Fatalf("Failed to create skill dir: %v", err)
	}

	// Create SKILL.md
	skillMD := `---
name: csv-to-json
description: Converts a CSV file to a JSON file.
---
# CSV to JSON Conversion
Use the ` + "`convert.py`" + ` script to convert a CSV file from the ` + "`uploads`" + ` directory
to a JSON file in the ` + "`outputs`" + ` directory.
Example: ` + "`bash(\"python skills/csv-to-json/scripts/convert.py uploads/data.csv outputs/result.json\")`" + `
`
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(skillMD), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Create Python script for the skill
	convertScript := `import csv
import json
import sys
if len(sys.argv) != 3:
    print(f"Usage: python {sys.argv[0]} <input_csv> <output_json>")
    sys.exit(1)
input_path, output_path = sys.argv[1], sys.argv[2]
try:
    data = []
    with open(input_path, 'r', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        for row in reader:
            data.append(row)
    with open(output_path, 'w', encoding='utf-8') as f:
        json.dump(data, f, indent=2)
    print(f"Successfully converted {input_path} to {output_path}")
except FileNotFoundError:
    print(f"Error: Input file not found at {input_path}")
    sys.exit(1)
`
	scriptFile := filepath.Join(scriptDir, "convert.py")
	if err := os.WriteFile(scriptFile, []byte(convertScript), 0644); err != nil {
		t.Fatalf("Failed to write convert.py: %v", err)
	}

	// Create symlink from session to skills root
	skillsLink := filepath.Join(sessionDir, "skills")
	if err := os.Symlink(skillsRootDir, skillsLink); err != nil {
		// On Windows, symlinks might fail, so we'll skip this test
		t.Logf("Failed to create symlink (may not be supported on this system): %v", err)
	}

	return sessionDir, skillsRootDir
}

func TestDiscoverSkills(t *testing.T) {
	sessionDir, skillsRootDir := createSkillTestEnv(t)
	defer os.RemoveAll(filepath.Dir(sessionDir))

	skills, err := DiscoverSkills(skillsRootDir)
	if err != nil {
		t.Fatalf("DiscoverSkills() error = %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}

	skill := skills[0]
	if skill.Name != "csv-to-json" {
		t.Errorf("Expected skill name = %q, got %q", "csv-to-json", skill.Name)
	}

	if !strings.Contains(skill.Description, "Converts a CSV file") {
		t.Errorf("Expected description to contain 'Converts a CSV file', got %q", skill.Description)
	}
}

func TestDiscoverSkills_EmptyDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "empty-skills-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skills, err := DiscoverSkills(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverSkills() error = %v", err)
	}

	if len(skills) != 0 {
		t.Errorf("Expected 0 skills in empty directory, got %d", len(skills))
	}
}

func TestDiscoverSkills_NonexistentDirectory(t *testing.T) {
	nonexistentDir := filepath.Join(os.TempDir(), "nonexistent-skills-12345")

	skills, err := DiscoverSkills(nonexistentDir)
	if err != nil {
		t.Fatalf("DiscoverSkills() should not error on nonexistent directory, got %v", err)
	}

	if len(skills) != 0 {
		t.Errorf("Expected 0 skills for nonexistent directory, got %d", len(skills))
	}
}

func TestDiscoverSkills_InvalidSkill(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "invalid-skill-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a directory without SKILL.md
	skillDir := filepath.Join(tmpDir, "no-skill-md")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill dir: %v", err)
	}

	skills, err := DiscoverSkills(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverSkills() error = %v", err)
	}

	// Should not include skills without SKILL.md
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills (no SKILL.md), got %d", len(skills))
	}
}

func TestDiscoverSkills_InvalidMetadata(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "invalid-metadata-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create skill with invalid metadata
	skillDir := filepath.Join(tmpDir, "invalid-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill dir: %v", err)
	}

	// SKILL.md without proper frontmatter
	skillFile := filepath.Join(skillDir, "SKILL.md")
	invalidContent := "This is not a valid SKILL.md file"
	if err := os.WriteFile(skillFile, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to write invalid SKILL.md: %v", err)
	}

	skills, err := DiscoverSkills(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverSkills() error = %v", err)
	}

	// Should skip skills with invalid metadata
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills (invalid metadata), got %d", len(skills))
	}
}

func TestLoadSkillContent(t *testing.T) {
	_, skillsRootDir := createSkillTestEnv(t)
	defer os.RemoveAll(filepath.Dir(skillsRootDir))

	content, err := LoadSkillContent(skillsRootDir, "csv-to-json")
	if err != nil {
		t.Fatalf("LoadSkillContent() error = %v", err)
	}

	if !strings.Contains(content, "name: csv-to-json") {
		t.Error("Expected 'name: csv-to-json' in content")
	}

	if !strings.Contains(content, "# CSV to JSON Conversion") {
		t.Error("Expected '# CSV to JSON Conversion' in content")
	}

	if !strings.Contains(content, "Example:") {
		t.Error("Expected 'Example:' in content")
	}
}

func TestLoadSkillContent_NonexistentSkill(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "load-skill-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = LoadSkillContent(tmpDir, "nonexistent-skill")
	if err == nil {
		t.Error("Expected error for nonexistent skill, got nil")
	}
}

func TestLoadSkillContent_NoSkillMD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "no-skill-md-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create skill directory but no SKILL.md
	skillDir := filepath.Join(tmpDir, "no-md-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill dir: %v", err)
	}

	_, err = LoadSkillContent(tmpDir, "no-md-skill")
	if err == nil {
		t.Error("Expected error for skill without SKILL.md, got nil")
	}
}

func TestSkillExecution_Integration(t *testing.T) {
	sessionDir, _ := createSkillTestEnv(t)
	defer os.RemoveAll(filepath.Dir(sessionDir))

	// 1. "Upload" a file for the skill to process
	inputCSVPath := filepath.Join(sessionDir, "uploads", "data.csv")
	csvContent := "id,name\n1,Alice\n2,Bob\n"
	if err := os.WriteFile(inputCSVPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("Failed to write input CSV: %v", err)
	}

	// 2. Execute the skill's core command
	command := "python skills/csv-to-json/scripts/convert.py uploads/data.csv outputs/result.json"
	result, err := ExecuteCommand(context.Background(), command, sessionDir)
	if err != nil {
		// Python might not be available, skip this test
		t.Skipf("Python not available or command failed: %v", err)
	}

	if !strings.Contains(result, "Successfully converted") {
		t.Errorf("Expected 'Successfully converted' in result, got %q", result)
	}

	// 3. Verify the output by reading the generated file
	outputJSONPath := filepath.Join(sessionDir, "outputs", "result.json")
	rawOutput, err := ReadFileContent(outputJSONPath, 0, 0)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	// Parse line-numbered output to get JSON content
	lines := strings.Split(rawOutput, "\n")
	var jsonLines []string
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 {
			jsonLines = append(jsonLines, parts[1])
		}
	}
	jsonContentStr := strings.Join(jsonLines, "\n")

	// Parse and verify JSON content
	var data []map[string]string
	if err := json.Unmarshal([]byte(jsonContentStr), &data); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	expectedData := []map[string]string{
		{"id": "1", "name": "Alice"},
		{"id": "2", "name": "Bob"},
	}

	if len(data) != len(expectedData) {
		t.Fatalf("Expected %d records, got %d", len(expectedData), len(data))
	}

	for i, expected := range expectedData {
		if data[i]["id"] != expected["id"] || data[i]["name"] != expected["name"] {
			t.Errorf("Record %d: expected %v, got %v", i, expected, data[i])
		}
	}
}

func TestGetSessionPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session-path-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skillsDir := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("Failed to create skills dir: %v", err)
	}

	sessionID := "test-session-123"
	sessionPath, err := GetSessionPath(sessionID, skillsDir)
	if err != nil {
		t.Fatalf("GetSessionPath() error = %v", err)
	}

	// Verify session path structure
	uploadsDir := filepath.Join(sessionPath, "uploads")
	outputsDir := filepath.Join(sessionPath, "outputs")
	skillsLink := filepath.Join(sessionPath, "skills")

	if _, err := os.Stat(uploadsDir); os.IsNotExist(err) {
		t.Error("Expected uploads directory to exist")
	}

	if _, err := os.Stat(outputsDir); os.IsNotExist(err) {
		t.Error("Expected outputs directory to exist")
	}

	// Check if skills symlink exists (may not work on all systems)
	if _, err := os.Lstat(skillsLink); err == nil {
		// Symlink exists, verify it points to skills directory
		linkTarget, err := os.Readlink(skillsLink)
		if err == nil {
			// Resolve absolute paths for comparison
			absSkillsDir, err1 := filepath.Abs(skillsDir)
			if err1 != nil {
				t.Fatalf("Failed to resolve absolute path for skillsDir: %v", err1)
			}

			// If linkTarget is relative, resolve it relative to the symlink's directory
			var absLinkTarget string
			if filepath.IsAbs(linkTarget) {
				absLinkTarget, err = filepath.Abs(linkTarget)
				if err != nil {
					t.Fatalf("Failed to resolve absolute path for linkTarget: %v", err)
				}
			} else {
				// Resolve relative symlink
				absLinkTarget = filepath.Join(filepath.Dir(skillsLink), linkTarget)
				absLinkTarget, err = filepath.Abs(absLinkTarget)
				if err != nil {
					t.Fatalf("Failed to resolve absolute path for relative linkTarget: %v", err)
				}
			}

			// Clean paths for comparison (remove trailing slashes, resolve . and ..)
			absSkillsDir = filepath.Clean(absSkillsDir)
			absLinkTarget = filepath.Clean(absLinkTarget)

			if absLinkTarget != absSkillsDir {
				t.Errorf("Expected symlink to point to %q, got %q (resolved from %q)", absSkillsDir, absLinkTarget, linkTarget)
			}
		}
	}
}

func TestGenerateSkillsToolDescription(t *testing.T) {
	skills := []Skill{
		{Name: "skill1", Description: "First skill"},
		{Name: "skill2", Description: "Second skill"},
	}

	description := GenerateSkillsToolDescription(skills)

	if !strings.Contains(description, "skill1") {
		t.Error("Expected 'skill1' in description")
	}

	if !strings.Contains(description, "skill2") {
		t.Error("Expected 'skill2' in description")
	}

	if !strings.Contains(description, "First skill") {
		t.Error("Expected 'First skill' in description")
	}
}

func TestGenerateSkillsToolDescription_Empty(t *testing.T) {
	description := GenerateSkillsToolDescription([]Skill{})

	if !strings.Contains(description, "No skills available") {
		t.Errorf("Expected 'No skills available' message, got %q", description)
	}
}
