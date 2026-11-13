package declarative

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
)

func buildShowcase(t *testing.T) *System {
	t.Helper()
	return buildDoc(t, testRegistry(t), string(readShowcase(t)))
}

func TestBuildShowcase(t *testing.T) {
	scenarios := []struct {
		name, input string
		final       any
	}{
		{"a BUG ticket runs the nested workflow", "bug x y", "summary:exec(plan([bug x y]))"},
		{"a BILLING ticket runs the tool node", "bill me", "summary:map[customer:c-42 plan:pro]"},
		{"a SUPPORT ticket runs the agent node", "help me", "summary:[stub] help me"},
		{"an OTHER ticket runs the inline llm node", "other thing", "summary:[stub] other thing"},
		{"an unmatched ticket takes the DEFAULT route", "zzz", "item:zzz"},
	}
	for _, sc := range scenarios {
		t.Run("Given the showcase, when "+sc.name+", assert that its branch produces the final output", func(t *testing.T) {
			got := outputs(run(t, buildShowcase(t).Root(), sc.input))
			require.Equal(t, sc.final, got[len(got)-1])
		})
	}

	t.Run("Given the showcase, when it runs, assert that the join collects every fan-out branch", func(t *testing.T) {
		got := outputs(run(t, buildShowcase(t).Root(), "zzz"))
		require.Contains(t, got, map[string]any{"branch_a": "a", "branch_b": "b", "branch_c": "c"})
	})

	t.Run("Given the showcase, when it is built, assert that every agent is addressable and shared agents are built once", func(t *testing.T) {
		sys := buildShowcase(t)
		require.Equal(t, "multi_agent_root", sys.Root().Name())
		require.Equal(t, []string{"multi_agent_root", "specialist_helper", "code_expert", "learning_coordinator", "docs_expert"}, sys.Names())

		coordinator, ok := sys.Agent("learning_coordinator")
		require.True(t, ok)
		require.Equal(t, "docs_expert", coordinator.SubAgents()[0].Name())
		_, ok = sys.Agent("missing")
		require.False(t, ok)

		helper, _ := sys.Agent("specialist_helper")
		require.Same(t, helper, sys.Root().SubAgents()[0])

		loader, err := sys.Loader()
		require.NoError(t, err)
		require.Equal(t, "multi_agent_root", loader.RootAgent().Name())
		require.ElementsMatch(t, sys.Names(), loader.ListAgents())
	})
}

func TestBuild(t *testing.T) {
	t.Run("Given a nil registry, when a stub LlmAgent is built and run, assert that it replies", func(t *testing.T) {
		spec, err := Parse([]byte(docHeader + llmAgent("solo", ", model: stub")))
		require.NoError(t, err)
		sys, err := Build(spec, nil)
		require.NoError(t, err)
		require.Contains(t, texts(run(t, sys.Root(), "ping")), "[stub] ping")
	})

	t.Run("Given an invalid document, when it is built, assert that a validation error is returned", func(t *testing.T) {
		_, err := Build(&Spec{}, NewRegistry(nil))
		require.True(t, IsValidation(err))
	})

	failures := map[string]string{
		"an unconditional cycle": wf("[{ id: a, type: function, function: prefix }, { id: b, type: function, function: prefix }]",
			"[{ from: START, to: a }, { from: a, to: b }, { from: b, to: a }]"),
		"a fan-in to a non-join node": wf("[{ id: a, type: function, function: prefix }, { id: b, type: function, function: prefix }, { id: c, type: function, function: prefix }]",
			"[{ from: START, to: a }, { from: START, to: b }, { from: a, to: c }, { from: b, to: c }]"),
		"a task-mode agent node": docHeader + llmAgent("worker", ", model: stub, mode: task") +
			"  - name: wf\n    agent_class: Workflow\n    nodes: [{ id: n, type: agent, agent: worker }]\n    edges: [{ from: START, to: n }]\n",
		"incompatible schemas on an edge": wf("[{ id: a, type: function, function: prefix, output_schema: { type: string } }, { id: b, type: function, function: prefix, input_schema: { type: integer } }]",
			"[{ from: START, to: a }, { from: a, to: b }]"),
	}
	for name, doc := range failures {
		t.Run("Given a valid document with "+name+", when it is built, assert that the engine rejects the agent", func(t *testing.T) {
			reg := testRegistry(t)
			spec, err := reg.Parse([]byte(doc))
			require.NoError(t, err)
			_, err = Build(spec, reg)
			require.ErrorContains(t, err, `failed to build agent "wf"`)
			require.False(t, IsValidation(err))
		})
	}
}

