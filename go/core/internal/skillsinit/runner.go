package skillsinit

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Run executes the full skills-init sequence: docker config merge → git auth
// setup → git clones → OCI pulls. It returns the first error encountered;
// successful operations before the failure are left in place on disk (the
// container restarts and re-runs from scratch).
//
// homeDir is the binary's $HOME — exposed for tests. In production callers
// should pass os.UserHomeDir() or "/root".
func Run(cfg Config, homeDir string) error {
	if len(cfg.ImagePullSecrets) > 0 {
		dockerCfgPath := filepath.Join(os.TempDir(), "kagent-docker-config", "config.json")
		dockerCfgDir, err := MergeDockerConfigs(DockerSecretsDir, cfg.ImagePullSecrets, dockerCfgPath)
		if err != nil {
			return fmt.Errorf("merge docker configs: %w", err)
		}
		if err := os.Setenv("DOCKER_CONFIG", dockerCfgDir); err != nil {
			return err
		}
	}

	if err := SetupGitAuth(homeDir, cfg.AuthMountPath, cfg.SSHHosts); err != nil {
		return fmt.Errorf("setup git auth: %w", err)
	}

	for _, ref := range cfg.GitRefs {
		log.Printf("cloning %s (ref=%s) into %s", ref.URL, ref.Ref, ref.Dest)
		if err := CloneGit(ref); err != nil {
			return fmt.Errorf("clone %s: %w", ref.URL, err)
		}
	}

	for _, ref := range cfg.OCIRefs {
		log.Printf("exporting OCI image %s into %s", ref.Image, ref.Dest)
		if err := FetchOCI(ref, cfg.InsecureOCI); err != nil {
			return fmt.Errorf("oci %s: %w", ref.Image, err)
		}
	}

	return nil
}

// LoadConfig reads and parses the config JSON from the conventional mount.
func LoadConfig() (Config, error) {
	path := filepath.Join(ConfigMountPath, ConfigFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}
