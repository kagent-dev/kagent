package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestProductionRoundTrip(t *testing.T) {
	cfg := Production("claude-test", "help")
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.ExpectedClaudeVersion != PinnedClaudeVersion || cfg.Model != "claude-test" || cfg.AppendSystemPrompt != "help" {
		t.Errorf("production config = %#v", cfg)
	}
}

func TestAgentsJSON(t *testing.T) {
	cfg := Production("claude-test", "help")
	cfg.Agents = map[string]Agent{
		"reviewer": {
			Description: "Reviews changes", Prompt: "Review carefully", Model: "claude-child",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := cfg.AgentsJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"reviewer":{"description":"Reviews changes","prompt":"Review carefully","model":"claude-child"}}`
	if raw != want {
		t.Fatalf("AgentsJSON() = %s, want %s", raw, want)
	}
	parsed, err := Parse([]byte(`{"version":2,"claude_executable":"claude","expected_claude_version":"2.1.217","strict_version":true,"agents":` + raw + `,"max_event_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed.Agents, cfg.Agents) {
		t.Fatalf("parsed agents = %#v, want %#v", parsed.Agents, cfg.Agents)
	}
}

func TestConfigRejectsInvalidAgents(t *testing.T) {
	tests := []map[string]Agent{
		{"": {Description: "description", Prompt: "prompt"}},
		{"not valid": {Description: "description", Prompt: "prompt"}},
		{"reviewer": {Prompt: "prompt"}},
	}
	for _, agents := range tests {
		cfg := Production("claude-test", "help")
		cfg.Agents = agents
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate() accepted agents %#v", agents)
		}
	}
}

func TestParseValidates(t *testing.T) {
	contents := `{"version":2,"claude_executable":"claude","expected_claude_version":"2.1.217","strict_version":true,"model":"claude-test","append_system_prompt":"help","max_event_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`
	cfg, err := Parse([]byte(contents))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "claude-test" || cfg.AppendSystemPrompt != "help" {
		t.Errorf("parsed config = %#v", cfg)
	}
}

func TestConfigRejectsUnknownFields(t *testing.T) {
	if _, err := Parse([]byte(`{"version":2,"surprise":true}`)); err == nil {
		t.Fatal("Parse() accepted an unknown field")
	}
}

func TestConfigRejectsTrailingValue(t *testing.T) {
	if _, err := Parse([]byte(`{} {}`)); err == nil {
		t.Fatal("Parse() accepted a trailing JSON value")
	}
}

func TestConfigRejectsMissingLimits(t *testing.T) {
	_, err := Parse([]byte(`{"version":2,"claude_executable":"claude"}`))
	if err == nil || !strings.Contains(err.Error(), "limits must be positive") {
		t.Fatalf("Parse() error = %v", err)
	}
}
