package skillsinit

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
)

// MergeDockerConfigs reads each /<secretsDir>/<name>/.dockerconfigjson, merges
// their auths maps into a single config, and writes it to outPath. Returns
// outPath's parent dir so callers can set DOCKER_CONFIG.
//
// Missing per-secret files are skipped silently (matches the old script
// behavior, where a misconfigured secret is non-fatal). A malformed file is
// an error.
func MergeDockerConfigs(secretsDir string, secretNames []string, outPath string) (dockerConfigDir string, err error) {
	merged := map[string]any{"auths": map[string]any{}}

	for _, name := range secretNames {
		path := filepath.Join(secretsDir, name, ".dockerconfigjson")
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return "", fmt.Errorf("read %s: %w", path, readErr)
		}
		var parsed struct {
			Auths map[string]any `json:"auths"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return "", fmt.Errorf("parse %s: %w", path, err)
		}
		dst := merged["auths"].(map[string]any)
		maps.Copy(dst, parsed.Auths)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return "", fmt.Errorf("mkdir docker config: %w", err)
	}
	buf, err := json.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("marshal docker config: %w", err)
	}
	if err := os.WriteFile(outPath, buf, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", outPath, err)
	}
	return filepath.Dir(outPath), nil
}