func TestNodeExecution(t *testing.T) {
	t.Run("Given a function with retry, when it fails twice, assert that the third attempt's output is returned", func(t *testing.T) {
		var calls atomic.Int32
		reg := NewRegistry(nil)
		require.NoError(t, reg.RegisterFunction("flaky", func(agent.Context, any) (Result, error) {
			if calls.Add(1) < 3 {
				return Result{}, errors.New("transient")
			}
			return Output("recovered"), nil
		}))
		sys := buildDoc(t, reg, wf("[{ id: only, type: function, function: flaky, retry: { max_attempts: 3, initial_delay: 1ms, max_delay: 2ms, backoff_factor: 1, jitter: 0 } }]", startTo("only")))
		require.Equal(t, []any{"recovered"}, outputs(run(t, sys.Root(), "go")))
		require.EqualValues(t, 3, calls.Load())
	})

	t.Run("Given a function with a timeout, when it outlives the timeout, assert that the run fails with a deadline error", func(t *testing.T) {
		reg := NewRegistry(nil)
		require.NoError(t, reg.RegisterFunction("slow", func(ctx agent.Context, _ any) (Result, error) {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			case <-time.After(5 * time.Second):
				return Output("too late"), nil
			}
		}))
		sys := buildDoc(t, reg, wf("[{ id: only, type: function, function: slow, timeout: 20ms }]", startTo("only")))
		_, err := tryRun(t, sys.Root(), "go")
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("Given a chain of function nodes, when it runs, assert that only the terminal output reaches the chat", func(t *testing.T) {
		reg := testRegistry(t)
		require.NoError(t, reg.RegisterFunction("report", func(agent.Context, any) (Result, error) {
			return Output(map[string]any{"status": "ok", "count": 2}), nil
		}))
		sys := buildDoc(t, reg, wf("[{ id: first, type: function, function: prefix }, { id: nested, type: workflow, nodes: [{ id: inner, type: function, function: report }], edges: [{ from: START, to: inner }] }]",
			"[{ from: START, to: first }, { from: first, to: nested }]"))
		require.Equal(t, []string{`{"count":2,"status":"ok"}`}, texts(run(t, sys.Root(), "go")))
	})
}

const personSchema = `{ type: object, properties: { name: { type: string } }, required: [name], additionalProperties: false }`

func TestNodeSchemas(t *testing.T) {
	reg := func(t *testing.T) *Registry {
		r := testRegistry(t)
		require.NoError(t, r.RegisterFunction("make_person", func(_ agent.Context, in any) (Result, error) {
			return Output(map[string]any{"name": fmt.Sprint(in)}), nil
		}))
		require.NoError(t, r.RegisterFunction("make_wrong", func(_ agent.Context, in any) (Result, error) {
			return Output(map[string]any{"title": fmt.Sprint(in)}), nil
		}))
		return r
	}
	twoStep := func(first, firstSchema, secondSchema string) string {
		return wf("[{ id: make, type: function, function: "+first+firstSchema+" }, { id: greet, type: function, function: prefix"+secondSchema+" }]",
			"[{ from: START, to: make }, { from: make, to: greet }]")
	}
	join := func(second string) string {
		return wf("[{ id: one, type: function, function: make_person }, { id: two, type: function, function: "+second+" }, { id: gather, type: join, input_schema: "+personSchema+" }, { id: end, type: function, function: prefix }]",
			"[{ from: START, to: one }, { from: START, to: two }, { from: one, to: gather }, { from: two, to: gather }, { from: gather, to: end }]")
	}
	scenarios := []struct {
		name, doc string
	}{
		{"an output_schema", twoStep("%s", ", output_schema: "+personSchema, "")},
		{"an input_schema on the next node", twoStep("%s", "", ", input_schema: "+personSchema)},
		{"a join input_schema", join("%s")},
	}
	for _, sc := range scenarios {
		t.Run("Given "+sc.name+", when a node produces a conforming value, assert that the run succeeds", func(t *testing.T) {
			r := reg(t)
			_, err := tryRun(t, buildDoc(t, r, fmt.Sprintf(sc.doc, "make_person")).Root(), "Ada")
			require.NoError(t, err)
		})
		t.Run("Given "+sc.name+", when a node produces a non-conforming value, assert that the run fails", func(t *testing.T) {
			r := reg(t)
			_, err := tryRun(t, buildDoc(t, r, fmt.Sprintf(sc.doc, "make_wrong")).Root(), "Ada")
			require.Error(t, err)
		})
	}
}
