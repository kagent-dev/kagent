package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a discovered skill with metadata
type Skill struct {
	Name        string
	Description string
}

// DiscoverSkills discovers available skills in the skills directory
func DiscoverSkills(skillsDirectory string) ([]Skill, error) {
	dir := filepath.Clean(skillsDirectory)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return []Skill{}, nil
	}

	var skills []Skill
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read skills directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(dir, entry.Name())
		skillFile := filepath.Join(skillDir, "SKILL.md")

		if _, err := os.Stat(skillFile); os.IsNotExist(err) {
			continue
		}

		// Parse skill metadata from SKILL.md
		metadata, err := parseSkillMetadata(skillFile)
		if err != nil {
			continue // Skip skills with invalid metadata
		}

		skills = append(skills, Skill{
			Name:        metadata["name"],
			Description: metadata["description"],
		})
	}

	return skills, nil
}

// LoadSkillContent loads the full content of a skill's SKILL.md file
func LoadSkillContent(skillsDirectory, skillName string) (string, error) {
	skillDir := filepath.Join(skillsDirectory, skillName)
	skillFile := filepath.Join(skillDir, "SKILL.md")

	if _, err := os.Stat(skillFile); os.IsNotExist(err) {
		return "", fmt.Errorf("skill '%s' not found or has no SKILL.md file", skillName)
	}

	content, err := os.ReadFile(skillFile)
	if err != nil {
		return "", fmt.Errorf("failed to load skill '%s': %w", skillName, err)
	}

	return string(content), nil
}

// parseSkillMetadata parses YAML frontmatter from SKILL.md
func parseSkillMetadata(skillFile string) (map[string]string, error) {
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, err
	}

	contentStr := string(content)
	if !strings.HasPrefix(contentStr, "---") {
		return nil, fmt.Errorf("no YAML frontmatter found")
	}

	parts := strings.SplitN(contentStr, "---", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid YAML frontmatter format")
	}

	// Simple YAML parsing for name and description
	// For full YAML support, you might want to use a YAML library
	frontmatter := parts[1]
	metadata := make(map[string]string)

	lines := strings.Split(frontmatter, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			metadata["name"] = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			metadata["name"] = strings.Trim(metadata["name"], `"'`)
		} else if strings.HasPrefix(line, "description:") {
			metadata["description"] = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			metadata["description"] = strings.Trim(metadata["description"], `"'`)
		}
	}

	if metadata["name"] == "" || metadata["description"] == "" {
		return nil, fmt.Errorf("missing required metadata fields")
	}

	return metadata, nil
}

// GenerateSkillsToolDescription generates a tool description with available skills
func GenerateSkillsToolDescription(skills []Skill) string {
	if len(skills) == 0 {
		return "No skills available. Use this tool to discover and load skill instructions."
	}

	var desc strings.Builder
	desc.WriteString("Discover and load skill instructions. Available skills:\n\n")

	for _, skill := range skills {
		desc.WriteString(fmt.Sprintf("- %s: %s\n", skill.Name, skill.Description))
	}

	desc.WriteString("\nCall this tool with command='<skill-name>' to load the full skill instructions.")
	return desc.String()
}

// GetSessionPath returns the working directory path for a session
func GetSessionPath(sessionID, skillsDirectory string) (string, error) {
	basePath := filepath.Join(os.TempDir(), "kagent")
	sessionPath := filepath.Join(basePath, sessionID)

	// Create working directories
	uploadsDir := filepath.Join(sessionPath, "uploads")
	outputsDir := filepath.Join(sessionPath, "outputs")

	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create uploads directory: %w", err)
	}
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create outputs directory: %w", err)
	}

	// Create symlink to skills directory
	skillsLink := filepath.Join(sessionPath, "skills")
	// Use absolute path for symlink target to avoid issues with relative paths
	absSkillsDir, err := filepath.Abs(skillsDirectory)
	if err != nil {
		// If we can't get absolute path, use original
		absSkillsDir = skillsDirectory
	}

	// Check if symlink already exists
	if linkInfo, err := os.Lstat(skillsLink); err == nil {
		// If it's a symlink, check if it points to the correct location
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			existingTarget, err := os.Readlink(skillsLink)
			if err == nil {
				// Resolve existing target to absolute path
				var absExistingTarget string
				if filepath.IsAbs(existingTarget) {
					absExistingTarget, _ = filepath.Abs(existingTarget)
				} else {
					absExistingTarget = filepath.Join(filepath.Dir(skillsLink), existingTarget)
					absExistingTarget, _ = filepath.Abs(absExistingTarget)
				}
				absExistingTarget = filepath.Clean(absExistingTarget)
				absSkillsDirClean := filepath.Clean(absSkillsDir)

				// If it points to the correct location, we're done
				if absExistingTarget == absSkillsDirClean {
					return sessionPath, nil
				}
			}
		}
		// Remove existing symlink/file if it doesn't point to the correct location
		os.Remove(skillsLink)
	}

	// Create new symlink
	if err := os.Symlink(absSkillsDir, skillsLink); err != nil {
		// Ignore: skills can still be accessed via absolute path
		_ = err
	}

	return sessionPath, nil
}
