package config

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var environment = map[string]string{"KAGENT_CREDENTIAL_A": "placeholder-a", "KAGENT_CREDENTIAL_B": "placeholder-b"}

func lookupEnv(name string) (string, bool) {
	value, ok := environment[name]
	return value, ok
}

func anthropic() map[string]any {
	return map[string]any{"type": "anthropic", "model": "claude-sonnet-4-5"}
}

func agent(fields map[string]any) []byte {
	document := map[string]any{"description": "d", "instruction": "Be brief.", "model": anthropic()}
	maps.Copy(document, fields)
	raw, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}
	return raw
}

func TestParse(t *testing.T) {
	model := anthropic()
	model["base_url"] = "https://gateway.example.com/anthropic/"
	model["headers"] = map[string]string{"X-Team": "platform", "X-Key": "__KAGENT_ENV[KAGENT_CREDENTIAL_A]__"}
	model["max_tokens"] = 4096
	cfg, err := Parse(agent(map[string]any{
		"model": model,
		"http_tools": []map[string]any{
			{
				"params": map[string]any{
					"url":     "https://muster.example.com/mcp",
					"headers": map[string]string{"Authorization": "__KAGENT_ENV[KAGENT_CREDENTIAL_B]__", "X-Other": "__KAGENT_ENV[KAGENT_CREDENTIAL_A]__", "X-Plain": "v"},
					"timeout": 12.5,
				},
				"tools": []string{"list", "get"},
			},
			{"params": map[string]any{"url": "http://muster.other.example.com/mcp"}},
			{"params": map[string]any{"url": "http://10.0.0.1:8080/mcp"}},
		},
	}), lookupEnv)
	require.NoError(t, err)

	assert.Equal(t, Model{
		ID: "claude-sonnet-4-5", BaseURL: "https://gateway.example.com/anthropic",
		Headers: map[string]string{"X-Team": "platform", "X-Key": "placeholder-a"}, MaxTokens: 4096,
	}, cfg.Model)
	assert.Equal(t, "Be brief.", cfg.Instruction)
	require.Len(t, cfg.MCPServers, 3)
	assert.Equal(t, MCPServer{
		Name: "muster", URL: "https://muster.example.com/mcp",
		Headers:         map[string]string{"Authorization": "placeholder-b", "X-Other": "placeholder-a", "X-Plain": "v"},
		HeaderVariables: []string{"KAGENT_CREDENTIAL_A", "KAGENT_CREDENTIAL_B"},
		Tools:           []string{"list", "get"}, TimeoutSeconds: 12.5,
	}, cfg.MCPServers[0])
	assert.Equal(t, "muster_2", cfg.MCPServers[1].Name, "a second server on the same host label gets a suffix")
	assert.Equal(t, "10", cfg.MCPServers[2].Name)
}

func TestParseDefaultsTheAnthropicEndpoint(t *testing.T) {
	cfg, err := Parse(agent(nil), lookupEnv)
	require.NoError(t, err)
	assert.Equal(t, DefaultAnthropicBaseURL, cfg.Model.BaseURL)
	assert.Empty(t, cfg.MCPServers)
}

func TestParseRefuses(t *testing.T) {
	withModel := func(name string, value any) map[string]any {
		model := anthropic()
		model[name] = value
		return map[string]any{"model": model}
	}
	server := func(params map[string]any, extra map[string]any) map[string]any {
		entry := map[string]any{"params": params}
		maps.Copy(entry, extra)
		return map[string]any{"http_tools": []map[string]any{entry}}
	}
	url := "https://mcp.example.com/mcp"
	for name, test := range map[string]struct {
		fields map[string]any
		want   string
	}{
		"openai model":     {map[string]any{"model": map[string]any{"type": "openai", "model": "gpt"}}, `needs an Anthropic ModelConfig, got "openai"`},
		"no model name":    {withModel("model", " "), "names no model"},
		"key passthrough":  {withModel("api_key_passthrough", true), "API key passthrough"},
		"model TLS":        {withModel("tls_insecure_skip_verify", true), "custom model TLS"},
		"model URL":        {withModel("base_url", "gateway:443"), "model base URL must be an absolute HTTP(S) URL"},
		"unset variable":   {withModel("headers", map[string]string{"X": "__KAGENT_ENV[MISSING]__"}), "environment variable MISSING is not set"},
		"SSE server":       {map[string]any{"sse_tools": []map[string]any{{"params": map[string]any{"url": url}}}}, "SSE MCP servers"},
		"stdio server":     {map[string]any{"stdio_tools": []map[string]any{{"params": map[string]any{"command": "x"}}}}, "stdio MCP servers"},
		"sub agent":        {map[string]any{"sub_agents": []map[string]any{{"description": "s", "instruction": "s", "model": anthropic()}}}, "agent bindings"},
		"memory":           {map[string]any{"memory": map[string]any{}}, "memory is not supported"},
		"approval":         {server(map[string]any{"url": url}, map[string]any{"require_approval": true}), "tool approval"},
		"server TLS":       {server(map[string]any{"url": url, "tls_insecure_skip_verify": true}, nil), "custom TLS"},
		"server URL":       {server(map[string]any{"url": "file:///etc/passwd"}, nil), "MCP server 0 URL must be an absolute HTTP(S) URL"},
		"plugins":          {map[string]any{"agent_plugins": map[string]any{"plugins": []map[string]any{{"name": "p", "ref": "oci://r/p:1"}}}}, "plugins are not supported"},
		"malformed config": {map[string]any{"http_tools": "nope"}, "decode agent configuration"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(agent(test.fields), lookupEnv)
			assert.ErrorContains(t, err, test.want)
		})
	}
}
