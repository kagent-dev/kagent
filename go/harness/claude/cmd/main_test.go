package main

import (
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
