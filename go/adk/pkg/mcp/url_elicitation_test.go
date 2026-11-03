package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// urlElicitationToolContext is the part of a tool context the URL elicitation path uses.
type urlElicitationToolContext struct {
	adkagent.Context
	ctx          context.Context
	confirmation *toolconfirmation.ToolConfirmation
	actions      session.EventActions
	hint         string
	payload      any
}

func (c *urlElicitationToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return c.confirmation
}
func (c *urlElicitationToolContext) Actions() *session.EventActions { return &c.actions }
func (c *urlElicitationToolContext) FunctionCallID() string         { return "call-1" }
func (c *urlElicitationToolContext) RequestConfirmation(hint string, payload any) error {
	c.hint, c.payload = hint, payload
	return nil
}

func (c *urlElicitationToolContext) Deadline() (time.Time, bool) { return c.ctx.Deadline() }
func (c *urlElicitationToolContext) Done() <-chan struct{}       { return c.ctx.Done() }
func (c *urlElicitationToolContext) Err() error                  { return c.ctx.Err() }
func (c *urlElicitationToolContext) Value(key any) any           { return c.ctx.Value(key) }

// signInServer serves the tool whoami, which asks for a sign-in until the
// person signed in: with an input_required result on MCP 2026-07-28, which a
// stateless server serves, and with -32042 on 2025-11-25.
func signInServer(t *testing.T, stateless bool, signedIn *atomic.Bool, calls *atomic.Int32) string {
	t.Helper()
	signIn := &mcpsdk.ElicitParams{Mode: "url", Message: "Sign in to github", URL: "https://gateway.example/oauth/proxy/start?s=1", ElicitationID: "e1"}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "gateway", Version: "0.0.1"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "whoami", Description: "Names the person"}, func(_ context.Context, request *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
		calls.Add(1)
		if signedIn.Load() {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "jane"}}}, nil, nil
		}
		if request.ProtocolVersion() < "2026-07-28" {
			return nil, nil, mcpsdk.URLElicitationRequiredError([]*mcpsdk.ElicitParams{signIn})
		}
		return &mcpsdk.CallToolResult{InputRequests: mcpsdk.InputRequestMap{"sign_in": signIn}}, nil, nil
	})
	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: stateless}))
	t.Cleanup(func() {
		// A stateful session keeps the client's stream open.
		httpServer.CloseClientConnections()
		httpServer.Close()
	})
	return httpServer.URL
}

func TestURLElicitationToolAsksThePersonAndRunsOnceConfirmed(t *testing.T) {
	for _, test := range []struct {
		name      string
		stateless bool
	}{
		{name: "input required", stateless: true},
		{name: "-32042", stateless: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var signedIn atomic.Bool
			var calls atomic.Int32
			serverURL := signInServer(t, test.stateless, &signedIn, &calls)
			toolset, err := mcptoolset.New(mcptoolset.Config{
				Transport: &mcpsdk.StreamableClientTransport{Endpoint: serverURL},
				Client:    newElicitingClient(serverURL, nil),
			})
			require.NoError(t, err)
			wrapped := &urlElicitationToolset{inner: toolset, serverURL: serverURL}
			ctx := &urlElicitationToolContext{ctx: t.Context()}
			tools, err := wrapped.Tools(ctx)
			require.NoError(t, err)
			require.Len(t, tools, 1)
			whoami, ok := tools[0].(*urlElicitationTool)
			require.True(t, ok, "tool = %T", tools[0])

			_, err = whoami.Run(ctx, map[string]any{})
			require.ErrorIs(t, err, tool.ErrConfirmationRequired)
			require.Equal(t, int32(1), calls.Load(), "the server is not answered before the person opens the URL")
			payload, ok := ctx.payload.(map[string]any)
			require.True(t, ok, "payload = %#v", ctx.payload)
			require.Equal(t, apia2a.HITLTypeURLElicitationRequest, payload["type"])
			require.Equal(t, "https://gateway.example/oauth/proxy/start?s=1", payload["url"])
			require.Equal(t, "Sign in to github", payload["message"])
			require.Equal(t, "e1", payload["elicitation_id"])
			require.NotEmpty(t, payload["server"])
			require.Equal(t, "Sign in to github", ctx.hint)
			require.True(t, ctx.actions.SkipSummarization)

			signedIn.Store(true)
			before := calls.Load()
			confirmed := &urlElicitationToolContext{ctx: t.Context(), confirmation: &toolconfirmation.ToolConfirmation{Confirmed: true}}
			result, err := whoami.Run(confirmed, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, map[string]any{"output": "jane"}, result)
			require.Equal(t, before+1, calls.Load(), "the confirmed call runs once")

			rejected := &urlElicitationToolContext{ctx: t.Context(), confirmation: &toolconfirmation.ToolConfirmation{Confirmed: false}}
			_, err = whoami.Run(rejected, map[string]any{})
			require.True(t, errors.Is(err, tool.ErrConfirmationRejected), "err = %v", err)
		})
	}
}

func TestElicitationHandlerRelaysOnlyASafeURL(t *testing.T) {
	handler := elicitationHandler("https://gateway.example/mcp")
	for name, params := range map[string]*mcpsdk.ElicitParams{
		"form":       {Mode: "form", Message: "Your token?", RequestedSchema: map[string]any{"type": "object"}},
		"javascript": {Mode: "url", Message: "Sign in", URL: "javascript:alert(1)", ElicitationID: "e1"},
		"plain http": {Mode: "url", Message: "Sign in", URL: "http://gateway.example/s", ElicitationID: "e1"},
	} {
		result, err := handler(t.Context(), &mcpsdk.ElicitRequest{Params: params})
		require.NoError(t, err, name)
		require.Equal(t, "decline", result.Action, name)
	}

	_, err := handler(t.Context(), &mcpsdk.ElicitRequest{Params: &mcpsdk.ElicitParams{
		Mode: "url", Message: "Sign in", URL: "https://gateway.example/s", ElicitationID: "e1",
	}})
	var elicitation *urlElicitationError
	require.ErrorAs(t, err, &elicitation)
	require.Equal(t, apia2a.URLElicitationRequest{
		Type: apia2a.HITLTypeURLElicitationRequest, Server: "gateway.example",
		Message: "Sign in", URL: "https://gateway.example/s", ElicitationID: "e1",
	}, elicitation.request)
}
