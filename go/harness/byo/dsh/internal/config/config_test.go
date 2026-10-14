package config

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const valid = `{"model":{"type":"openai","model":"deepseek-chat","base_url":"https://api.deepseek.com/v1"},"instruction":"Answer accurately.","runtime":{"control_plane_url":"http://controller:8083"}}`

func TestParseRejectsUnsupportedSemantics(t *testing.T) {
	for _, test := range []struct{ name, field string }{
		{"missing model", `"model":null`},
		{"responses", `"model":{"type":"openai","model":"x","api_format":"responses"}`},
		{"generation overrides", `"model":{"type":"openai","model":"x","temperature":0.2}`},
		{"credential passthrough", `"model":{"type":"openai","model":"x","api_key_passthrough":true}`},
		{"model userinfo", `"model":{"type":"openai","model":"x","base_url":"https://key@example.com/v1"}`},
		{"subagents", `"sub_agents":[{}]`},
		{"plugins", `"agent_plugins":{}`},
		{"memory", `"memory":{}`},
		{"SSE", `"sse_tools":[{}]`},
		{"non-streaming", `"stream":false`},
		{"MCP tool subset", `"http_tools":[{"params":{"url":"https://tools.example/mcp"},"tools":["read"]}]`},
		{"MCP approval", `"http_tools":[{"params":{"url":"https://tools.example/mcp"},"require_approval":true}]`},
		{"MCP forwarded headers", `"http_tools":[{"params":{"url":"https://tools.example/mcp"},"allowed_headers":["authorization"]}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(valid), &fields))
			var change map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte("{"+test.field+"}"), &change))
			maps.Copy(fields, change)
			data, err := json.Marshal(fields)
			require.NoError(t, err)
			_, err = Parse(data)
			require.Error(t, err)
		})
	}
}

func TestMCPReferencesUseOnlyGatewayPlaceholders(t *testing.T) {
	c, err := Parse([]byte(valid))
	require.NoError(t, err)
	c.MCP = []MCPServer{{Type: "http", Name: "tools", URL: "https://tools.example/mcp", Headers: []Header{{Name: "Authorization", Value: "__KAGENT_ENV[KAGENT_CREDENTIAL_TEST]__"}}}}
	servers, err := c.Bindings([]string{"KAGENT_CREDENTIAL_TEST=kagent-credential-injected"})
	require.NoError(t, err)
	require.Equal(t, "kagent-credential-injected", servers[0].Headers[0].Value)
	require.Equal(t, "__KAGENT_ENV[KAGENT_CREDENTIAL_TEST]__", c.MCP[0].Headers[0].Value)
	for _, env := range [][]string{nil, {"KAGENT_CREDENTIAL_TEST=actual-secret"}} {
		_, err = c.Bindings(env)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "actual-secret")
	}
}

func TestPatchPreservesInstructionAndUsesDurableState(t *testing.T) {
	c, err := Parse([]byte(valid))
	require.NoError(t, err)
	c.Agent.Instruction = "quotes: \"\n!!js doNotExecute()\n$variable"
	data, err := c.Patch("/data")
	require.NoError(t, err)
	var rows []struct {
		ID     string          `json:"id"`
		Config json.RawMessage `json:"config"`
	}
	require.NoError(t, json.Unmarshal(data, &rows))
	for _, row := range rows {
		if row.ID == "system-prompt" {
			var p persona
			require.NoError(t, json.Unmarshal(row.Config, &p))
			require.Equal(t, c.Agent.Instruction, p.Prefix)
		}
		if row.ID == "session-persistence-jsonl" {
			require.JSONEq(t, `{"root":"/data/dsh/sessions"}`, string(row.Config))
		}
	}
	require.Contains(t, string(data), `"apiKeyEnv":"OPENAI_API_KEY"`)
}

func TestWholeHTTPMCPAndEnvironment(t *testing.T) {
	data := strings.TrimSuffix(valid, "}") + `,"http_tools":[{"params":{"url":"https://tools.example/mcp","headers":{"z":"last","a":"first"}}}]}`
	c, err := Parse([]byte(data))
	require.NoError(t, err)
	require.Equal(t, []Header{{Name: "a", Value: "first"}, {Name: "z", Value: "last"}}, c.MCP[0].Headers)
	env := Environment([]string{"NODE_EXTRA_CA_CERTS=/run/trust.pem", "OPENAI_API_KEY=placeholder", "DSH_HOME=/old", "DSH_TELEMETRY_DISABLED=0", "KAGENT_TOKEN=controller-token"}, "/tmp/dsh")
	require.Contains(t, env, "NODE_EXTRA_CA_CERTS=/run/trust.pem")
	require.Contains(t, env, "OPENAI_API_KEY=placeholder")
	require.NotContains(t, env, "DSH_HOME=/old")
	require.Contains(t, env, "DSH_TELEMETRY_DISABLED=1")
	require.NotContains(t, env, "KAGENT_TOKEN=controller-token")
}

func TestBindingsPreserveEmptyHeaderArray(t *testing.T) {
	c, err := Parse([]byte(strings.TrimSuffix(valid, "}") + `,"http_tools":[{"params":{"url":"https://tools.example/mcp"}}]}`))
	require.NoError(t, err)
	servers, err := c.Bindings(nil)
	require.NoError(t, err)
	data, err := json.Marshal(servers)
	require.NoError(t, err)
	require.Contains(t, string(data), `"headers":[]`)
}
