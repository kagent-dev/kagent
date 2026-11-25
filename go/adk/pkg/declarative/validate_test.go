package declarative

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// llmAgent is a minimal valid LlmAgent entry with extra flow-mapping fields.
func llmAgent(name, extra string) string {
	return "  - { name: " + name + ", agent_class: LlmAgent, instruction: i" + extra + " }\n"
}

func startTo(id string) string { return "[{ from: START, to: " + id + " }]" }

func TestValidate(t *testing.T) {
	t.Run("Given the showcase document, when it is parsed, assert that it is valid", func(t *testing.T) {
		spec, err := Parse(readShowcase(t))
		require.NoError(t, err)
		require.Len(t, spec.Agents, 5)
	})

	scenarios := []struct {
		name string
		doc  string
		want []string
	}{
		{"a foreign apiVersion and kind", "apiVersion: v1\nkind: Pod\nagents:\n" + llmAgent("a", ""),
			[]string{`apiVersion: must be "adk.declarative/v0alpha1"`, `kind: must be "AgentSystem"`}},
		{"no agents", docHeader + "  []\n", []string{"agents: at least one agent is required"}},
		{"malformed and reserved agent names", docHeader + llmAgent("'a b'", "") + llmAgent("user", ""),
			[]string{"agents[0].name:", `agents[1].name: "user" is reserved`}},
		{"a duplicate agent", docHeader + llmAgent("a", "") + llmAgent("a", ""),
			[]string{`agents[1].name: duplicate agent name "a"`}},
		{"an excluded agent class", docHeader + "  - { name: a, agent_class: SequentialAgent, sub_agents: [b] }\n" + llmAgent("b", ""),
			[]string{`agents[0].agent_class: must be one of LlmAgent, Workflow, got "SequentialAgent"`}},
		{"llm fields on a workflow", docHeader + "  - { name: a, agent_class: Workflow, instruction: i, nodes: [{ id: n, type: function, function: f }], edges: [{ from: START, to: n }] }\n",
			[]string{"agents[0].instruction: not allowed on agent_class Workflow"}},
		{"workflow fields on an llm agent", docHeader + llmAgent("a", ", entry: x"),
			[]string{"agents[0].entry: not allowed on agent_class LlmAgent"}},
		{"an llm agent without instruction", docHeader + "  - { name: a, agent_class: LlmAgent }\n",
			[]string{"agents[0].instruction: required for agent_class LlmAgent"}},
		{"a provider model name", docHeader + llmAgent("a", ", model: gpt-4o"),
			[]string{`agents[0].model: "gpt-4o" is not accepted; omit model`}},
		{"a bad mode, include_contents and duplicate tool", docHeader + llmAgent("a", ", mode: weird, include_contents: all, tools: [t, t]"),
			[]string{"agents[0].mode: must be one of", "agents[0].include_contents: must be default or none", `agents[0].tools[1]: duplicate tool "t"`}},
		{"an invalid output_schema", docHeader + llmAgent("a", ", output_schema: { type: 12 }"),
			[]string{"agents[0].output_schema:"}},
		{"unknown and self sub-agents", docHeader + llmAgent("a", ", sub_agents: [a, ghost]"),
			[]string{"sub_agents[0]: an agent cannot be its own sub-agent", `sub_agents[1]: unknown agent "ghost"`}},
		{"a sub-agent with two parents", docHeader + llmAgent("a", ", sub_agents: [c]") + llmAgent("b", ", sub_agents: [c]") + llmAgent("c", ""),
			[]string{`agents[1].sub_agents[0]: agent "c" is already a sub-agent of "a"`}},
		{"a sub-agent cycle", docHeader + llmAgent("a", ", sub_agents: [b]") + llmAgent("b", ", sub_agents: [a]"),
			[]string{"is part of a sub-agent cycle"}},
		{"a cycle through an agent node", docHeader + llmAgent("caller", ", sub_agents: [flow]") +
			"  - name: flow\n    agent_class: Workflow\n    nodes: [{ id: ask, type: agent, agent: caller }, { id: end, type: function, function: f }]\n    edges: [{ from: START, to: ask }, { from: ask, to: end }]\n",
			[]string{"agents reference themselves: caller → flow → caller"}},
		{"a workflow without nodes", docHeader + "  - { name: a, agent_class: Workflow }\n",
			[]string{"agents[0].nodes: required for agent_class Workflow"}},
		{"bad node ids", wf("[{ id: START, type: join }, { id: 'a-b', type: join }, { id: x, type: function, function: f }, { id: x, type: function, function: f }]", startTo("x")),
			[]string{`nodes[0].id: "START" is reserved`, "nodes[1].id:", `nodes[3].id: duplicate node id "x"`}},
		{"an excluded node type", wf("[{ id: n, type: cel }]", startTo("n")),
			[]string{`nodes[0].type: unknown node type "cel" (known: agent, function, join, llm, tool, workflow)`}},
		{"a function node without function", wf("[{ id: n, type: function }]", startTo("n")),
			[]string{"nodes[0].function: required for node type function"}},
		{"a field foreign to the node type", wf("[{ id: n, type: function, function: f, tool: t }]", startTo("n")),
			[]string{"nodes[0].tool: not allowed on node type function"}},
		{"a chat-mode inline llm node", wf("[{ id: n, type: llm, instruction: i, mode: chat }]", startTo("n")),
			[]string{`nodes[0].mode: an inline llm node runs mid-graph and must be single_turn, got "chat"`}},
		{"agent nodes with bad targets", docHeader + llmAgent("llm", "") +
			"  - name: wf\n    agent_class: Workflow\n    nodes: [{ id: a, type: agent, agent: ghost }, { id: b, type: agent, agent: wf }, { id: c, type: agent, agent: llm }, { id: d, type: agent, agent: llm }]\n" +
			"    edges: [{ from: START, to: a }, { from: a, to: b }, { from: b, to: c }, { from: c, to: d }]\n",
			[]string{`nodes[0].agent: unknown agent "ghost"`, `nodes[1].agent: agent "wf" is a Workflow`, `nodes[3].agent: agent "llm" is already used by node "c"`}},
		{"an agent name that collides with a node id", docHeader + llmAgent("llm", "") +
			"  - name: wf\n    agent_class: Workflow\n    nodes: [{ id: llm, type: function, function: f }, { id: a, type: agent, agent: llm }]\n    edges: [{ from: START, to: llm }, { from: llm, to: a }]\n",
			[]string{`agent "llm" collides with a node id`}},
		{"bad durations", wf("[{ id: n, type: function, function: f, timeout: soon, retry: { initial_delay: later, max_delay: -1s } }]", startTo("n")),
			[]string{"nodes[0].timeout: must be a positive duration", "retry.initial_delay: must be a non-negative duration", "retry.max_delay: must be a non-negative duration"}},
		{"bad retry numbers", wf("[{ id: n, type: function, function: f, retry: { max_attempts: -1, backoff_factor: 0, jitter: 2 } }]", startTo("n")),
			[]string{"retry.max_attempts: must not be negative", "retry.backoff_factor: must be positive", "retry.jitter: must be within [0, 1]"}},
		{"bad edge endpoints", wf("[{ id: n, type: function, function: f }]", "[{ from: ghost, to: n }, { from: START, to: START }, { from: n, to: END }]"),
			[]string{`edges[0].from: unknown node "ghost"`, `edges[1].to: "START" cannot be an edge target`, "edges[2].to: there is no END node"}},
		{"a routed START edge", wf("[{ id: n, type: function, function: f }]", "[{ from: START, to: n, route: X }]"),
			[]string{`edges[0].route: "START" does not emit routes`}},
		{"a duplicate edge and a second DEFAULT", wf("[{ id: a, type: function, function: f }, { id: b, type: function, function: f }, { id: c, type: function, function: f }]",
			"[{ from: START, to: a }, { from: START, to: a }, { from: a, to: b, route: DEFAULT }, { from: a, to: c, route: DEFAULT }]"),
			[]string{"edges[1]: duplicate edge", `edges[3].route: node "a" has more than one DEFAULT edge`}},
		{"a graph without a start", wf("[{ id: a, type: function, function: f }, { id: b, type: function, function: f }]", "[{ from: a, to: b }]"),
			[]string{"no edge from START and no entry"}},
		{"a node outside every edge", wf("[{ id: a, type: function, function: f }, { id: b, type: function, function: f }]", startTo("a")),
			[]string{`nodes[1]: node "b" is not connected by any edge`}},
		{"an unknown entry", wf("[{ id: a, type: function, function: f }]", startTo("a")) + "    entry: ghost\n",
			[]string{`entry: unknown node "ghost"`}},
		{"terminal structured nodes", wf("[{ id: t, type: tool, tool: x }, { id: j, type: join }]", "[{ from: START, to: t }, { from: START, to: j }]"),
			[]string{"nodes[0]: terminal tool node emits structured output", "nodes[1]: terminal join node"}},
		{"a terminal tool node inside a terminal nested workflow", wf("[{ id: outer, type: workflow, nodes: [{ id: t, type: tool, tool: x }], edges: [{ from: START, to: t }] }]", startTo("outer")),
			[]string{"agents[0].nodes[0].nodes[0]: terminal tool node"}},
		{"concurrent branches that share an output key", docHeader +
			"  - name: wf\n    agent_class: Workflow\n    nodes:\n" +
			"      - { id: left, type: llm, instruction: i, output_key: out }\n" +
			"      - { id: right, type: agent, agent: worker }\n" +
			"      - { id: g, type: join }\n" +
			"      - { id: e, type: function, function: f }\n" +
			"    edges: [{ from: START, to: left }, { from: START, to: right }, { from: left, to: g }, { from: right, to: g }, { from: g, to: e }]\n" +
			llmAgent("worker", ", mode: single_turn, output_key: out"),
			[]string{`nodes "left" and "right" run concurrently after "START" and both write the state key "out"`}},
		{"an error inside a nested workflow", wf("[{ id: outer, type: workflow, nodes: [{ id: in, type: function }], edges: [{ from: START, to: in }] }]", startTo("outer")),
			[]string{"agents[0].nodes[0].nodes[0].function: required for node type function"}},
		{"malformed and unresolvable node schemas", wf("[{ id: a, type: function, function: f, input_schema: { type: 12 } }, { id: b, type: function, function: f, output_schema: { $ref: '#/$defs/missing' } }]", "[{ from: START, to: a }, { from: a, to: b }]"),
			[]string{"nodes[0].input_schema: not a JSON Schema", "nodes[1].output_schema: cannot resolve the schema"}},
		{"an output_schema on a join", wf("[{ id: a, type: function, function: f }, { id: j, type: join, output_schema: { type: object } }, { id: e, type: function, function: f }]", "[{ from: START, to: a }, { from: a, to: j }, { from: j, to: e }]"),
			[]string{"nodes[1].output_schema: not allowed on node type join"}},
	}
	for _, sc := range scenarios {
		t.Run("Given a document with "+sc.name+", when it is parsed, assert that the issue is reported at its field path", func(t *testing.T) {
			_, err := Parse([]byte(sc.doc))
			require.True(t, IsValidation(err), "got %v", err)
			for _, want := range sc.want {
				require.ErrorContains(t, err, want)
			}
		})
	}

	t.Run("Given a document with several problems, when it is parsed, assert that every issue is reported together", func(t *testing.T) {
		_, err := Parse([]byte("apiVersion: nope\nkind: nope\nagents:\n  - { name: a, agent_class: LlmAgent, model: m }\n"))
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		require.Len(t, ve.Issues, 4)
		require.True(t, strings.HasPrefix(err.Error(), "invalid AgentSystem document:"))
	})

	t.Run("Given no document, when it is validated, assert that it is rejected", func(t *testing.T) {
		require.ErrorContains(t, Validate(nil), "document is required")
	})

	valid := map[string]string{
		"an entry that repeats the START edge": wf("[{ id: a, type: function, function: f }]", startTo("a")) + "    entry: a\n",
		"a tool node followed by a function":   wf("[{ id: t, type: tool, tool: x }, { id: f, type: function, function: g }]", "[{ from: START, to: t }, { from: t, to: f }]"),
		"routed branches that share an output key": docHeader +
			"  - name: wf\n    agent_class: Workflow\n    nodes:\n" +
			"      - { id: r, type: function, function: f }\n" +
			"      - { id: x, type: llm, instruction: i, output_key: out }\n" +
			"      - { id: y, type: llm, instruction: i, output_key: out }\n" +
			"    edges: [{ from: START, to: r }, { from: r, to: x, route: A }, { from: r, to: y, route: B }]\n",
	}
	for name, doc := range valid {
		t.Run("Given a document with "+name+", when it is parsed, assert that it is valid", func(t *testing.T) {
			_, err := Parse([]byte(doc))
			require.NoError(t, err)
		})
	}

	t.Run("Given workflow nodes nested deeper than the limit, when the document is parsed, assert that the depth is rejected", func(t *testing.T) {
		inner := "{ id: leaf, type: function, function: f }"
		first := "leaf"
		for range maxNesting + 2 {
			inner = "{ id: w, type: workflow, nodes: [" + inner + "], edges: [{ from: START, to: " + first + " }] }"
			first = "w"
		}
		_, err := Parse([]byte(wf("["+inner+"]", startTo("w"))))
		require.ErrorContains(t, err, "nested deeper than 8 levels")
	})
}

