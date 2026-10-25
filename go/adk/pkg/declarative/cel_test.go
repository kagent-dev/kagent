package declarative

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestCELVarsRoutesAndHelpers(t *testing.T) {
	sys := buildDoc(t, NewRegistry(), head+`
agents:
  - name: vars_wf
    agent_class: Workflow
    nodes:
      - id: classify
        type: cel
        vars:
          - { name: text, expr: input.lowerAscii() }
          - { name: refund, expr: "vars.text.contains('refund')" }
        routes:
          - { route: URGENT, when: "vars.text.contains('outage')" }
          - { route: BILLING, when: vars.refund }
        output: |
          json('{"user": {"name": "ann"}}').with(doc, [
            vars.text,
            default(doc.user.name, 'anon'),
            default(doc.user.tier, 'free'),
            default(state['missing'], 'none'),
            toJson({'refund': vars.refund}),
          ].join(' '))
      - { id: urgent, type: cel, output: "'urgent: ' + input" }
      - { id: billing, type: cel, output: "'billing: ' + input" }
      - { id: other, type: cel, output: "'other: ' + input" }
    edges:
      - { from: START, to: classify }
      - { from: classify, to: urgent, route: URGENT }
      - { from: classify, to: billing, route: BILLING }
      - { from: classify, to: other, route: DEFAULT }
`)
	for in, want := range map[string]string{
		"Outage and REFUND": `urgent: outage and refund ann free none {"refund":true}`,
		"Refund please":     `billing: refund please ann free none {"refund":true}`,
		"Hello":             `other: hello ann free none {"refund":false}`,
	} {
		svc, id := newSession(t)
		reply := replyText(run(t, sys.Root(), genai.NewContentFromText(in, genai.RoleUser), svc, id))
		require.Equal(t, want, reply, in)
	}
}

// TestCELNodeRunsConcurrently runs one built cel node from many sessions at
// once. Each run must see only its own evaluation error; run it with -race.
func TestCELNodeRunsConcurrently(t *testing.T) {
	sys := buildDoc(t, NewRegistry(), head+`
agents:
  - name: double
    agent_class: Workflow
    nodes:
      - { id: double, type: cel, output: "string(int(input) * 2)" }
    edges:
      - { from: START, to: double }
`)
	const runs = 32
	errs := make([]error, runs)
	replies := make([]string, runs)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Go(func() {
			input := strconv.Itoa(i)
			if i%2 == 1 {
				input = "not a number"
			}
			svc, id := newSession(t)
			events, err := tryRun(t, sys, svc, id, input)
			errs[i], replies[i] = err, replyText(events)
		})
	}
	wg.Wait()
	for i := range runs {
		if i%2 == 1 {
			require.Error(t, errs[i], "run %d", i)
			continue
		}
		require.NoError(t, errs[i], "run %d", i)
		require.Equal(t, strconv.Itoa(i*2), replies[i], "run %d", i)
	}
}

func TestCELVarsAndRoutesValidation(t *testing.T) {
	reg := NewRegistry()
	for _, tc := range []struct{ node, want string }{
		{`{ id: c, type: cel, vars: [{ name: "1x", expr: "1" }], output: "'x'" }`, "must be an identifier"},
		{`{ id: c, type: cel, vars: [{ name: a, expr: "1" }, { name: a, expr: "2" }], output: "'x'" }`, `duplicate var "a"`},
		{`{ id: c, type: cel, vars: [{ name: a, expr: "1 +" }], output: "'x'" }`, "vars[0].expr"},
		{`{ id: c, type: cel, route: "'A'", routes: [{ route: B }] }`, "route and routes are mutually exclusive"},
		{`{ id: c, type: cel, routes: [{ when: "true" }] }`, "routes[0].route: required"},
		{`{ id: c, type: cel, routes: [{ route: A }, { route: B, when: "true" }] }`, "only the last rule may omit when"},
		{`{ id: c, type: cel, routes: [{ route: A, when: "'yes'" }] }`, "routes[0].when"},
		{`{ id: c, type: cel, vars: [{ name: a, expr: "vars.b" }, { name: b, expr: "1" }], output: "'x'" }`, "vars.b is not declared before"},
		{`{ id: c, type: cel, vars: [{ name: a, expr: "1" }], output: "vars.nope" }`, "vars.nope is not declared"},
		{`{ id: c, type: cel, vars: [{ name: a, expr: "1" }] }`, "at least one of output, route, routes, state_delta"},
		{`{ id: c, type: cel, output: "default(input, 'x')" }`, "default() needs a field or index access"},
		{`{ id: c, type: cel, output: "input.with('n', 1)" }`, "with() variable name must be a simple identifier"},
	} {
		_, err := reg.Parse([]byte(head + `
agents:
  - name: w
    agent_class: Workflow
    nodes: [` + tc.node + `]
    edges: [{ from: START, to: c }]`))
		require.ErrorContains(t, err, tc.want, tc.node)
	}
}

func TestEdgeToENDExplainsTerminals(t *testing.T) {
	_, err := NewRegistry().Parse([]byte(head + `
agents:
  - name: w
    agent_class: Workflow
    nodes: [{ id: c, type: cel, output: "'x'" }]
    edges: [{ from: START, to: c }, { from: c, to: END }]`))
	require.ErrorContains(t, err, "there is no END node")
}
