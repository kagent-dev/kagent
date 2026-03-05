package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear any env vars that could interfere
	for _, key := range []string{"KANBAN_ADDR", "KANBAN_TRANSPORT", "KANBAN_DB_TYPE", "KANBAN_DB_PATH", "KANBAN_DB_URL", "KANBAN_LOG_LEVEL"} {
		os.Unsetenv(key)
	}

	cfg, err := LoadArgs([]string{})
	if err != nil {
		t.Fatalf("LoadArgs() error = %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Addr", cfg.Addr, ":8080"},
		{"Transport", cfg.Transport, "http"},
		{"DBType", string(cfg.DBType), "sqlite"},
		{"DBPath", cfg.DBPath, "./kanban.db"},
		{"DBURL", cfg.DBURL, ""},
		{"LogLevel", cfg.LogLevel, "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Config.%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	os.Setenv("KANBAN_ADDR", ":9090")
	defer os.Unsetenv("KANBAN_ADDR")

	cfg, err := LoadArgs([]string{})
	if err != nil {
		t.Fatalf("LoadArgs() error = %v", err)
	}

	if cfg.Addr != ":9090" {
		t.Errorf("Config.Addr = %q, want %q", cfg.Addr, ":9090")
	}
}
