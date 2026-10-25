package mcp

import (
	"context"
	"io"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"

	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// Authenticated MCP connections belong to the invocation that opened them.
// Creating a tool listing never borrows a connection opened by a different
// caller. Background SSE reads and teardown retain the same credential source.
type invocationToolset struct {
	params   mcpServerParams
	filter   map[string]bool
	approval bool

	mu          sync.Mutex
	invocations map[invocationKey]*invocationTools
}

type invocationKey struct {
	app, session, user, caller, invocation string
}

type invocationTools struct {
	ready   chan struct{}
	toolset tool.Toolset
	err     error
}

func (*invocationToolset) Name() string { return "mcp_toolset" }

func (s *invocationToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	inner, err := s.forInvocation(ctx)
	if err != nil {
		return nil, err
	}
	return inner.Tools(ctx)
}

func (s *invocationToolset) forInvocation(ctx agent.ReadonlyContext) (tool.Toolset, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Standalone callers without an invocation lifetime retain uncached behavior.
	if ctx.InvocationID() == "" || ctx.Done() == nil {
		return s.initialize(ctx)
	}
	key := invocationKey{
		app: ctx.AppName(), session: ctx.SessionID(), user: ctx.UserID(),
		caller: auth.UserIDFromContext(ctx), invocation: ctx.InvocationID(),
	}
	s.mu.Lock()
	if entry, ok := s.invocations[key]; ok {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.ready:
			return entry.toolset, entry.err
		}
	}
	entry := &invocationTools{ready: make(chan struct{})}
	if s.invocations == nil {
		s.invocations = make(map[invocationKey]*invocationTools)
	}
	s.invocations[key] = entry
	s.mu.Unlock()

	stop := context.AfterFunc(ctx, func() { s.remove(key, entry) })
	// Only the first caller constructs the transport and classifies tools.
	// The ADK toolset owns connection synchronization, refresh, and retries.
	entry.toolset, entry.err = s.initialize(ctx)
	if entry.err != nil {
		stop()
		s.remove(key, entry)
	}
	close(entry.ready)
	return entry.toolset, entry.err
}

func (s *invocationToolset) remove(key invocationKey, entry *invocationTools) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.invocations[key] == entry {
		delete(s.invocations, key)
	}
}

func (s *invocationToolset) initialize(ctx context.Context) (tool.Toolset, error) {
	ts, err := initializeToolSet(ctx, s.params, s.filter)
	if err != nil {
		return nil, err
	}
	inner := withCallScope(ts)
	if s.approval {
		inner = tool.WithConfirmation(inner, true, nil)
	}
	return inner, nil
}

func closeOnInvocationEnd(ctx context.Context, closer io.Closer) {
	context.AfterFunc(ctx, func() {
		if err := closer.Close(); err != nil {
			logging.FromContext(ctx).DebugContext(ctx, "failed to close invocation MCP connection", "error", err)
		}
	})
}

var _ tool.Toolset = (*invocationToolset)(nil)
