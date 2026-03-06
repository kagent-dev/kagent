package config

import (
	"testing"
	"time"
)

func TestLoadArgs_Defaults(t *testing.T) {
	cfg, err := LoadArgs(nil)
	if err != nil {
		t.Fatalf("LoadArgs: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":8080")
	}
	if cfg.Transport != "http" {
		t.Errorf("Transport = %q, want %q", cfg.Transport, "http")
	}
	if cfg.TemporalHostPort != "temporal-server:7233" {
		t.Errorf("TemporalHostPort = %q, want %q", cfg.TemporalHostPort, "temporal-server:7233")
	}
	if cfg.TemporalNamespace != "kagent" {
		t.Errorf("TemporalNamespace = %q, want %q", cfg.TemporalNamespace, "kagent")
	}
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 5*time.Second)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestLoadArgs_FlagOverrides(t *testing.T) {
	args := []string{
		"--addr", ":9090",
		"--transport", "stdio",
		"--temporal-host-port", "localhost:7233",
		"--temporal-namespace", "test-ns",
		"--poll-interval", "10s",
		"--log-level", "debug",
	}
	cfg, err := LoadArgs(args)
	if err != nil {
		t.Fatalf("LoadArgs: %v", err)
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":9090")
	}
	if cfg.Transport != "stdio" {
		t.Errorf("Transport = %q, want %q", cfg.Transport, "stdio")
	}
	if cfg.TemporalHostPort != "localhost:7233" {
		t.Errorf("TemporalHostPort = %q, want %q", cfg.TemporalHostPort, "localhost:7233")
	}
	if cfg.TemporalNamespace != "test-ns" {
		t.Errorf("TemporalNamespace = %q, want %q", cfg.TemporalNamespace, "test-ns")
	}
	if cfg.PollInterval != 10*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 10*time.Second)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}

func TestLoadArgs_EnvOverride(t *testing.T) {
	t.Setenv("TEMPORAL_HOST_PORT", "env-host:7233")
	t.Setenv("TEMPORAL_NAMESPACE", "env-ns")

	cfg, err := LoadArgs(nil)
	if err != nil {
		t.Fatalf("LoadArgs: %v", err)
	}
	if cfg.TemporalHostPort != "env-host:7233" {
		t.Errorf("TemporalHostPort = %q, want %q", cfg.TemporalHostPort, "env-host:7233")
	}
	if cfg.TemporalNamespace != "env-ns" {
		t.Errorf("TemporalNamespace = %q, want %q", cfg.TemporalNamespace, "env-ns")
	}
}

func TestLoadArgs_InvalidPollInterval(t *testing.T) {
	args := []string{"--poll-interval", "not-a-duration"}
	cfg, err := LoadArgs(args)
	if err != nil {
		t.Fatalf("LoadArgs: %v", err)
	}
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want fallback %v", cfg.PollInterval, 5*time.Second)
	}
}
