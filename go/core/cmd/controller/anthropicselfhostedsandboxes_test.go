package main

import (
	"bytes"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckConfigContinuesWithInvalidAnthropicSelfHostedSandboxes(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "database-url"), []byte("postgres://localhost/kagent"), 0o600))
	path := filepath.Join(dir, "controller.yaml")
	require.NoError(t, os.WriteFile(path, []byte("database: {owner: {urlFile: database-url, role: kagent_owner}, readWrite: {urlFile: database-url, role: kagent_readwrite}}\n"), 0o600))
	t.Setenv("KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_ENABLED", "true")
	t.Setenv("ANTHROPIC_ENVIRONMENT_ID", "")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	require.NoError(t, run(path, true, ""))
	assert.Contains(t, logs.String(), "level=ERROR")
	assert.Contains(t, logs.String(), "anthropic self-hosted sandboxes disabled")
	assert.Contains(t, logs.String(), "environment ID")
}

func TestAnthropicSelfHostedSandboxesConfiguration(t *testing.T) {
	base := map[string]string{
		"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_ENABLED":            "true",
		"ANTHROPIC_ENVIRONMENT_ID":                                  "env_test",
		"ANTHROPIC_ENVIRONMENT_KEY":                                 "standing-secret",
		"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TEMPLATE_NAMESPACE": "test",
		"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TEMPLATE_NAME":      "claude",
	}
	for _, tt := range []struct {
		name      string
		changes   map[string]string
		wantError string
		enabled   bool
		wantTTL   time.Duration
	}{
		{name: "enabled", enabled: true, wantTTL: 12 * time.Hour},
		{name: "custom sandbox TTL", enabled: true, changes: map[string]string{"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TTL": " 2h30m "}, wantTTL: 150 * time.Minute},
		{name: "invalid sandbox TTL", changes: map[string]string{"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TTL": "forever"}, wantError: "expected duration"},
		{name: "zero sandbox TTL", changes: map[string]string{"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TTL": "0"}, wantError: "sandbox TTL must be positive"},
		{name: "negative sandbox TTL", changes: map[string]string{"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TTL": "-1h"}, wantError: "sandbox TTL must be positive"},
		{name: "disabled ignores incomplete config", changes: map[string]string{"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_ENABLED": "false", "ANTHROPIC_ENVIRONMENT_ID": ""}},
		{name: "invalid boolean", changes: map[string]string{"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_ENABLED": "yes"}, wantError: "expected boolean"},
		{name: "missing key", changes: map[string]string{"ANTHROPIC_ENVIRONMENT_KEY": ""}, wantError: "environment key"},
		{name: "missing template", changes: map[string]string{"KAGENT_ANTHROPIC_SELF_HOSTED_SANDBOXES_TEMPLATE_NAME": ""}, wantError: "template name"},
		{name: "Anthropic URL query", changes: map[string]string{"ANTHROPIC_BASE_URL": "https://example.com/?token=secret"}, wantError: "base URL"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			values := maps.Clone(base)
			maps.Copy(values, tt.changes)
			cfg, err := loadAnthropicSelfHostedSandboxesConfig(func(key string) string { return values[key] })
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				assert.NotContains(t, err.Error(), "standing-secret")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.enabled, cfg != nil)
			if cfg != nil {
				assert.Equal(t, "env_test", cfg.EnvironmentID)
				assert.Equal(t, tt.wantTTL, cfg.SandboxTTL)
			}
		})
	}
	cfg, err := loadAnthropicSelfHostedSandboxesConfig(func(string) string { return "" })
	require.NoError(t, err)
	assert.Nil(t, cfg)
}
