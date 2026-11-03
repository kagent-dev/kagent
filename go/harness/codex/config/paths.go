package config

import "path/filepath"

// SkillsRoot is the directory where Codex discovers installed skills.
func SkillsRoot(dataDir string) string {
	return filepath.Join(dataDir, "codex", "skills")
}
