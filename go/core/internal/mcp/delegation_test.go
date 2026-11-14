package mcp

import (
	"context"
	"iter"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// delegationGateway records the delegating Session that admission receives.
type delegationGateway struct {
	*fakeGateway
	delegating []string
}

func (g *delegationGateway) SendStreamingMessage(ctx context.Context, request *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	g.delegating = append(g.delegating, sessionsvc.DelegatingSessionFrom(ctx))
	return g.fakeGateway.SendStreamingMessage(ctx, request)
}

func TestInvocationRequestsDelegationForSynchronousRuntimeCalls(t *testing.T) {
	runtime := auth.Principal{ID: "owner", RuntimeBinding: &auth.RuntimeBinding{SessionID: "parent-session"}}
	scenarios := []struct {
		name      string
		principal auth.Principal
		async     bool
		want      string
	}{
		{"Given a verified runtime origin, when it invokes synchronously, assert that admission receives the delegating Session", runtime, false, "parent-session"},
		{"Given a verified runtime origin, when it invokes as an MCP Task, assert that admission receives no delegating Session", runtime, true, ""},
		{"Given a user without a runtime origin, when it invokes synchronously, assert that admission receives no delegating Session", auth.Principal{ID: "owner"}, false, ""},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			gateway := &delegationGateway{fakeGateway: &fakeGateway{}}
			handler, err := New(Config{Agents: testAgentService(t), Sessions: testSessionService(), Checkpoints: testCheckpointService(), Gateway: gateway})
			require.NoError(t, err)
			ctx := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: scenario.principal})

			_, err = handler.invoke(ctx, InvokeAgentInput{SessionID: testSessionID, Message: "hello"}, scenario.async)

			require.NoError(t, err)
			require.Equal(t, []string{scenario.want}, gateway.delegating)
		})
	}
}
