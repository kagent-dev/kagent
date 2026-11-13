package declarative

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
)

const personSchema = `{ type: object, properties: { name: { type: string } }, required: [name], additionalProperties: false }`

func schemaRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := testRegistry(t)
	for name, fn := range map[string]Func{
		"make_person": func(_ agent.Context, in any) (Result, error) {
			return Output(map[string]any{"name": fmt.Sprint(in)}), nil
		},
		"make_wrong": func(_ agent.Context, in any) (Result, error) {
			return Output(map[string]any{"title": fmt.Sprint(in)}), nil
		},
		"greet": func(_ agent.Context, in any) (Result, error) {
			person, _ := in.(map[string]any)
			return Output("hello " + fmt.Sprint(person["name"])), nil
		},
	} {
		require.NoError(t, reg.RegisterFunction(name, fn))
	}
	return reg
}

func runSchemaDoc(t *testing.T, reg *Registry, nodes, edges string) error {
	t.Helper()
	spec, err := reg.Parse([]byte(docHead + "  - name: wf\n    agent_class: Workflow\n    nodes:\n" + nodes + "    edges: " + edges + "\n"))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	if err != nil {
		return err
	}
	svc, id := newSession(t)
	_, err = tryRun(t, sys, svc, id, "Ada")
	return err
}

func TestNodeOutputSchemaValidatesWhatAFunctionReturns(t *testing.T) {
	reg := schemaRegistry(t)
	nodes := func(fn string) string {
		return "      - { id: make, type: function, function: " + fn + ", output_schema: " + personSchema + " }\n" +
			"      - { id: greet, type: function, function: greet }\n"
	}
	edges := "[{ from: START, to: make }, { from: make, to: greet }]"
	require.NoError(t, runSchemaDoc(t, reg, nodes("make_person"), edges))
	err := runSchemaDoc(t, reg, nodes("make_wrong"), edges)
	require.Error(t, err, "a result that breaks the contract fails the run")
	require.ErrorContains(t, err, "make")
}

func TestNodeInputSchemaValidatesWhatANodeReceives(t *testing.T) {
	reg := schemaRegistry(t)
	nodes := func(fn string) string {
		return "      - { id: make, type: function, function: " + fn + " }\n" +
			"      - { id: greet, type: function, function: greet, input_schema: " + personSchema + " }\n"
	}
	edges := "[{ from: START, to: make }, { from: make, to: greet }]"
	require.NoError(t, runSchemaDoc(t, reg, nodes("make_person"), edges))
	require.Error(t, runSchemaDoc(t, reg, nodes("make_wrong"), edges))
}

func TestJoinInputSchemaAppliesToEachPredecessor(t *testing.T) {
	reg := schemaRegistry(t)
	nodes := func(second string) string {
		return "      - { id: one, type: function, function: make_person }\n" +
			"      - { id: two, type: function, function: " + second + " }\n" +
			"      - { id: gather, type: join, input_schema: " + personSchema + " }\n" +
			"      - { id: end, type: function, function: summarize_join }\n"
	}
	edges := "[{ from: START, to: one }, { from: START, to: two }, { from: one, to: gather }, { from: two, to: gather }, { from: gather, to: end }]"
	require.NoError(t, runSchemaDoc(t, reg, nodes("make_person"), edges))
	err := runSchemaDoc(t, reg, nodes("make_wrong"), edges)
	require.Error(t, err)
	require.ErrorContains(t, err, "predecessor")
}

func TestIncompatibleSchemasAreCaughtBeforeAnyRun(t *testing.T) {
	reg := schemaRegistry(t)
	err := runSchemaDoc(t, reg,
		"      - { id: make, type: function, function: make_person, output_schema: { type: string } }\n"+
			"      - { id: greet, type: function, function: greet, input_schema: { type: integer } }\n",
		"[{ from: START, to: make }, { from: make, to: greet }]")
	require.ErrorContains(t, err, `agent "wf"`, "the engine compares the schemas on every edge when it builds the graph")
	require.False(t, IsValidation(err))
}

func TestDynamicNodeOutputSchema(t *testing.T) {
	reg := schemaRegistry(t)
	require.NoError(t, reg.RegisterDynamic("person_maker", func(_ agent.Context, in any, _ *DynamicEnv) (any, error) {
		return map[string]any{"name": fmt.Sprint(in)}, nil
	}))
	require.NoError(t, runSchemaDoc(t, reg,
		"      - { id: make, type: dynamic, function: person_maker, output_schema: "+personSchema+" }\n"+
			"      - { id: greet, type: function, function: greet }\n",
		"[{ from: START, to: make }, { from: make, to: greet }]"))
}

func TestSchemaFieldsAreValidated(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{"malformed input_schema", wf("[{ id: f, type: function, function: f, input_schema: { type: 12 } }]", "[{ from: START, to: f }]"),
			"nodes[0].input_schema: not a JSON Schema"},
		{"malformed output_schema", wf("[{ id: f, type: function, function: f, output_schema: { type: 12 } }]", "[{ from: START, to: f }]"),
			"nodes[0].output_schema: not a JSON Schema"},
		{"unresolvable reference", wf("[{ id: f, type: function, function: f, input_schema: { $ref: '#/$defs/missing' } }]", "[{ from: START, to: f }]"),
			"cannot resolve the schema"},
		{"input_schema is not allowed on hitl", wf("[{ id: h, type: hitl, message: m, input_schema: { type: string } }, { id: f, type: function, function: f }]", "[{ from: START, to: h }, { from: h, to: f }]"),
			"nodes[0].input_schema: not allowed on node type hitl"},
		{"output_schema is not allowed on join", wf("[{ id: a, type: function, function: f }, { id: j, type: join, output_schema: { type: object } }, { id: f, type: function, function: f }]", "[{ from: START, to: a }, { from: a, to: j }, { from: j, to: f }]"),
			"nodes[1].output_schema: not allowed on node type join"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.doc))
			require.ErrorContains(t, err, tt.want)
		})
	}
}
