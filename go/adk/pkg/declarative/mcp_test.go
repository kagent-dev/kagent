package declarative

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const head = "apiVersion: adk.declarative/v0.2\nkind: AgentSystem\n"

const crmKey = "secret-key"

// crmServer is a remote MCP server reached over streamable HTTP. It rejects
// requests without the X-Api-Key header and counts tool calls.
type crmServer struct {
	URL   string
	calls atomic.Int32
}

func startCRM(t testing.TB) *crmServer {
	t.Helper()
	crm := &crmServer{}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "crm", Version: "v1"}, nil)
	type lookupIn struct {
		CustomerID string `json:"customer_id"`
	}
	type customer struct {
		ID      string `json:"id"`
		Plan    string `json:"plan"`
		Balance int    `json:"balance"`
	}
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "lookup_customer", Description: "Looks up a customer by id."},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in lookupIn) (*mcpsdk.CallToolResult, customer, error) {
			crm.calls.Add(1)
			balance := 120
			if in.CustomerID == "c-13" {
				balance = 50
			}
			return nil, customer{ID: in.CustomerID, Plan: "pro", Balance: balance}, nil
		})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "delete_customer", Description: "Deletes a customer."},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in lookupIn) (*mcpsdk.CallToolResult, any, error) {
			crm.calls.Add(1)
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "deleted " + in.CustomerID}}}, nil, nil
		})
	mcp := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != crmKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mcp.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	crm.URL = srv.URL
	return crm
}

const crmServers = `
mcp_servers:
  - name: crm
    url_from: { env: CRM_MCP_URL }
    headers:
      - { name: X-Api-Key, value_from: { env: CRM_API_KEY } }
    tools: [lookup_customer, delete_customer]
`

func buildDoc(t *testing.T, reg *Registry, doc string) *System {
	t.Helper()
	spec, err := reg.Parse([]byte(doc))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sys.Close()) })
	return sys
}

func replyText(events []*session.Event) string {
	var sb strings.Builder
	for _, ev := range events {
		if ev.Content != nil && !ev.Partial {
			for _, p := range ev.Content.Parts {
				sb.WriteString(p.Text)
			}
		}
	}
	return sb.String()
}

// TestRemoteMCPToolNode runs Declarative + CEL + remote MCP end to end: CEL
// extracts the arguments, a tool node calls the MCP server, CEL renders the
// structured result with context variables.
func TestRemoteMCPToolNode(t *testing.T) {
	crm := startCRM(t)
	t.Setenv("CRM_MCP_URL", crm.URL)
	t.Setenv("CRM_API_KEY", crmKey)
	sys := buildDoc(t, NewRegistry(), head+crmServers+`
agents:
  - name: billing
    agent_class: Workflow
    nodes:
      - id: args
        type: cel
        output: '{"customer_id": regex.extract(user.content, r"c-[0-9]+").orValue("unknown")}'
      - { id: lookup, type: tool, server: crm, tool: lookup_customer, timeout: 10s }
      - id: reply
        type: cel
        output: |
          '%s: %s is on %s, balance %d (%s)'.format([invocation.agent_name, input.output.id,
            input.output.plan, input.output.balance, toJson(input.output.plan)])
        state_delta:
          plan: input.output.plan
    edges:
      - { from: START, to: args }
      - { from: args, to: lookup }
      - { from: lookup, to: reply }
`)
	svc, id := newSession(t)
	events := run(t, sys.Root(), genai.NewContentFromText("why was c-42 charged?", genai.RoleUser), svc, id)
	require.Contains(t, replyText(events), `billing: c-42 is on pro, balance 120 ("pro")`)
	require.EqualValues(t, 1, crm.calls.Load())
	got, err := svc.Get(t.Context(), &session.GetRequest{AppName: "test", UserID: "user", SessionID: id})
	require.NoError(t, err)
	plan, err := got.Session.State().Get("plan")
	require.NoError(t, err)
	require.Equal(t, "pro", plan)
}

func TestRemoteMCPUnreachableFailsTheRunNotTheBuild(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	sys := buildDoc(t, NewRegistry(), head+`
mcp_servers:
  - { name: crm, url: "`+srv.URL+`" }
agents:
  - name: wf
    agent_class: Workflow
    nodes:
      - { id: lookup, type: tool, server: crm, tool: lookup_customer }
      - { id: done, type: cel, output: "'ok'" }
    edges:
      - { from: START, to: lookup }
      - { from: lookup, to: done }
`)
	svc, id := newSession(t)
	_, err := tryRun(t, &System{root: sys.Root()}, svc, id, "hi")
	require.ErrorContains(t, err, `failed to list tools of "crm"`)
}

// toolCaller calls tool with the customer id of the user message, then
// answers with the tool response it receives.
type toolCaller struct{ tool string }

func (m toolCaller) Name() string { return "tool_caller" }

