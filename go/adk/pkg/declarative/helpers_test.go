package declarative

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

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
		words := []any{}
		for w := range strings.FieldsSeq(text) {
			words = append(words, w)
		}
		return Route(words, "BUG"), nil
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
	reg := NewRegistry()
	for name, fn := range map[string]Func{
		"branch_a":       constant("a"),
		"branch_b":       constant("b"),
		"branch_c":       constant("c"),
		"route_by_text":  routeByText,
		"prefix":         func(_ agent.Context, in any) (Result, error) { return Output(fmt.Sprintf("item:%v", in)), nil },
		"plan":           func(_ agent.Context, in any) (Result, error) { return Output(fmt.Sprintf("plan(%v)", in)), nil },
		"execute":        func(_ agent.Context, in any) (Result, error) { return Output(fmt.Sprintf("exec(%v)", in)), nil },
		"summarize_join": func(_ agent.Context, in any) (Result, error) { return Output(fmt.Sprintf("summary:%v", in)), nil },
		"critic_step":    func(_ agent.Context, in any) (Result, error) { return Output(fmt.Sprintf("critiqued(%v)", in)), nil },
	} {
		require.NoError(t, reg.RegisterFunction(name, fn))
	}
	require.NoError(t, reg.RegisterDynamic("critique", func(_ agent.Context, in any, env *DynamicEnv) (any, error) {
		for {
			if _, err := env.RunChild("critic", in); errors.Is(err, ErrIterationBudget) {
				return fmt.Sprintf("%v after %d runs", in, env.Runs()), nil
			} else if err != nil {
				return nil, err
			}
		}
	}))
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

func loadShowcase(t testing.TB) *Spec {
	t.Helper()
	data, err := os.ReadFile("testdata/showcase.yaml")
	require.NoError(t, err)
	spec, err := testRegistry(t).Parse(data)
	require.NoError(t, err)
	return spec
}

// run drives root once and returns every event.
func run(t testing.TB, root agent.Agent, content *genai.Content, sessions session.Service, sessionID string) []*session.Event {
	t.Helper()
	r, err := runner.New(runner.Config{AppName: "test", Agent: root, SessionService: sessions, MemoryService: memory.InMemoryService()})
	require.NoError(t, err)
	var events []*session.Event
	for ev, err := range r.Run(context.Background(), "user", sessionID, content, agent.RunConfig{}) {
		require.NoError(t, err)
		events = append(events, ev)
	}
	return events
}

func newSession(t testing.TB) (session.Service, string) {
	t.Helper()
	svc := session.InMemoryService()
	resp, err := svc.Create(context.Background(), &session.CreateRequest{AppName: "test", UserID: "user"})
	require.NoError(t, err)
	return svc, resp.Session.ID()
}

// tryRun runs the root agent once and returns the first error instead of failing.
func tryRun(t testing.TB, sys *System, sessions session.Service, sessionID, input string) ([]*session.Event, error) {
	t.Helper()
	r, err := runner.New(runner.Config{AppName: "test", Agent: sys.Root(), SessionService: sessions, MemoryService: memory.InMemoryService()})
	require.NoError(t, err)
	var events []*session.Event
	for ev, err := range r.Run(context.Background(), "user", sessionID, genai.NewContentFromText(input, genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
	return events, nil
}
