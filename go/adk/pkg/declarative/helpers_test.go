package declarative

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

const docHeader = "apiVersion: adk.declarative/v0alpha1\nkind: AgentSystem\nagents:\n"

// wf wraps nodes and edges in a minimal Workflow agent.
func wf(nodes, edges string) string {
	return docHeader + "  - name: wf\n    agent_class: Workflow\n    nodes: " + nodes + "\n    edges: " + edges + "\n"
}

func userText(ctx agent.Context) string {
	var sb strings.Builder
	if uc := ctx.UserContent(); uc != nil {
		for _, p := range uc.Parts {
			if p != nil {
				sb.WriteString(p.Text)
			}
		}
	}
	return sb.String()
}

func constant(v string) Func {
	return func(agent.Context, any) (Result, error) { return Output(v), nil }
}

// routeByText shapes the output for the branch it selects, because each
// successor expects a different input type.
func routeByText(ctx agent.Context, _ any) (Result, error) {
	text := userText(ctx)
	switch {
	case strings.HasPrefix(text, "bug"):
		return Route(strings.Fields(text), "BUG"), nil
	case strings.HasPrefix(text, "bill"):
		return Route(map[string]any{"customer_id": "c-42"}, "BILLING"), nil
	case strings.HasPrefix(text, "help"):
		return Route(text, "SUPPORT"), nil
	case strings.HasPrefix(text, "other"):
		return Route(text, "OTHER"), nil
	}
	return Route(text, "UNMATCHED"), nil
}

func testRegistry(t testing.TB) *Registry {
	t.Helper()
	reg := NewRegistry(nil)
	for name, fn := range map[string]Func{
		"branch_a":       constant("a"),
		"branch_b":       constant("b"),
		"branch_c":       constant("c"),
		"route_by_text":  routeByText,
		"prefix":         func(_ agent.Context, in any) (Result, error) { return Output(fmt.Sprintf("item:%v", in)), nil },
		"plan":           func(_ agent.Context, in any) (Result, error) { return Output(fmt.Sprintf("plan(%v)", in)), nil },
		"execute":        func(_ agent.Context, in any) (Result, error) { return Output(fmt.Sprintf("exec(%v)", in)), nil },
		"summarize_join": func(_ agent.Context, in any) (Result, error) { return Output(fmt.Sprintf("summary:%v", in)), nil },
	} {
		require.NoError(t, reg.RegisterFunction(name, fn))
	}
	type lookupArgs struct {
		CustomerID string `json:"customer_id"`
	}
	lookup, err := functiontool.New(functiontool.Config{Name: "lookup_customer", Description: "Looks up a customer."},
		func(_ agent.Context, a lookupArgs) (map[string]any, error) {
			return map[string]any{"customer": a.CustomerID, "plan": "pro"}, nil
		})
	require.NoError(t, err)
	require.NoError(t, reg.RegisterTool(lookup))
	return reg
}

func readShowcase(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/showcase.yaml")
	require.NoError(t, err)
	return data
}

// buildDoc parses doc against reg and builds it.
func buildDoc(t testing.TB, reg *Registry, doc string) *System {
	t.Helper()
	spec, err := reg.Parse([]byte(doc))
	require.NoError(t, err)
	sys, err := Build(spec, reg)
	require.NoError(t, err)
	return sys
}

func newSession(t testing.TB) (session.Service, string) {
	t.Helper()
	svc := session.InMemoryService()
	resp, err := svc.Create(context.Background(), &session.CreateRequest{AppName: "test", UserID: "user"})
	require.NoError(t, err)
	return svc, resp.Session.ID()
}

// tryRun runs root once in a fresh session and returns its events and the
// first error.
func tryRun(t testing.TB, root agent.Agent, input string) ([]*session.Event, error) {
	t.Helper()
	svc, id := newSession(t)
	r, err := runner.New(runner.Config{AppName: "test", Agent: root, SessionService: svc})
	require.NoError(t, err)
	var events []*session.Event
	for ev, err := range r.Run(context.Background(), "user", id, genai.NewContentFromText(input, genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
	return events, nil
}

// run is tryRun for runs that must succeed.
func run(t testing.TB, root agent.Agent, input string) []*session.Event {
	t.Helper()
	events, err := tryRun(t, root, input)
	require.NoError(t, err)
	return events
}

// outputs returns the non-nil event outputs in arrival order.
func outputs(events []*session.Event) []any {
	var out []any
	for _, ev := range events {
		if ev.Output != nil {
			out = append(out, ev.Output)
		}
	}
	return out
}

// texts returns the message text that A2A clients would display.
func texts(events []*session.Event) []string {
	var out []string
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.Text != "" {
				out = append(out, p.Text)
			}
		}
	}
	return out
}
