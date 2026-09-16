package main

import (
	"context"
	"strings"
	"testing"
)

func TestRequiredEnvironment(t *testing.T) {
	values := map[string]string{configEnv: "  {\"version\":2}  "}
	got, err := requiredEnvironment(func(name string) string { return values[name] }, configEnv)
	if err != nil || string(got) != `{"version":2}` {
		t.Fatalf("requiredEnvironment() = %q, %v", got, err)
	}
	_, err = requiredEnvironment(func(string) string { return " " }, agentCardEnv)
	if err == nil || !strings.Contains(err.Error(), agentCardEnv) {
		t.Fatalf("requiredEnvironment() error = %v", err)
	}
}

func TestValidateSessionID(t *testing.T) {
	if err := validateSessionID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"); err != nil {
		t.Fatalf("validateSessionID() error = %v", err)
	}
	if err := validateSessionID("not-a-session"); err == nil {
		t.Fatal("validateSessionID() accepted an invalid UUID")
	}
}

func TestRunRejectsBadStartupInput(t *testing.T) {
	card := `{"name":"t"}`
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "relative durable dir", env: map[string]string{configEnv: `{}`, agentCardEnv: card, dataDirEnv: "relative/dir"}, wantErr: "absolute path"},
		{name: "malformed config", env: map[string]string{configEnv: `{"version":`, agentCardEnv: card, dataDirEnv: t.TempDir()}, wantErr: "parse " + configEnv},
		{name: "unknown config field", env: map[string]string{configEnv: `{"nope":1}`, agentCardEnv: card, dataDirEnv: t.TempDir()}, wantErr: "parse " + configEnv},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(context.Background(), true, func(k string) string { return tt.env[k] }, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("run() err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