func TestParseDecodesStrictly(t *testing.T) {
	scenarios := map[string]string{
		"a misspelled agent field":          docHeader + llmAgent("a", ", temprature: 1"),
		"an unknown node field":             wf("[{ id: n, type: function, function: f, colour: red }]", startTo("n")),
		"an unknown edge field":             wf("[{ id: n, type: function, function: f }]", "[{ from: START, to: n, weight: 2 }]"),
		"a CEL retry condition":             wf("[{ id: n, type: function, function: f, retry: { when: 'true' } }]", startTo("n")),
		"a workflow max_concurrency":        wf("[{ id: n, type: function, function: f }]", startTo("n")) + "    max_concurrency: 2\n",
		"an MCP server list":                docHeader + "mcp_servers: []\n" + llmAgent("a", ""),
		"an instruction loaded from a file": docHeader + llmAgent("a", ", instruction_from: { configMapKeyRef: { name: x, key: y } }"),
		"broken YAML":                       "agents: [",
	}
	for name, doc := range scenarios {
		t.Run("Given a document with "+name+", when it is parsed, assert that decoding fails before validation", func(t *testing.T) {
			_, err := Parse([]byte(doc))
			require.ErrorContains(t, err, "failed to decode AgentSystem document")
			require.False(t, IsValidation(err))
		})
	}
}
