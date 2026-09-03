package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProductionRoundTrip(t *testing.T) {
	cfg := Production("gpt-5.2-codex", "work carefully")
	cfg.Provider = Provider{Name: "openai", BaseURL: "https://gateway.example.com/v1"}
	cfg.Agents = map[string]Agent{"reviewer": {Description: "Reviews", Instruction: "Review", Model: "gpt-5.2-codex"}}
	cfg.MCPServers = map[string]MCPServer{"tools": {URL: "https://mcp.example.com/mcp", EnabledTools: []string{"read"}}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ExpectedCodexVersion != PinnedCodexVersion || parsed.Provider.Name != "openai" {
		t.Fatalf("parsed config = %#v", parsed)
	}
}

func TestParseRejectsUnsafeConfiguration(t *testing.T) {
	base := Production("model", "instruction")
	base.Provider = Provider{Name: "openai"}
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"version", func(c *Config) { c.Version++ }, "unsupported config version"},
		{"provider", func(c *Config) { c.Provider.Name = "other" }, "unsupported Codex provider"},
		{"URL", func(c *Config) { c.Provider.BaseURL = "https://user:pass@example.com" }, "invalid provider base URL"},
		{"agent name", func(c *Config) {
			c.Agents = map[string]Agent{"../bad": {Description: "x", Instruction: "x", Model: "x"}}
		}, "agent name"},
		{"duplicate tool", func(c *Config) {
			c.MCPServers = map[string]MCPServer{"mcp": {URL: "https://example.com", EnabledTools: []string{"x", "x"}}}
		}, "duplicate enabled tool"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.mutate(&cfg)
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse() error = %v, want %q", err, test.want)
			}
		})
	}
	if _, err := Parse([]byte(`{"version":1,"unknown":true}`)); err == nil {
		t.Fatal("Parse() accepted an unknown field")
	}
}