func (m toolCaller) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		last := req.Contents[len(req.Contents)-1]
		for _, p := range last.Parts {
			if p.FunctionResponse != nil {
				data, _ := json.Marshal(p.FunctionResponse.Response)
				yield(&adkmodel.LLMResponse{Content: genai.NewContentFromText("tool said "+string(data), genai.RoleModel), TurnComplete: true}, nil)
				return
			}
		}
		id := regexp.MustCompile(`c-[0-9]+`).FindString(lastUserText(req))
		yield(&adkmodel.LLMResponse{
			Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
				ID: "call-1", Name: m.tool, Args: map[string]any{"customer_id": id},
			}}}},
			TurnComplete: true,
		}, nil)
	}
}

// TestHooksGuardModelAndRemoteMCPTools covers accept and reject on every
// callback point of an LLM agent whose tools come from a remote MCP server.
func TestHooksGuardModelAndRemoteMCPTools(t *testing.T) {
	crm := startCRM(t)
	t.Setenv("CRM_MCP_URL", crm.URL)
	t.Setenv("CRM_API_KEY", crmKey)
	t.Setenv("ASSISTANT_PROMPT", "You are the CRM assistant.")

	newReg := func(tool string) *Registry {
		reg := NewRegistry()
		require.NoError(t, reg.RegisterModel("caller", toolCaller{tool: tool}))
		require.NoError(t, reg.RegisterHook("vip_only", func(_ agent.Context, ev *HookEvent) (Decision, error) {
			if ev.Args["customer_id"] == "c-1" {
				return Decision{Verdict: Accept}, nil
			}
			return Decision{Verdict: Continue}, nil
		}))
		return reg
	}
	const agentDoc = `
agents:
  - name: assistant
    agent_class: LlmAgent
    model: caller
    instruction_from: { env: ASSISTANT_PROMPT }
    tools: [crm]
    callbacks:
      before_model:
        - when: request.text.matches(r'(?i)password')
          action: reject
          message: I cannot help with credentials.
        - when: "!request.system_instruction.contains('CRM assistant')"
          action: reject
          message: missing prompt
      before_tool:
        - function: vip_only
        - when: tool.name == 'delete_customer'
          action: reject
          message: deleting customers is not allowed
        - when: tool.args.customer_id == ''
          action: reject
          message: a customer id is required
      after_tool:
        - when: "tool.result.?output.?balance.orValue(0) > 100 && state[?'role'].orValue('') != 'admin'"
          action: reject
          message: balance hidden
      after_model:
        - when: response.text.contains('c-13')
          action: reject
          message: redacted
`
	for _, tc := range []struct {
		name, tool, input string
		want              string
		calls             int32
	}{
		{"before_model rejects without calling the model", "lookup_customer", "reset my password for c-42", "I cannot help with credentials.", 0},
		{"before_tool rejects a tool by name", "delete_customer", "remove c-42", "deleting customers is not allowed", 0},
		{"before_tool rejects missing arguments", "lookup_customer", "who am I", "a customer id is required", 0},
		{"a function hook accepts and stops the chain", "delete_customer", "remove c-1", `deleted c-1`, 1},
		{"after_tool rejects the remote result", "lookup_customer", "lookup c-42", "balance hidden", 1},
		{"after_model rejects the reply", "lookup_customer", "c-13 please", "redacted", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := crm.calls.Load()
			sys := buildDoc(t, newReg(tc.tool), head+crmServers+agentDoc)
			svc, id := newSession(t)
			reply := replyText(run(t, sys.Root(), genai.NewContentFromText(tc.input, genai.RoleUser), svc, id))
			require.Contains(t, reply, tc.want)
			require.Equal(t, tc.calls, crm.calls.Load()-before, "remote tool calls")
		})
	}
}

func TestPromptSources(t *testing.T) {
	const doc = head + `
agents:
  - name: a
    agent_class: LlmAgent
    model: stub
    instruction_from: %s
`
	reg := NewRegistry()
	_, err := reg.Parse([]byte(strings.Replace(doc, "%s", "{ config_map: { name: prompts, key: a.md } }", 1)))
	require.ErrorContains(t, err, "must be resolved by the kagent controller")
	_, err = reg.Parse([]byte(strings.Replace(doc, "%s", "{ env: A, secret: { name: s, key: k } }", 1)))
	require.ErrorContains(t, err, "exactly one of config_map, secret, env")

	spec, err := decode([]byte(strings.Replace(doc, "%s", "{ config_map: { name: prompts, key: a.md } }", 1)))
	require.NoError(t, err)
	require.NoError(t, Validate(spec), "document-only validation accepts Kubernetes references")
	var paths []string
	require.NoError(t, ResolvePrompts(spec, func(path string, src TextSource) (string, *TextSource, error) {
		paths = append(paths, path)
		require.Equal(t, "prompts", src.ConfigMap.Name)
		return "Be brief.", nil, nil
	}))
	require.Equal(t, []string{"agents[0].instruction_from"}, paths)
	require.Equal(t, "Be brief.", spec.Agents[0].Instruction)
	require.Nil(t, spec.Agents[0].InstructionFrom)
	require.NoError(t, reg.Validate(spec))

	spec, err = reg.Parse([]byte(strings.Replace(doc, "%s", "{ env: MISSING_PROMPT_VAR }", 1)))
	require.NoError(t, err)
	_, err = Build(t.Context(), spec, reg)
	require.ErrorContains(t, err, `prompt environment variable "MISSING_PROMPT_VAR" is not set`)
}

