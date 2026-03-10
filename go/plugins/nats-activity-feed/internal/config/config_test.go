package config

import (
	"testing"
)

func TestLoadArgs_Defaults(t *testing.T) {
	cfg, err := LoadArgs([]string{})
	if err != nil {
		t.Fatalf("LoadArgs() error = %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"NATSAddr", cfg.NATSAddr, "nats://localhost:4222"},
		{"Addr", cfg.Addr, ":8090"},
		{"Subject", cfg.Subject, "agent.>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Config.%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}

	if cfg.BufferSize != 100 {
		t.Errorf("Config.BufferSize = %d, want 100", cfg.BufferSize)
	}
}

func TestLoadArgs_Flags(t *testing.T) {
	args := []string{
		"--nats-addr", "nats://custom:4222",
		"--addr", ":9090",
		"--buffer-size", "50",
		"--subject", "test.>",
	}
	cfg, err := LoadArgs(args)
	if err != nil {
		t.Fatalf("LoadArgs() error = %v", err)
	}

	if cfg.NATSAddr != "nats://custom:4222" {
		t.Errorf("NATSAddr = %q, want %q", cfg.NATSAddr, "nats://custom:4222")
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":9090")
	}
	if cfg.BufferSize != 50 {
		t.Errorf("BufferSize = %d, want 50", cfg.BufferSize)
	}
	if cfg.Subject != "test.>" {
		t.Errorf("Subject = %q, want %q", cfg.Subject, "test.>")
	}
}

func TestLoadArgs_EnvVars(t *testing.T) {
	t.Setenv("NATS_ADDR", "nats://env:4222")
	t.Setenv("ACTIVITY_FEED_ADDR", ":7070")
	t.Setenv("ACTIVITY_FEED_BUFFER", "200")
	t.Setenv("ACTIVITY_FEED_SUBJECT", "env.>")

	cfg, err := LoadArgs([]string{})
	if err != nil {
		t.Fatalf("LoadArgs() error = %v", err)
	}

	if cfg.NATSAddr != "nats://env:4222" {
		t.Errorf("NATSAddr = %q, want %q", cfg.NATSAddr, "nats://env:4222")
	}
	if cfg.Addr != ":7070" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":7070")
	}
	if cfg.BufferSize != 200 {
		t.Errorf("BufferSize = %d, want 200", cfg.BufferSize)
	}
	if cfg.Subject != "env.>" {
		t.Errorf("Subject = %q, want %q", cfg.Subject, "env.>")
	}
}
