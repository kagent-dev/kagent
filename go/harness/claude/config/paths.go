package config

import "path/filepath"

// SkillsRoot is the directory of selected skills in the generated Claude plugin.
func SkillsRoot(dataDir string) string {
	return filepath.Join(dataDir, "generated", "claude", ".claude", "skills")
}