func TestMCPSourceValidation(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"URL ConfigMap", "url_from: { config_map: { name: endpoints, key: crm } }", "mcp_servers[0].url_from: must use env"},
		{"header ConfigMap", "url: http://crm, headers: [{ name: Authorization, value_from: { config_map: { name: credentials, key: token } } }]", "mcp_servers[0].headers[0].value_from: must use env"},
		{"header Secret", "url: http://crm, headers: [{ name: Authorization, value_from: { secret: { name: credentials, key: token } } }]", "mcp_servers[0].headers[0].value_from: must use env"},
		{"invalid env name", "url: http://crm, headers: [{ name: Authorization, value_from: { env: BAD-NAME } }]", "mcp_servers[0].headers[0].value_from.env"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := head + "mcp_servers: [{ name: crm, " + tc.source + " }]\nagents: [{ name: a, agent_class: LlmAgent, model: stub, instruction: hi }]\n"
			_, err := Parse([]byte(doc))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestMCPAndHookValidation(t *testing.T) {
	reg := NewRegistry()
	for _, tc := range []struct{ body, want string }{
		{`
mcp_servers: [{ name: crm, url: "ftp://x" }]
agents: [{ name: a, agent_class: LlmAgent, model: stub, instruction: hi, tools: [crm] }]`, "must be an absolute http or https URL"},
		{`
mcp_servers: [{ name: crm, url: "http://x", url_from: { env: X } }]
agents: [{ name: a, agent_class: LlmAgent, model: stub, instruction: hi }]`, "exactly one of url, url_from"},
		{`
mcp_servers: [{ name: crm, url: "http://x", headers: [{ name: "bad header", value: v }] }]
agents: [{ name: a, agent_class: LlmAgent, model: stub, instruction: hi }]`, "is not an HTTP header name"},
		{`
mcp_servers: [{ name: crm, url: "http://x", tools: [lookup_customer] }]
agents:
  - name: w
    agent_class: Workflow
    nodes: [{ id: t, type: tool, server: crm, tool: delete_customer }, { id: d, type: cel, output: "'x'" }]
    edges: [{ from: START, to: t }, { from: t, to: d }]`, `tool "delete_customer" is not in the tools of mcp server "crm"`},
		{`
agents:
  - name: w
    agent_class: Workflow
    nodes: [{ id: t, type: tool, server: ghost, tool: x }, { id: d, type: cel, output: "'x'" }]
    edges: [{ from: START, to: t }, { from: t, to: d }]`, `server "ghost" is not declared in mcp_servers`},
		{`
agents:
  - name: a
    agent_class: LlmAgent
    model: stub
    instruction: hi
    callbacks: { before_tool: [{ action: reject }] }`, "a reject action requires a message"},
		{`
agents:
  - name: a
    agent_class: LlmAgent
    model: stub
    instruction: hi
    callbacks: { before_model: [{ when: "tool.name == 'x'", action: accept }] }`, "undeclared reference to 'tool'"},
		{`
agents:
  - name: a
    agent_class: LlmAgent
    model: stub
    instruction: hi
    callbacks: { after_tool: [{ function: ghost }] }`, `hook "ghost" is not registered`},
		{`
agents:
  - name: w
    agent_class: Workflow
    nodes: [{ id: c, type: cel, route: "1 + 1" }]
    edges: [{ from: START, to: c }]`, "want string or list(string)"},
	} {
		_, err := reg.Parse([]byte(head + tc.body))
		require.ErrorContains(t, err, tc.want, tc.body)
	}
}

func TestCELContextAndLibrary(t *testing.T) {
	sys := buildDoc(t, NewRegistry(), head+`
agents:
  - name: ctx_wf
    agent_class: Workflow
    nodes:
      - id: seed
        type: cel
        state_delta:
          tags: "['b', 'a', 'b']"
          raw: '''{"n": 2, "items": [1, 2, 3]}'''
      - id: show
        type: cel
        output: |
          cel.bind(doc, json(state.raw), [
            session.app_name, session.user_id, user.id, invocation.agent_name,
            string(size(session.id) > 0), string(size(invocation.id) > 0),
            string(math.greatest(doc.items)), string(doc.n * 2),
            sets.contains(state.tags, ['a']) ? 'has-a' : 'no-a',
            state.tags.map(t, string(t)).distinct().sort().join('+'),
            string(state[?'missing'].orValue('none')),
            toJson({'k': [1, 'x']}),
          ].join(' '))
    edges:
      - { from: START, to: seed }
      - { from: seed, to: show }
`)
	svc, id := newSession(t)
	reply := replyText(run(t, sys.Root(), genai.NewContentFromText("hi", genai.RoleUser), svc, id))
	require.Contains(t, reply, `test user user ctx_wf true true 3 4 has-a a+b none {"k":[1,"x"]}`)
}
