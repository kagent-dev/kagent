package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	// No flags and no env vars: every field should fall back to its default.
	t.Setenv(envAddr, "")
	t.Setenv(envTransport, "")
	t.Setenv(envDBURL, "")
	t.Setenv(envDBURLFile, "")
	t.Setenv(envLogLevel, "")

	cfg, err := loadArgs("kanban-mcp", nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "Addr", got: cfg.Addr, want: defaultAddr},
		{name: "Transport", got: cfg.Transport, want: defaultTransport},
		{name: "DBURL", got: cfg.DBURL, want: ""},
		{name: "DBURLFile", got: cfg.DBURLFile, want: ""},
		{name: "LogLevel", got: cfg.LogLevel, want: defaultLogLevel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
		envVal string
		field  func(*Config) string
		want   string
	}{
		{name: "addr from env", envKey: envAddr, envVal: ":9090", field: func(c *Config) string { return c.Addr }, want: ":9090"},
		{name: "transport from env", envKey: envTransport, envVal: "http", field: func(c *Config) string { return c.Transport }, want: "http"},
		{name: "db-url from env", envKey: envDBURL, envVal: "postgres://x", field: func(c *Config) string { return c.DBURL }, want: "postgres://x"},
		{name: "db-url-file from env", envKey: envDBURLFile, envVal: "/run/secret", field: func(c *Config) string { return c.DBURLFile }, want: "/run/secret"},
		{name: "log-level from env", envKey: envLogLevel, envVal: "debug", field: func(c *Config) string { return c.LogLevel }, want: "debug"},
		{name: "boards from env", envKey: envBoards, envVal: `[{"key":"a","columns":["X"]}]`, field: func(c *Config) string { return c.Boards }, want: `[{"key":"a","columns":["X"]}]`},
		{name: "boards-file from env", envKey: envBoardsFile, envVal: "/etc/kanban/boards/boards.json", field: func(c *Config) string { return c.BoardsFile }, want: "/etc/kanban/boards/boards.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envVal)
			cfg, err := loadArgs("kanban-mcp", nil)
			if err != nil {
				t.Fatalf("loadArgs() error = %v", err)
			}
			if got := tt.field(cfg); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestLoad_Readonly(t *testing.T) {
	tests := []struct {
		name   string
		envVal string
		args   []string
		want   bool
	}{
		{name: "default off", want: false},
		{name: "env true", envVal: "true", want: true},
		{name: "env 1", envVal: "1", want: true},
		{name: "env false", envVal: "false", want: false},
		{name: "flag true", args: []string{"--readonly"}, want: true},
		{name: "flag beats env", envVal: "true", args: []string{"--readonly=false"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envReadonly, tt.envVal)
			cfg, err := loadArgs("kanban-mcp", tt.args)
			if err != nil {
				t.Fatalf("loadArgs() error = %v", err)
			}
			if cfg.Readonly != tt.want {
				t.Errorf("Readonly = %t, want %t", cfg.Readonly, tt.want)
			}
		})
	}
}

func TestLoad_FlagBeatsEnv(t *testing.T) {
	// An explicitly-set flag must win over the environment variable.
	t.Setenv(envAddr, ":7070")
	cfg, err := loadArgs("kanban-mcp", []string{"--addr=:6060"})
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.Addr != ":6060" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":6060")
	}
}
