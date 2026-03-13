package tools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillInfo describes a discovered skill.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListSkills scans skillsDir for subdirectories containing a SKILL.md file,
// parses YAML frontmatter (name/description), and returns the list.
func ListSkills(skillsDir string) ([]SkillInfo, error) {
	if skillsDir == "" {
		skillsDir = "/skills"
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read skills directory %s: %w", skillsDir, err)
	}

	var skills []SkillInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, e.Name(), "SKILL.md")
		info, err := parseSkillFrontmatter(skillPath)
		if err != nil {
			continue // skip skills that can't be parsed
		}
		skills = append(skills, *info)
	}
	return skills, nil
}

// LoadSkill reads the full content of a skill's SKILL.md file.
func LoadSkill(skillsDir, name string) (string, error) {
	if skillsDir == "" {
		skillsDir = "/skills"
	}

	// Prevent path traversal.
	clean := filepath.Clean(name)
	if strings.Contains(clean, "..") || strings.ContainsAny(clean, "/\\") {
		return "", fmt.Errorf("invalid skill name: %s", name)
	}

	skillPath := filepath.Join(skillsDir, clean, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return "", fmt.Errorf("failed to read skill %s: %w", name, err)
	}
	return string(data), nil
}

// parseSkillFrontmatter extracts name and description from YAML frontmatter
// in a SKILL.md file. The frontmatter is delimited by "---" lines.
func parseSkillFrontmatter(path string) (*SkillInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inFrontmatter := false
	info := &SkillInfo{}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" {
			if inFrontmatter {
				break // end of frontmatter
			}
			inFrontmatter = true
			continue
		}

		if !inFrontmatter {
			continue
		}

		if key, val, ok := parseYAMLLine(trimmed); ok {
			switch key {
			case "name":
				info.Name = val
			case "description":
				info.Description = val
			}
		}
	}

	if info.Name == "" {
		// Fall back to directory name from path.
		info.Name = filepath.Base(filepath.Dir(path))
	}

	return info, nil
}

// parseYAMLLine does simple "key: value" parsing for frontmatter.
func parseYAMLLine(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	// Strip surrounding quotes.
	value = strings.Trim(value, `"'`)
	return key, value, true
}
